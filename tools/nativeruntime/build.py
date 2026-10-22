"""Build the Lo runtime and its declared LLVM executable distribution."""
from __future__ import annotations

import argparse
import fcntl
import hashlib
import json
import os
import shutil
from pathlib import Path
import subprocess
import sys

from prepare import LOCK, ROOT, RUST_COMPONENTS, digest, local, pin_file, tree_digest

TARGETS = {'darwin/arm64': 'aarch64-apple-darwin', 'linux/arm64': 'aarch64-unknown-linux-gnu'}


METAL_FILES = {
    'internal/device/metal_bridge.h',
    'internal/device/metal_bridge_darwin.m',
    'internal/device/metal_darwin.go',
    'internal/device/metal_types.h',
    'internal/metalconfig/config.go',
    'runtime/metal/embed.go',
    'runtime/metal/kernels.metal',
    'runtime/metal/metal.metallib',
    'tools/metalbuild/main.go',
}
METAL_LIBRARY = 'runtime/metal/metal.metallib'
METAL_MANIFEST = 'runtime/metal/manifest.json'


def metal_assets() -> tuple[bytes, dict[str, object], dict[str, object]]:
    manifest = local(ROOT / METAL_MANIFEST)
    producer = local(ROOT / 'bin/lo-metal-build.json')
    if not all(local(ROOT / name).is_file() for name in (*METAL_FILES, METAL_MANIFEST, 'bin/lo-metal-build.json')):
        raise ValueError('Metal requires the prepared kernel assets and producer receipt from make build-metal')
    record = json.loads(manifest.read_text())
    receipt = json.loads(producer.read_text())
    if set(record) != {'identity', 'files'}:
        raise ValueError('Metal asset manifest has an incorrect field set')
    identity, files = record['identity'], record['files']
    identity_fields = {'profile', 'xcode', 'sdk', 'clang', 'metal_compiler', 'kernel_linker',
                       'deployment_target', 'kernel_flags', 'kernel_sha256'}
    if set(identity) != identity_fields or set(files) != METAL_FILES:
        raise ValueError('Metal assets have an incorrect identity or source set')
    if identity['profile'] != 'metal-fp32' or identity['deployment_target'] != '27.0':
        raise ValueError('Metal assets use an unsupported numeric profile')
    verified_sources = METAL_FILES | {METAL_MANIFEST}
    for name in sorted(verified_sources):
        source = local(ROOT / name)
        if digest(source) != receipt['source_sha256'].get(name):
            raise ValueError(f'Metal producer source identity differs: {name}')
        if name in METAL_FILES and digest(source) != files[name]:
            raise ValueError(f'Metal asset source differs: {name}')
    library = local(ROOT / METAL_LIBRARY).read_bytes()
    if files[METAL_LIBRARY] != identity['kernel_sha256'] or receipt['metallib_sha256'] != identity['kernel_sha256']:
        raise ValueError('Metal kernel identity differs from its producer receipt')
    return library, identity, {'manifest': pin_file(manifest), 'producer': pin_file(producer),
                               'sources': {name: pin_file(local(ROOT / name)) for name in sorted(verified_sources)}}


def metal_archive(preparation: Path, tools: dict[str, dict[str, str]], archive: Path,
                  library: bytes, identity: dict[str, object]) -> tuple[Path, Path]:
    work = local(preparation / 'metal-native')
    work.mkdir(parents=True, exist_ok=True)
    compiler = str(local(Path(tools['clang']['path'])))
    headers = local(ROOT / 'runtime/native/include')
    bridge = local(ROOT / 'internal/device/metal_bridge_darwin.m')
    embedding = local(work / 'kernel.c')
    embedding.write_text('#include <stddef.h>\nconst unsigned char lo_native_metal_kernel_bytes[]={'
                         + ','.join(f'0x{byte:02x}' for byte in library)
                         + '};\nconst size_t lo_native_metal_kernel_length=sizeof(lo_native_metal_kernel_bytes);\n'
                         + 'const char lo_native_metal_kernel_digest[]=' + json.dumps(identity['kernel_sha256']) + ';\n'
                         + 'const void *lo_native_metal_library(size_t *length){*length=lo_native_metal_kernel_length;return lo_native_metal_kernel_bytes;}\n'
                         + 'const char *lo_native_metal_kernel_sha256(void){return lo_native_metal_kernel_digest;}\n')
    environment = {'CI': '1', 'PATH': '', 'TMPDIR': str(work)}
    base = [compiler, '-target', 'aarch64-apple-darwin', '-mmacosx-version-min=27.0', '-nostdinc',
            '-I' + str(headers), '-I' + str(headers / 'darwin'), '-I' + str(bridge.parent),
            '-fPIC', '-O2', '-Werror']
    objects = [work / 'bridge.o', work / 'kernel.o']
    for command in [base + ['-fblocks', '-fno-objc-arc', '-c', str(bridge), '-o', str(objects[0])],
                    base + ['-c', str(embedding), '-o', str(objects[1])]]:
        result = subprocess.run(command, env=environment, cwd=ROOT, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, check=False)
        if result.returncode:
            raise ValueError(f'native Metal bridge build failed: {result.stderr}')
    combined = local(work / 'liblo_runtime.a')
    shutil.copyfile(archive, combined)
    subprocess.run([str(local(Path(tools['llvm-ar']['path']))), 'q', str(combined),
                    *[str(path) for path in objects]], env=environment, cwd=ROOT,
                   stdin=subprocess.DEVNULL, capture_output=True, check=True, text=True)
    kernel = local(work / 'metal.metallib')
    kernel.write_bytes(library)
    return combined, kernel


def validate_build_environment() -> None:
    # Reject attempted overrides explicitly, before reading or mutating artifacts.
    controls = {'RUSTFLAGS', 'CARGO_ENCODED_RUSTFLAGS', 'RUSTC_WRAPPER',
                'RUSTC_WORKSPACE_WRAPPER', 'CARGO_BUILD_RUSTFLAGS'}
    for name in sorted(os.environ):
        if name in controls or name.startswith('CARGO_PROFILE_'):
            raise ValueError(f'undeclared Cargo build control: {name}')


def cargo_environment(preparation: Path, compiler: Path, host_linker: Path, sdk: Path) -> dict[str, str]:
    # No caller environment is a Cargo input. These are the complete declared
    # inputs; Cargo.toml, .cargo/config.toml and the vendored lock own all flags.
    return {'CI': '1', 'CARGO_HOME': str(local(preparation / 'cargo-home')),
            'RUSTUP_HOME': str(local(preparation / 'rustup-home')),
            'RUSTC': str(compiler), 'CARGO_TARGET_DIR': str(local(preparation / 'cargo-target')),
            'CARGO_TARGET_AARCH64_APPLE_DARWIN_LINKER': str(host_linker), 'SDKROOT': str(sdk),
            'TMPDIR': str(preparation), 'PATH': str(compiler.parent),
            'MACOSX_DEPLOYMENT_TARGET': '27.0',
            'LC_ALL': 'C', 'TZ': 'UTC'}


def native_c_environment(preparation: Path, tools: dict[str, dict[str, str]]) -> dict[str, str]:
    compiler = local(Path(tools['clang']['path']))
    archiver = local(Path(tools['llvm-ar']['path']))
    for name, path in (('clang', compiler), ('llvm-ar', archiver)):
        if digest(path) != tools[name]['sha256']:
            raise ValueError(f'prepared C tool digest mismatch: {name}')
    headers = local(ROOT / 'runtime/native/include')
    result = {'CRATE_CC_NO_DEFAULTS': '1'}
    for triple in TARGETS.values():
        target = triple.replace('-', '_')
        flags = ['-target', triple, '-nostdinc', '-I' + str(headers), '-fPIC']
        if triple == 'aarch64-apple-darwin':
            flags += ['-I' + str(headers / 'darwin'), '-mmacosx-version-min=27.0']
        result['CC_' + target] = str(compiler)
        result['AR_' + target] = str(archiver)
        result['CFLAGS_' + target] = ' '.join(flags)
    return result


def process_archive(preparation: Path, tools: dict[str, dict[str, str]], archive: Path) -> Path:
    work = local(preparation / 'process-native')
    work.mkdir(parents=True, exist_ok=True)
    compiler = str(local(Path(tools['clang']['path'])))
    headers = local(ROOT / 'runtime/native/include')
    environment = {'CI': '1', 'PATH': '', 'TMPDIR': str(work)}
    sources = [('transport', ROOT / 'runtime/native/src/process_transport.c', []),
               ('launcher', ROOT / 'internal/processhost/native/launcher.c', ['-Dmain=lo_process_launcher_main']),
               ('normalization', ROOT / 'internal/processhost/native/normalization.c', [])]
    objects = []
    for name, source, defines in sources:
        output = local(work / (name + '.o'))
        command = [compiler, '-target', 'aarch64-apple-darwin', '-mmacosx-version-min=27.0',
                   '-nostdinc', '-I' + str(headers), '-I' + str(headers / 'darwin'),
                   '-DLO_NATIVE_RUNTIME', '-fPIC', '-O2', *defines, '-c', str(local(source)), '-o', str(output)]
        result = subprocess.run(command, env=environment, cwd=ROOT, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, check=False)
        if result.returncode:
            raise ValueError(f'native process {name} build failed: {result.stderr}')
        objects.append(str(output))
    combined = local(work / 'liblo_runtime.a')
    shutil.copyfile(archive, combined)
    command = [str(local(Path(tools['llvm-ar']['path']))), 'q', str(combined), *objects]
    subprocess.run(command, env=environment, cwd=ROOT, stdin=subprocess.DEVNULL, check=True,
                   capture_output=True, text=True)
    return combined


def prepare_host_linker(preparation: Path, tool_pins: dict[str, dict[str, str]]) -> tuple[Path, Path]:
    work = local(preparation / 'host-linker')
    library = local(work / 'sdk/usr/lib')
    library.mkdir(parents=True, exist_ok=True)
    stub = local(ROOT / 'tools/nativeruntime/platform/darwin/libSystem.tbd')
    for name in ('libSystem.tbd', 'libc.tbd', 'libm.tbd'):
        target = local(library / name)
        target.unlink(missing_ok=True)
        os.link(stub, target)
    selected = {}
    for name in ('clang', 'ld64.lld'):
        pin = tool_pins[name]
        path = local(Path(pin['path']))
        if digest(path) != pin['sha256']:
            raise ValueError(f'prepared LLVM tool digest mismatch: {name}')
        selected[name] = path
    sdk, executable, object_path = local(work / 'sdk'), local(work / 'linker'), local(work / 'linker.o')
    commands = [
        [str(selected['clang']), '-target', 'aarch64-apple-darwin', '-mmacosx-version-min=27.0',
         '-nostdinc', '-fno-stack-protector',
         '-DLO_CLANG=' + json.dumps(str(selected['clang'])),
         '-DLO_LINKER=' + json.dumps(str(selected['ld64.lld'])), '-DLO_SYSROOT=' + json.dumps(str(sdk)),
         '-c', str(local(ROOT / 'tools/nativeruntime/host_linker.c')), '-o', str(object_path)],
        [str(selected['ld64.lld']), '-arch', 'arm64', '-platform_version', 'macos', '27.0', '27.0',
         '-o', str(executable), str(object_path), str(stub)]]
    for command in commands:
        result = subprocess.run(command, cwd=ROOT, env={'CI': '1', 'PATH': '', 'TMPDIR': str(work)},
                                stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False)
        if result.returncode:
            raise ValueError(f'host linker preparation failed: {result.stderr}')
    return executable, sdk


def build(preparation: Path, output: Path, targets: list[str], include_metal: bool = False) -> Path:
    if include_metal and 'darwin/arm64' not in targets:
        raise ValueError('Metal requires the darwin/arm64 runtime target')
    metal_inputs = metal_assets() if include_metal else None
    receipt = local(preparation / 'rust.json')
    if not receipt.is_file():
        raise ValueError(f'missing prepared Rust receipt: {receipt}')
    rust = json.loads(receipt.read_text())
    lock = json.loads(LOCK.read_text())
    expected_archives = [pin for pin in lock['artifacts'] if pin['archive'] in RUST_COMPONENTS]
    if rust['version'] != '1.85.1' or rust['archives'] != expected_archives:
        raise ValueError('prepared Rust receipt differs from repository pins')
    prefix = local(Path(rust['prefix']))
    if tree_digest(prefix) != lock['rust_prefix_sha256'] or rust['prefix_sha256'] != lock['rust_prefix_sha256']:
        raise ValueError('prepared Rust sysroot digest mismatch')
    if 'linux/arm64' in targets:
        from platforms import verify_platform
        verify_platform(preparation)
    source_digest = tree_digest(local(ROOT / 'runtime/native'))
    process_sources = {name: pin_file(local(ROOT / 'internal/processhost/native' / name))
                       for name in ('launcher.c', 'normalization.c', 'protocol.h')}
    tools = json.loads(local(preparation / 'tools.json').read_text())
    if tools['version'] != '21.1.8' or tools['archive'] != lock['artifacts'][0]:
        raise ValueError('prepared LLVM receipt differs from repository pins')
    if {name: pin['sha256'] for name, pin in tools['tools'].items()} != lock['llvm_tools']:
        raise ValueError('prepared LLVM member receipt differs from repository pins')
    for subject in ('rustc', 'cargo'):
        source = local(Path(rust[subject]['path']))
        if digest(source) != rust[subject]['sha256']:
            raise ValueError(f'prepared {subject} digest mismatch')
    compiler = local(Path(rust['rustc']['path']))
    cargo = local(Path(rust['cargo']['path']))
    output.mkdir(parents=True, exist_ok=True)
    local(output / 'toolchain.json').unlink(missing_ok=True)
    host_linker, sdk = prepare_host_linker(preparation, tools['tools'])
    environment = cargo_environment(preparation, compiler, host_linker, sdk)
    environment.update(native_c_environment(preparation, tools["tools"]))
    def copy_file(source: Path, relative: str) -> dict[str, str]:
        target = local(output / relative)
        target.parent.mkdir(parents=True, exist_ok=True)
        target.unlink(missing_ok=True)
        if relative.startswith('bin/'):
            os.link(source, target)
        else:
            shutil.copyfile(source, target)
        return {'path': relative, 'sha256': digest(target)}
    def tool(name: str) -> dict[str, str]:
        pin = tools['tools'][name]
        source = local(Path(pin['path']))
        if digest(source) != pin['sha256']:
            raise ValueError(f'prepared LLVM tool digest mismatch: {name}')
        return copy_file(source, 'bin/' + name)
    distribution: dict[str, object] = {'abi': 'lo-native-1', 'llvm': '21.1.8', 'host': 'darwin/arm64',
                                       'compiler': tool('clang'), 'targets': {}}
    target_records = {}
    for target in targets:
        triple = TARGETS[target]
        command = [str(cargo), 'build', '--frozen', '--offline', '--release', '--target', triple,
                   '--manifest-path', str(ROOT / 'runtime/native/Cargo.toml'),
                   '--config', str(ROOT / 'runtime/native/.cargo/config.toml')]
        result = subprocess.run(command, env=environment, cwd=ROOT, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, check=False)
        (output / (triple + '.build.json')).write_text(json.dumps({'command': command, 'exit_code': result.returncode,
                                                                  'stdout': result.stdout, 'stderr': result.stderr}, indent=2) + '\n')
        if result.returncode:
            raise ValueError(f'{triple} runtime build failed: {result.stderr}')
        archive = local(preparation / 'cargo-target' / triple / 'release/liblo_runtime.a')
        if target == 'darwin/arm64':
            archive = process_archive(preparation, tools['tools'], archive)
        runtime = copy_file(archive, 'runtime/' + triple + '/liblo_runtime.a')
        if target == 'darwin/arm64':
            inputs = {'system': copy_file(local(ROOT / 'tools/nativeruntime/platform/darwin/libSystem.tbd'),
                                          'platform/darwin/libSystem.tbd'),
                      'security': copy_file(local(ROOT / 'tools/nativeruntime/platform/darwin/Security.tbd'), 'platform/darwin/Security.tbd'),
                      'corefoundation': copy_file(local(ROOT / 'tools/nativeruntime/platform/darwin/CoreFoundation.tbd'), 'platform/darwin/CoreFoundation.tbd')}
            args = ['-dead_strip', '-arch', 'arm64', '-platform_version', 'macos', '27.0', '27.0',
                    '-o', '{output}', '{object}', '{runtime}', '{input:system}', '{input:security}', '{input:corefoundation}']
            linker = tool('ld64.lld')
        else:
            sysroot = local(preparation / 'linux-sysroot')
            names = {'crt1': 'usr/lib/aarch64-linux-gnu/crt1.o', 'crti': 'usr/lib/aarch64-linux-gnu/crti.o',
                     'crtn': 'usr/lib/aarch64-linux-gnu/crtn.o', 'libc': 'lib/aarch64-linux-gnu/libc.so.6',
                     'libm': 'lib/aarch64-linux-gnu/libm.so.6',
                     'libgcc': 'lib/aarch64-linux-gnu/libgcc_s.so.1',
                     'nonshared': 'usr/lib/aarch64-linux-gnu/libc_nonshared.a'}
            inputs = {name: copy_file(local(sysroot / path), 'platform/linux/' + Path(path).name)
                      for name, path in names.items()}
            args = ['-m', 'aarch64elf', '-dynamic-linker', '/lib/ld-linux-aarch64.so.1',
                    '-o', '{output}', '{input:crt1}', '{input:crti}', '{object}', '{runtime}',
                    '{input:libgcc}', '{input:libm}', '{input:libc}', '{input:nonshared}', '{input:crtn}']
            linker = tool('ld.lld')
        target_records[target] = {'triple': triple, 'linker': linker, 'runtime': runtime,
                                  'inputs': inputs, 'link_args': args}
    distribution['targets'] = target_records
    if metal_inputs is not None:
        library, identity, metal_provenance = metal_inputs
        triple = TARGETS['darwin/arm64']
        command = [str(cargo), 'build', '--frozen', '--offline', '--release', '--target', triple,
                   '--features', 'metal', '--config', str(local(ROOT / 'runtime/native/.cargo/config.toml')),
                   '--manifest-path', str(local(ROOT / 'runtime/native/Cargo.toml'))]
        result = subprocess.run(command, cwd=ROOT, env=environment, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True, check=False)
        (preparation / 'cargo-metal.json').write_text(json.dumps({'command': command, 'environment': environment,
                                                                'status': result.returncode,
                                                                'stdout': result.stdout, 'stderr': result.stderr}, indent=2) + '\n')
        if result.returncode:
            raise ValueError(f'Metal runtime build failed: {result.stderr}')
        archive = process_archive(preparation, tools['tools'],
                                  local(preparation / 'cargo-target' / triple / 'release/liblo_runtime.a'))
        archive, kernel = metal_archive(preparation, tools['tools'], archive, library, identity)
        metal_record = dict(target_records['darwin/arm64'])
        metal_record['runtime'] = copy_file(archive, 'runtime/' + triple + '/metal/liblo_runtime.a')
        metal_record['inputs'] = {}
        for name, filename in (('system', 'libSystem.tbd'), ('security', 'Security.tbd'),
                               ('corefoundation', 'CoreFoundation.tbd'), ('foundation', 'Foundation.tbd'),
                               ('metal', 'Metal.tbd'), ('objc', 'libobjc.tbd')):
            metal_record['inputs'][name] = copy_file(local(ROOT / 'tools/nativeruntime/platform/darwin/metal' / filename),
                                                     'platform/darwin/metal/' + filename)
        metal_record['link_args'] = metal_record['link_args'] + ['{input:foundation}', '{input:metal}', '{input:objc}']
        metal_record['identity'] = identity
        metal_record['kernel'] = copy_file(kernel, 'runtime/' + triple + '/metal/metal.metallib')
        metal_record['target'] = 'darwin/arm64'
        distribution['metal'] = metal_record
    manifest = local(output / 'toolchain.json')
    if tree_digest(local(ROOT / 'runtime/native')) != source_digest:
        raise ValueError('runtime source changed during build')
    if process_sources != {name: pin_file(local(ROOT / 'internal/processhost/native' / name))
                           for name in process_sources}:
        raise ValueError('process source changed during build')
    if metal_inputs is not None and metal_assets()[2] != metal_provenance:
        raise ValueError('Metal producer inputs changed during build')
    manifest.write_text(json.dumps(distribution, indent=2) + '\n')
    provenance = {'runtime_source_sha256': source_digest, 'rust': rust,
                  'llvm': tools, 'cargo_environment': environment, 'manifest_sha256': digest(manifest),
                  'process_sources': process_sources,
                  'host_linker': {'executable': pin_file(host_linker),
                                  'source': pin_file(local(ROOT / 'tools/nativeruntime/host_linker.c')),
                                  'sdk_sha256': tree_digest(sdk)}}
    if metal_inputs is not None:
        provenance['metal'] = metal_provenance
    (output / 'build-receipt.json').write_text(json.dumps(provenance, indent=2) + '\n')
    return manifest


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--preparation', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--target', choices=tuple(TARGETS), action='append')
    parser.add_argument('--metal', action='store_true')
    arguments = parser.parse_args()
    try:
        validate_build_environment()
        preparation = local(arguments.preparation)
        if not (preparation / 'rust.json').is_file():
            raise ValueError(f'missing prepared Rust receipt: {preparation / "rust.json"}')
        with local(preparation / 'runtime-build.lock').open('a+b') as build_lock:
            try:
                fcntl.flock(build_lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError as error:
                raise ValueError('another runtime builder owns this preparation state') from error
            print(json.dumps({'toolchain': str(build(preparation, local(arguments.output),
                                                    arguments.target or list(TARGETS), arguments.metal).relative_to(ROOT))}))
        return 0
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError) as error:
        print(f'native runtime build: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
