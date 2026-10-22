"""Qualify installed compiler packages with isolated public commands."""
from __future__ import annotations

import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import time

from prepare import ROOT, digest, local, verified
from guest import compile as guest_compile

CORE = '''flow Recover(e: Error) -> String { return e.kind }
flow Handled() -> String onError Recover { x = Math.div(1, 0); return Int.to_string(x) }
flow Main() -> Record{large: Int, text: String, count: Int, tensor: Float, error: String}! {
 values = List.new(2, 4, 8)
 tensor = Tensor.new(List.new(1.0, 2.0, 3.0), List.new(3))
 total = Tensor.to_float(Tensor.sum(tensor, List.new(0)))
 return Record.new(large: Math.add(123456789012345678901234567890, 1), text: "λ雪", count: List.length(values), tensor: total, error: Handled())
}
return Main()
'''


def environment(work: Path) -> dict[str, str]:
    for name in ('home', 'tmp', 'cache', 'work', 'build'):
        (work / name).mkdir(parents=True, exist_ok=True)
    return {'CI': '1', 'PATH': '', 'HOME': str(work / 'home'), 'TMPDIR': str(work / 'tmp'),
            'TMP': str(work / 'tmp'), 'TEMP': str(work / 'tmp'), 'XDG_CACHE_HOME': str(work / 'cache'),
            'LO_BUILD_ROOT': str(work / 'build'), 'LANG': 'C', 'LC_ALL': 'C'}


def execute(command: list[str], work: Path, env: dict[str, str], timeout: int = 240) -> dict:
    start = time.monotonic()
    result = subprocess.run(command, cwd=work, env=env, stdin=subprocess.DEVNULL,
                            capture_output=True, text=True, timeout=timeout, check=False)
    return {'status': result.returncode, 'stdout': result.stdout, 'stderr': result.stderr,
            'seconds': round(time.monotonic() - start, 3)}


def require_success(result: dict, label: str) -> None:
    if result['status'] or result['stderr']:
        raise ValueError(f'{label}: {result}')


def install(archive: Path, destination: Path) -> Path:
    receipt = json.loads(archive.with_suffix('.receipt.json').read_text())
    if digest(archive) != receipt['archive_sha256']:
        raise ValueError('package archive SHA-256 mismatch')
    destination.mkdir(parents=True)
    with tarfile.open(archive) as package:
        for member in package:
            path = Path(member.name)
            if path.is_absolute() or '..' in path.parts or member.issym() or member.islnk():
                raise ValueError(f'nonlocal package member: {member.name}')
        package.extractall(destination, filter='data')
    roots = list(destination.iterdir())
    if len(roots) != 1 or not (roots[0] / 'bin/lo').is_file():
        raise ValueError('package needs one root and its compiler entry point')
    relocated = destination / 'relocated compiler package'
    roots[0].rename(relocated)
    if digest(relocated / 'inventory.json') != receipt['inventory_sha256']:
        raise ValueError('package inventory SHA-256 mismatch')
    inventory = json.loads((relocated / 'inventory.json').read_text())
    actual = {path.relative_to(relocated).as_posix(): digest(path)
              for path in relocated.rglob('*') if path.is_file() and path.name != 'inventory.json'}
    if actual != inventory['files'] or hashlib.sha256(json.dumps(actual, sort_keys=True).encode()).hexdigest() != inventory['sha256']:
        raise ValueError('installed package inventory differs')
    return relocated


def profile(path: Path, readable: list[Path], writable: Path) -> None:
    roots = [item.resolve() for item in readable] + [writable.resolve()]
    parents = sorted({str(parent) for item in roots for parent in item.parents})
    text = ['(version 1)', '(deny default (with no-log))', '(allow process-fork process-exec)',
            '(allow sysctl-read)', '(allow process-info* (target self))',
            '(allow file-read* (subpath "/usr/lib") (subpath "/System/Library"))',
            '(allow file-read* (literal "/dev/null") (literal "/dev/random") (literal "/dev/urandom"))',
            '(allow file-read-data (literal "/"))',
            '(allow file-read-metadata file-test-existence ' + ' '.join('(literal ' + json.dumps(item) + ')' for item in parents) + ')',
            '(allow file-read* ' + ' '.join('(subpath ' + json.dumps(str(item)) + ')' for item in roots) + ')',
            '(allow file-write* (subpath ' + json.dumps(str(writable.resolve())) + '))',
            '(deny network* (with no-log))']
    path.write_text('\n'.join(text) + '\n')


def sandbox(command: list[str], readable: list[Path], work: Path) -> dict:
    env = environment(work)
    policy = work / 'isolation.sb'
    profile(policy, readable, work)
    return execute(['/usr/bin/sandbox-exec', '-f', str(policy), *command], work / 'work', env)


def build_guard(preparation: Path, output: Path) -> Path:
    output.mkdir(parents=True)
    tools = json.loads((preparation / 'tools.json').read_text())['tools']
    clang, linker = verified(tools['clang']), verified(tools['ld64.lld'])
    obj, binary = output / 'guard.o', output / 'guard'
    for command in [[str(clang), '-target', 'aarch64-apple-darwin', '-mmacosx-version-min=27.0', '-nostdinc', '-fno-stack-protector', '-c', str(ROOT / 'tools/lodistribution/guard.c'), '-o', str(obj)],
                    [str(linker), '-arch', 'arm64', '-platform_version', 'macos', '27.0', '27.0', '-o', str(binary), str(obj), str(ROOT / 'tools/nativeruntime/platform/darwin/libSystem.tbd')]]:
        require_success(execute(command, output, environment(output)), 'guard preparation')
    return binary


def check_core(result: dict) -> None:
    require_success(result, 'representative artifact')
    value = json.loads(result['stdout'])['result']['value']['value']
    fields = {name: item['value'] for name, item in value.items()}
    expected = {'large': '123456789012345678901234567891', 'text': 'λ雪', 'count': '3', 'tensor': '6', 'error': 'Math.DivideByZero'}
    if fields != expected:
        raise ValueError(f'independent representative expectation differs: {fields}')


def darwin_compile(package: Path, source: Path, target: str, work: Path, extra: list[str] | None = None) -> tuple[Path, dict]:
    work.mkdir(parents=True)
    executable = work / 'program'
    command = [str(package / 'bin/lo'), 'build', str(source), '--output', str(executable), '--target', target, '--format', 'json', *(extra or [])]
    result = sandbox(command, [package, source], work)
    require_success(result, 'installed Darwin compiler')
    if json.loads(result['stdout'])['codegen'] != 'llvm':
        raise ValueError('installed build selected a different code generator')
    if list((work / 'tmp').iterdir()) or list((work / 'build/work').iterdir()) or list(work.glob('.lo-*')):
        raise ValueError('installed build retained temporary files')
    return executable, result


def check_guest_compile(result: dict) -> None:
    require_success(result, 'installed Linux compiler')
    if result['receipt']['codegen'] != 'llvm' or '/result/program' not in result['entries'] or not result['artifact']:
        raise ValueError('Linux compiler did not produce the expected native artifact')
    artifact = Path(result['artifact'])
    if artifact.stat().st_size != result['artifact_bytes'] or digest(artifact) != result['artifact_sha256']:
        raise ValueError('Linux artifact transport changed the exported bytes')
    check_guest_cleanup(result)


def check_guest_cleanup(result: dict) -> None:
    allowed = {'/result/program', *('/tmp/' + name for name in ('work', 'go-build', 'modules', 'gopath', 'cache', 'clang', 'ccache'))}
    if set(result['entries']) - allowed:
        raise ValueError(f'Linux build retained temporary files: {result["entries"]}')


def darwin_rejections(package: Path, source: Path, work: Path, metal: bool = False) -> list[dict]:
    manifest = json.loads((package / 'toolchain.json').read_text())
    selected = manifest['metal'] if metal else manifest['targets']['darwin/arm64']
    cases = [('manifest-missing', 'toolchain.json', True),
             ('manifest-corrupt', 'toolchain.json', False),
             ('compiler-missing', manifest['compiler']['path'], True),
             ('compiler-corrupt', manifest['compiler']['path'], False),
             ('linker-corrupt', selected['linker']['path'], False),
             ('runtime-corrupt', selected['runtime']['path'], False),
             ('platform-corrupt', selected['inputs']['system']['path'], False)]
    if metal:
        cases = [('kernel-corrupt', selected['kernel']['path'], False)]
    results = []
    for name, relative, missing in cases:
        state = work / name
        state.mkdir(parents=True)
        output = state / 'program'
        output.write_bytes(b'previous artifact')
        path = package / relative
        saved = path.with_name(path.name + '.saved')
        with path.open('rb') as original:
            first = original.read(1)
        if missing:
            path.rename(saved)
        else:
            with path.open('r+b') as changed:
                changed.write(bytes([first[0] ^ 1]))
        try:
            command = [str(package / 'bin/lo'), 'build', str(source), '--output', str(output), '--format', 'json']
            if metal:
                command.extend(['--device-backend', 'metal'])
            result = sandbox(command, [package, source], state)
            if result['status'] != 2 or result['stdout'] or 'build.toolchain' not in result['stderr']:
                raise ValueError(f'{name} did not reject its unadmitted input: {result}')
            if output.read_bytes() != b'previous artifact' or list(state.glob('.lo-*')) or list((state / 'build/work').iterdir()):
                raise ValueError(f'{name} changed the existing artifact or retained staging files')
            results.append(dict(result, case=name, prior_output_preserved=True, staging_empty=True))
        finally:
            if missing:
                saved.rename(path)
            else:
                with path.open('r+b') as restored:
                    restored.write(first)
    return results


def check_training(result: dict) -> None:
    require_success(result, 'XOR training artifact')
    fields = json.loads(result['stdout'])['result']['value']['value']
    if fields['steps']['value'] != '500' or not (float(fields['loss']['value']) < 1e-10 < float(fields['initial_loss']['value'])):
        raise ValueError('XOR independent loss and step expectations differ')
    predictions = fields['predictions']['value']['values']
    if len(predictions) != 4 or any(abs(float(value) - expected) > 1e-4 for value, expected in zip(predictions, (0, 1, 1, 0))):
        raise ValueError(f'XOR classification differs: {predictions}')


def darwin_run(executable: Path, work: Path) -> dict:
    work.mkdir(parents=True)
    # Ordinary archive copy supplies a separate execution root.
    target = work / 'application'
    with tarfile.open(work / 'application.tar', 'w') as archive:
        archive.add(executable, arcname=target.name, recursive=False)
    with tarfile.open(work / 'application.tar') as archive:
        archive.extractall(work, filter='data')
    return sandbox([str(target), 'run', '--format', 'json'], [target], work)


def transport(template: Path, executable: Path) -> None:
    # Use ordinary artifact transport as the destination for the guest's byte export.
    archive_path = executable.with_name(executable.name + '.transport.tar')
    with tarfile.open(archive_path, 'w') as archive:
        archive.add(template, arcname=executable.name, recursive=False)
    with tarfile.open(archive_path) as archive:
        archive.extractall(executable.parent, filter='data')
    archive_path.unlink()


def linux_run(preparation: Path, executable: Path, work: Path) -> dict:
    work.mkdir(parents=True)
    env = environment(work)
    # The executable-only guest contains OS libraries and the copied artifact.
    result = execute([sys.executable, '-B', str(ROOT / 'tools/nativeruntime/run.py'), '--preparation', str(preparation), str(executable), 'run', '--format', 'json'], work, env)
    return result


def qualify(darwin: Path, linux: Path, metal: Path, preparation: Path, output: Path) -> dict:
    if output.exists():
        raise ValueError('acceptance output must be fresh')
    output.mkdir(parents=True)
    sources = output / 'sources'
    sources.mkdir()
    core = sources / 'representative.lo'
    core.write_text(CORE)
    installed = {name: install(package.with_suffix('.tar'), output / (name + '-install'))
                 for name, package in [('darwin', darwin), ('linux', linux), ('metal', metal)]}
    guard = build_guard(preparation, output / 'helpers')
    denied = output / 'readonly-sentinel'
    denied.write_text('unchanged')
    guard_work = output / 'guard'
    guard_work.mkdir()
    allowed = guard_work / 'write-test'
    allowed.write_text('before')
    restrictions = sandbox([str(guard), str(denied), str(allowed), str(ROOT / 'go.mod'), str(preparation / 'tools/bin/clang'), '/usr/bin/clang'], [guard], guard_work)
    require_success(restrictions, 'Darwin isolation guard')
    if denied.read_text() != 'unchanged' or allowed.read_text() != 'okfore':
        raise ValueError('guard state differs')
    results = {'guard': restrictions, 'matrix': [], 'packages': {
        name: dict(json.loads(package.with_suffix('.receipt.json').read_text()),
                   frontend_sha256=digest(installed[name] / 'bin/lo'))
        for name, package in [('darwin', darwin), ('linux', linux), ('metal', metal)]}}
    template = None
    for target in ('darwin/arm64', 'linux/arm64'):
        name = target.split('/')[0]
        executable, compiled = darwin_compile(installed['darwin'], core, target, output / ('darwin-to-' + name))
        if target == 'darwin/arm64':
            template = executable
            result = darwin_run(executable, output / ('darwin-run-' + name))
        else:
            result = linux_run(preparation, executable, output / ('darwin-run-' + name))
        check_core(result)
        results['matrix'].append({'host': 'darwin/arm64', 'target': target, 'compile': compiled, 'execute': result, 'sha256': digest(executable)})
    for target in ('linux/arm64', 'darwin/arm64'):
        name = target.split('/')[0]
        executable = output / ('linux-' + name + '-program')
        transport(template, executable)
        compiled = guest_compile(preparation, installed['linux'], core, target, executable, output / ('linux-to-' + name))
        check_guest_compile(compiled)
        result = linux_run(preparation, executable, output / ('linux-run-' + name)) if name == 'linux' else darwin_run(executable, output / ('linux-run-' + name))
        check_core(result)
        results['matrix'].append({'host': 'linux/arm64', 'target': target, 'compile': compiled, 'execute': result, 'sha256': digest(executable)})
    executable, compiled = darwin_compile(installed['metal'], core, 'darwin/arm64', output / 'metal-build', ['--device-backend', 'metal'])
    result = darwin_run(executable, output / 'metal-run')
    check_core(result)
    results['metal'] = {'compile': compiled, 'execute': result}
    results['rejections'] = darwin_rejections(installed['darwin'], core, output / 'darwin-rejections')
    results['rejections'].extend(darwin_rejections(installed['metal'], core, output / 'metal-rejections', metal=True))
    # The installed Linux package must reject a corrupt transitive tool library.
    library = installed['linux'] / 'host/lib/libz.so.1'
    with library.open('rb') as stream:
        first = stream.read(1)
    with library.open('r+b') as stream:
        stream.write(bytes([first[0] ^ 1]))
    try:
        previous = output / 'linux-preserved-program'
        rejected = guest_compile(preparation, installed['linux'], core, 'linux/arm64', previous,
                                 output / 'linux-rejection', initial_output=b'previous artifact')
        if rejected['status'] != 2 or rejected['stdout'] or 'SHA-256 mismatch' not in rejected['stderr']:
            raise ValueError(f'Linux host library corruption was not rejected: {rejected}')
        check_guest_cleanup(rejected)
        if previous.read_bytes() != b'previous artifact':
            raise ValueError('Linux admission failure replaced the existing output')
        results['rejections'].append(dict(rejected, case='linux-host-library', prior_output_preserved=True, staging_empty=True))
    finally:
        with library.open('r+b') as stream:
            stream.write(first)
    # Repeat after a second move to prove resolution does not retain an install path.
    relocated = installed['darwin'].with_name('second relocated installation')
    installed['darwin'].rename(relocated)
    installed['darwin'] = relocated
    executable, compiled = darwin_compile(relocated, core, 'darwin/arm64', output / 'second-relocation-build')
    result = darwin_run(executable, output / 'second-relocation-run')
    check_core(result)
    results['relocation'] = {'compile': compiled, 'execute': result}
    xor = sources / 'xor.lo'
    xor.write_text((ROOT / 'examples/xor_cpu.lo').read_text())
    results['training'] = []
    for host in ('darwin', 'linux'):
        if host == 'darwin':
            executable, compiled = darwin_compile(installed[host], xor, 'darwin/arm64', output / 'darwin-xor-build')
            result = darwin_run(executable, output / 'darwin-xor-run')
        else:
            executable = output / 'linux-xor-program'
            transport(template, executable)
            compiled = guest_compile(preparation, installed[host], xor, 'linux/arm64', executable, output / 'linux-xor-build')
            check_guest_compile(compiled)
            result = linux_run(preparation, executable, output / 'linux-xor-run')
        check_training(result)
        results['training'].append({'host': host + '/arm64', 'compile': compiled, 'execute': result})
    results['status'] = 'passed'
    (output / 'validation.json').write_text(json.dumps(results, indent=2) + '\n')
    return results


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('darwin', 'linux', 'metal', 'preparation', 'output'):
        parser.add_argument('--' + name, required=True, type=Path)
    args = parser.parse_args()
    try:
        report = qualify(*(local(getattr(args, name)) for name in ('darwin', 'linux', 'metal', 'preparation', 'output')))
        print(json.dumps({'status': report['status'], 'receipt': str(args.output / 'validation.json')}))
        return 0
    except (ValueError, OSError, KeyError, subprocess.SubprocessError, tarfile.TarError) as error:
        print(f'distribution acceptance: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
