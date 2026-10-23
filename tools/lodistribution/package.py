"""Construct an immutable, relocatable compiler package from prepared inputs."""
from __future__ import annotations
import argparse
import copy
import io
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tomllib

from prepare import ROOT, DEPENDENCIES, local, digest, verified, tree_digest, validate_prepared

HOSTS = ('darwin/arm64', 'linux/arm64')
VARIANTS = ('software', 'metal')
TRIPLES = {'darwin/arm64': 'aarch64-apple-darwin', 'linux/arm64': 'aarch64-unknown-linux-gnu'}
LINUX_HOST_LIBRARIES = {'libstdc++.so.6', 'libgcc_s.so.1', 'libxml2.so.2', 'libicuuc.so.72',
                        'libicudata.so.72', 'liblzma.so.5', 'libz.so.1'}


def write_json(path: Path, value: object) -> None:
    path.write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')


def copy_file(source: Path, destination: Path) -> None:
    local(destination).parent.mkdir(parents=True, exist_ok=True)
    # Standard archive copying retains executable tool behavior without permission handling.
    buffer = io.BytesIO()
    with tarfile.open(fileobj=buffer, mode='w') as archive:
        archive.add(source, arcname=destination.name, recursive=False)
    buffer.seek(0)
    with tarfile.open(fileobj=buffer) as archive:
        archive.extractall(destination.parent, filter='data')


def materialize(record: dict[str, str], destination: Path) -> dict[str, str]:
    source = verified(record)
    copy_file(source, destination)
    if digest(destination) != record['sha256']:
        raise ValueError(f'copied input digest differs: {destination}')
    return {'path': '', 'sha256': record['sha256']}


PACKAGED_SOURCE_ROOTS = ('cmd', 'internal', 'runtime', 'tools/metalbuild', 'tools/nativeruntime/platform', 'go.mod', 'go.sum', 'LICENSE')


def source_snapshot() -> dict[str, str]:
    files = {}
    for name in PACKAGED_SOURCE_ROOTS:
        path = local(ROOT / name)
        for entry in ([path] if path.is_file() else sorted(path.rglob('*'))):
            if entry.is_file():
                files[entry.relative_to(ROOT).as_posix()] = digest(local(entry))
    return files


def source_identity(files: dict[str, str]) -> dict:
    return {'sha256': hashlib.sha256(json.dumps(files, sort_keys=True, separators=(',', ':')).encode()).hexdigest(),
            'files': files}


def source_archive(output: Path, snapshot: dict[str, str]) -> None:
    """Archive exactly the source files selected before construction."""
    with tarfile.open(output, 'w:gz') as archive:
        for name in sorted(snapshot, key=Path):
            archive.add(local(ROOT / name), arcname=name, recursive=False)


def validate_source_archive(path: Path, receipt: dict, variant: str, snapshot: dict[str, str]) -> None:
    """Verify the shipped source bytes against the runtime build receipt."""
    with tarfile.open(path, 'r:gz') as archive:
        runtime_files = sorted((member for member in archive.getmembers()
                                if member.isfile() and member.name.startswith('runtime/native/')), key=lambda member: Path(member.name))
        hasher = hashlib.sha256()
        members = {member.name: member for member in archive.getmembers() if member.isfile()}
        def member_digest(name: str) -> str:
            with archive.extractfile(members[name]) as source:
                return hashlib.file_digest(source, 'sha256').hexdigest()
        if set(members) != set(snapshot) or len(archive.getmembers()) != len(snapshot):
            raise ValueError('packaged source inventory differs from construction snapshot')
        for name, sha256 in snapshot.items():
            if member_digest(name) != sha256:
                raise ValueError(f'packaged source differs from construction snapshot: {name}')
        for member in runtime_files:
            relative = member.name.removeprefix('runtime/native/')
            hasher.update(relative.encode() + b'\0')
            hasher.update(bytes.fromhex(member_digest(member.name)))
        if hasher.hexdigest() != receipt['runtime_source_sha256']:
            raise ValueError('packaged runtime source differs from build receipt')
        records = list(receipt['process_sources'].values())
        if variant == 'metal':
            records.extend(receipt['metal']['sources'].values())
        for record in records:
            if member_digest(record['path']) != record['sha256']:
                raise ValueError(f'packaged source differs from runtime receipt: {record["path"]}')


def notices(output: Path, prepared: dict, host: str) -> list[dict]:
    directory = local(output / 'notices')
    directory.mkdir()
    components: list[dict] = []
    inventories = json.loads((ROOT / 'experiments/native-backends/notices/inventory.json').read_text())
    selected = [record for record in inventories if record['archive'] in ('llvm.tar.xz', 'rust-std.tar.xz', 'libc6.deb', 'libc6-dev.deb')]
    for record in selected:
        copied = []
        for notice in record['notice_files']:
            path = verified(notice)
            destination = directory / path.relative_to(ROOT / 'experiments/native-backends/notices')
            copy_file(path, destination)
            copied.append({'path': destination.relative_to(output).as_posix(), 'sha256': digest(destination)})
        components.append({'component': record['archive'], 'license': record['declared_license'],
                           'origin': (prepared['archives'].get('llvm-linux', {}).get('url', record['url']) if host == 'linux/arm64' and record['archive'] == 'llvm.tar.xz' else record['url']), 'notices': copied})
    dependencies = json.loads(DEPENDENCIES.read_text())
    for record in dependencies['notices']:
        # Every host supplies target libc and GCC support; extra libraries ship only on Linux hosts.
        if host == 'darwin/arm64' and record['package'] not in ('gcc-12-base', 'libc6'):
            continue
        source = verified(record)
        destination = directory / 'debian' / source.name
        copy_file(source, destination)
        components.append({'component': record['package'], 'origin': 'Debian bookworm arm64',
                           'notices': [{'path': destination.relative_to(output).as_posix(), 'sha256': digest(destination)}]})
    copy_file(ROOT / 'runtime/native/licenses/go-http-sniff-LICENSE', directory / 'Go-LICENSE')
    copy_file(ROOT / 'LICENSE', directory / 'Lo-LICENSE')
    components.extend([{'component': 'Go standard library', 'license': 'BSD-3-Clause', 'notices': ['notices/Go-LICENSE']},
                       {'component': 'Lo frontend, runtime, TAPI and Metal sources', 'license': 'Apache-2.0',
                        'notices': ['notices/Lo-LICENSE'], 'sources': 'source/lo-source.tar.gz'}])
    lock = tomllib.loads((ROOT / 'runtime/native/Cargo.lock').read_text())
    # The complete lockfile and vendor tree are supplied as corresponding source.
    for package in lock['package']:
        name = package['name']
        if name.startswith('windows') or name in {'wasi', 'schannel', 'lo-native-runtime', 'cc', 'shlex', 'find-msvc-tools', 'autocfg', 'proc-macro2', 'quote', 'syn', 'unicode-ident', 'serde_derive'}:
            continue
        matches = [path for path in (ROOT / 'runtime/native/vendor').glob(name + '*') if path.is_dir() and tomllib.loads((path / 'Cargo.toml').read_text())['package']['name'] == name]
        if len(matches) != 1:
            raise ValueError(f'crate source is not unique: {name}')
        metadata = tomllib.loads((matches[0] / 'Cargo.toml').read_text())['package']
        crate_notices = []
        for path in sorted(matches[0].rglob('*')):
            if path.is_file() and (path.name.startswith(('LICENSE', 'COPYING', 'NOTICE', 'COPYRIGHT'))):
                target = directory / 'crates' / name / path.relative_to(matches[0])
                copy_file(local(path), target)
                crate_notices.append({'path': target.relative_to(output).as_posix(), 'sha256': digest(target)})
        components.append({'component': name, 'version': package['version'], 'license': metadata.get('license', metadata.get('license-file')),
                           'origin': package.get('source'), 'checksum': package.get('checksum'), 'notices': crate_notices,
                           'sources': 'source/lo-source.tar.gz'})
    module = ROOT / '_scratch/build-state/modules/golang.org/x/sys@v0.49.0'
    copy_file(local(module / 'LICENSE'), directory / 'golang-x-sys-LICENSE')
    components.append({'component': 'golang.org/x/sys', 'version': 'v0.49.0', 'license': 'BSD-3-Clause',
                       'notices': ['notices/golang-x-sys-LICENSE'], 'origin': 'https://go.googlesource.com/sys'})
    return components


PROCESS_SOURCES = {'launcher.c', 'normalization.c', 'protocol.h'}
METAL_SOURCES = {
    'internal/device/metal_bridge.h',
    'internal/device/metal_bridge_darwin.m',
    'internal/device/metal_darwin.go',
    'internal/device/metal_types.h',
    'internal/metalconfig/config.go',
    'runtime/metal/embed.go',
    'runtime/metal/kernels.metal',
    'runtime/metal/manifest.json',
    'runtime/metal/metal.metallib',
    'tools/metalbuild/main.go',
}


def validate_runtime(runtime: Path, variant: str) -> dict:
    receipt = json.loads(local(runtime / 'build-receipt.json').read_text())
    if digest(local(runtime / 'toolchain.json')) != receipt['manifest_sha256']:
        raise ValueError('runtime manifest digest differs from build receipt')
    if tree_digest(local(ROOT / 'runtime/native')) != receipt['runtime_source_sha256']:
        raise ValueError('runtime source digest differs from build receipt')
    if set(receipt['process_sources']) != PROCESS_SOURCES:
        raise ValueError('runtime process source inventory differs')
    for name, record in receipt['process_sources'].items():
        if record['path'] != 'internal/processhost/native/' + name:
            raise ValueError(f'runtime process source path differs: {name}')
        verified(record)
    if variant == 'metal':
        metal = receipt['metal']
        if set(metal['sources']) != METAL_SOURCES:
            raise ValueError('runtime Metal source inventory differs')
        for name, record in metal['sources'].items():
            if record['path'] != name:
                raise ValueError(f'runtime Metal source path differs: {name}')
            verified(record)
        for key, name in [('manifest', 'runtime/metal/manifest.json'),
                          ('producer', 'bin/lo-metal-build.json')]:
            if metal[key]['path'] != name:
                raise ValueError(f'runtime Metal {key} path differs')
            verified(metal[key])
        source_manifest = json.loads(verified(metal['manifest']).read_text())
        runtime_manifest = json.loads(local(runtime / 'toolchain.json').read_text())
        if runtime_manifest['metal']['identity'] != source_manifest['identity']:
            raise ValueError('runtime Metal identity differs from source assets')
        if runtime_manifest['metal']['kernel']['sha256'] != source_manifest['identity']['kernel_sha256']:
            raise ValueError('runtime Metal kernel differs from source assets')
    return receipt


def construct(prepared_path: Path, runtime: Path, host: str, variant: str, go: Path, output: Path) -> dict:
    if variant == 'metal' and host != 'darwin/arm64':
        raise ValueError('Metal requires darwin/arm64')
    archive = local(output.with_suffix('.tar'))
    receipt_path = local(output.with_suffix('.receipt.json'))
    if output.exists() or archive.exists() or receipt_path.exists():
        raise ValueError(f'package output already exists: {output}')
    prepared = json.loads(prepared_path.read_text())
    for record in prepared['tools'][host].values():
        verified(record)
    for record in prepared.get('host_libraries', {}).values():
        verified(record)
    for record in prepared['sources']:
        verified(record)
    validate_prepared(prepared)
    runtime_receipt = validate_runtime(runtime, variant)
    manifest = copy.deepcopy(json.loads(local(runtime / 'toolchain.json').read_text()))
    if manifest.get('abi') != 'lo-native-2' or manifest.get('llvm') != '21.1.8':
        raise ValueError('runtime manifest ABI or LLVM identity is incompatible')
    if set(manifest['targets']) != set(HOSTS):
        raise ValueError('runtime manifest must supply both arm64 targets')
    for name, target in manifest['targets'].items():
        if target['triple'] != TRIPLES[name]:
            raise ValueError(f'runtime target triple is incompatible: {name}')
    if variant == 'metal' and 'metal' not in manifest:
        raise ValueError('runtime manifest lacks the required Metal profile')
    if host == 'linux/arm64' and set(prepared['host_libraries']) != LINUX_HOST_LIBRARIES:
        raise ValueError('prepared Linux host-library closure is incompatible')
    manifest['host'] = host
    if variant == 'software':
        manifest.pop('metal', None)
    manifest['host_libraries'] = {}
    records = [manifest['compiler']]
    for target in manifest['targets'].values():
        records.extend([target['linker'], target['runtime'], *target['inputs'].values()])
    if 'metal' in manifest:
        metal = manifest['metal']
        records.extend([metal['linker'], metal['runtime'], metal['kernel'], *metal['inputs'].values()])
    selected_tools = prepared['tools'][host]
    for record in records:
        relative = Path(record['path'])
        if relative.is_absolute() or '..' in relative.parts or not relative.parts:
            raise ValueError(f'invalid package-relative path: {relative}')
        if record['path'].startswith('bin/'):
            name = Path(record['path']).name
            record['sha256'] = selected_tools[name]['sha256']
            verified(selected_tools[name])
        else:
            verified({'path': str((runtime / record['path']).relative_to(ROOT)), 'sha256': record['sha256']})
    snapshot = source_snapshot()
    output.parent.mkdir(parents=True, exist_ok=True)
    temporary = local(output.with_name(output.name + '.building'))
    if temporary.exists():
        raise ValueError(f'unfinished package output exists: {temporary}')
    temporary.mkdir()
    state = local(prepared_path.parent / 'producer-state')
    for name in ('home', 'tmp', 'go-cache', 'go-path', 'xdg-cache'):
        local(state / name).mkdir(parents=True, exist_ok=True)
    env = dict(PATH='', LC_ALL='C', TZ='UTC', CI='1', CGO_ENABLED='0', GOOS=host.split('/')[0], GOARCH='arm64',
               GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOTELEMETRY='off', GOPROXY='off',
               GOSUMDB='off', GOCACHE=str(state / 'go-cache'), GOPATH=str(state / 'go-path'),
               GOMODCACHE=str(ROOT / '_scratch/build-state/modules'), HOME=str(state / 'home'),
               TMPDIR=str(state / 'tmp'), TMP=str(state / 'tmp'), TEMP=str(state / 'tmp'),
               XDG_CACHE_HOME=str(state / 'xdg-cache'))
    try:
        for record in records:
            relative = Path(record['path'])
            source = verified(selected_tools[relative.name]) if relative.parts[0] == 'bin' else local(runtime / relative)
            destination = local(temporary / relative)
            if not destination.exists():
                copy_file(source, destination)
                if digest(destination) != record['sha256']:
                    raise ValueError(f'copied input digest differs: {destination}')
        if host == 'linux/arm64':
            for name, record in prepared['host_libraries'].items():
                destination = local(temporary / 'host/lib' / name)
                materialize(record, destination)
                manifest['host_libraries'][name] = {'path': destination.relative_to(temporary).as_posix(), 'sha256': digest(destination)}
        write_json(temporary / 'toolchain.json', manifest)
        identity = digest(temporary / 'toolchain.json')
        frontend = local(temporary / 'bin/lo')
        result = subprocess.run([str(go), 'build', '-mod=readonly', '-trimpath', '-buildvcs=false',
                                 '-ldflags', '-X main.installedToolchainSHA256=' + identity,
                                 '-o', str(frontend), './cmd/lo'], cwd=ROOT, env=env,
                                stdin=subprocess.DEVNULL, capture_output=True, text=True)
        if result.returncode:
            raise ValueError(f'frontend build failed: {result.stderr}')
        identity_result = subprocess.run([str(go), 'version'], cwd=ROOT, env=env, stdin=subprocess.DEVNULL, capture_output=True, text=True, check=True)
        components = notices(temporary, prepared, host)
        source_dir = local(temporary / 'source')
        source_dir.mkdir()
        if source_snapshot() != snapshot:
            raise ValueError('source files changed during package construction')
        source_archive(source_dir / 'lo-source.tar.gz', snapshot)
        validate_source_archive(source_dir / 'lo-source.tar.gz', runtime_receipt, variant, snapshot)
        if source_snapshot() != snapshot:
            raise ValueError('source files changed during package construction')
        if validate_runtime(runtime, variant) != runtime_receipt:
            raise ValueError('runtime receipt changed during package construction')
        shipped_sources = {'gcc-12', 'glibc'} if host == 'darwin/arm64' else {record['source'] for record in prepared['sources']}
        sources = []
        for record in prepared['sources']:
            if record['source'] in shipped_sources:
                destination = source_dir / Path(record['path']).name
                materialize(record, destination)
                sources.append(dict(record, path=destination.relative_to(temporary).as_posix()))
        provenance = {'host': host, 'variant': variant, 'llvm': prepared['archives'],
                      'source_identity': source_identity(snapshot),
                      'frontend_source_identity': source_identity({name: sha256 for name, sha256 in snapshot.items() if name.startswith(('cmd/', 'internal/')) or name in ('go.mod', 'go.sum')}),
                      'frontend_producer': {'identity': identity_result.stdout.strip(), 'sha256': digest(go), 'cgo_enabled': False, 'trimpath': True},
                      'runtime_receipt': runtime_receipt,
                      'elf_dependencies': prepared['elf_dependencies'] if host == 'linux/arm64' else {},
                      'sources': sources, 'components': components}
        write_json(temporary / 'provenance.json', provenance)
        inventory = {path.relative_to(temporary).as_posix(): digest(local(path)) for path in sorted(temporary.rglob('*')) if path.is_file()}
        write_json(temporary / 'inventory.json', {'sha256': hashlib.sha256(json.dumps(inventory, sort_keys=True).encode()).hexdigest(), 'files': inventory})
        temporary.replace(output)
        with tarfile.open(archive, 'w') as package:
            package.add(output, arcname=output.name)
        result = {'package': str(output.relative_to(ROOT)), 'archive': str(archive.relative_to(ROOT)),
                  'archive_sha256': digest(archive), 'toolchain_sha256': identity,
                  'inventory_sha256': digest(output / 'inventory.json'), 'host': host, 'variant': variant}
        write_json(receipt_path, result)
        return result
    except BaseException:
        if temporary.exists():
            shutil.rmtree(temporary)
        if output.exists():
            shutil.rmtree(output)
        archive.unlink(missing_ok=True)
        if receipt_path.exists():
            receipt_path.unlink()
        raise


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('--prepared', type=Path, required=True)
    parser.add_argument('--runtime', type=Path, required=True)
    parser.add_argument('--host', choices=HOSTS, required=True)
    parser.add_argument('--variant', choices=VARIANTS, required=True)
    parser.add_argument('--go', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        print(json.dumps(construct(local(args.prepared), local(args.runtime), args.host, args.variant, args.go.resolve(), local(args.output))))
        return 0
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError) as error:
        print(f'distribution package: {error}', file=sys.stderr)
        return 1

if __name__ == '__main__':
    sys.exit(main())
