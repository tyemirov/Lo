"""Build test fixtures on the host and run the Linux semantic corpus offline."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

from batch import ROOT, local, run, verified

sys.path.insert(0, str(ROOT / 'tools/semanticcoverage'))
from run import select_tests as test_pattern  # noqa: E402
from report import source_identity  # noqa: E402

PACKAGE = 'github.com/MarcoPoloResearchLab/Lo/tests/integration'


def environment() -> dict[str, str]:
    result = dict(os.environ, CI='1', CGO_ENABLED='0', GOOS='linux', GOARCH='arm64',
                  GOTOOLCHAIN='local', GOENV='off', GOWORK='off', GOTELEMETRY='off',
                  GOPROXY='off', GOSUMDB='off', GOFLAGS='', GOEXPERIMENT='')
    result.pop('GOCACHEPROG', None)
    for name in ('HOME', 'TMPDIR', 'GOCACHE', 'GOMODCACHE', 'GOPATH'):
        value = result.get(name)
        if not value or not Path(value).is_absolute():
            raise ValueError(f'{name} must declare a repository-local directory')
        local(Path(value))
    return result


def execute(command: list[str], env: dict[str, str], output: Path) -> None:
    result = subprocess.run(command, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                            capture_output=True, check=False)
    output.write_bytes(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(f'host fixture command failed: {command}; inspect {output}')


def control_fixture(preparation: Path, package: Path, output: Path, env: dict[str, str], log: Path) -> None:
    tools = json.loads(local(preparation / 'tools.json').read_text())['tools']
    compiler, linker = (verified(tools[name]) for name in ('clang', 'ld.lld'))
    manifest = json.loads((package / 'toolchain.json').read_text())
    selected = manifest['targets']['linux/arm64']
    def resolve(identity: dict[str, str]) -> Path:
        path = local(package / identity['path'])
        if not path.is_relative_to(package) or hashlib.sha256(path.read_bytes()).hexdigest() != identity['sha256']:
            raise ValueError(f'controlled fixture input identity differs: {path}')
        return path
    obj = output.with_suffix('.o')
    execute([str(compiler), '--target=' + selected['triple'], '-O0', '-nostdinc', '-fno-stack-protector',
             '-c', str(ROOT / 'testdata/native_toolchain/control.c'), '-o', str(obj)], env, log)
    arguments = []
    for item in selected['link_args']:
        if item == '{object}':
            arguments.append(str(obj))
        elif item == '{output}':
            arguments.append(str(output))
        elif item == '{runtime}':
            continue
        elif item.startswith('{input:') and item.endswith('}'):
            arguments.append(str(resolve(selected['inputs'][item[7:-1]])))
        else:
            arguments.append(item)
    execute([str(linker), *arguments], env, log.with_name('control-link.log'))


def qualify(preparation: Path, package: Path, matrix: Path, work: Path, go: str,
            pattern: str | None = None, timeout: int = 7200, accelerator: str = 'tcg') -> dict:
    preparation, package, matrix, work = map(local, (preparation, package, matrix, work))
    if work.exists():
        raise ValueError(f'qualification suite work must be fresh: {work}')
    env = environment()
    work.mkdir(parents=True)
    inputs = work / 'inputs'
    inputs.mkdir()
    test_binary = inputs / 'integration.test'
    execute([go, 'test', '-c', '-o', str(test_binary), './tests/integration'], env, work / 'test-build.log')
    probe = inputs / 'fileprobe'
    execute([go, 'build', '-o', str(probe), './testdata/fileprobe'], env, work / 'probe-build.log')
    development = inputs / 'lo-development'
    execute([go, 'build', '-o', str(development), './cmd/lo'], env, work / 'development-build.log')
    controlled = inputs / 'native-toolchain'
    control_fixture(preparation, package, controlled, env, work / 'control-build.log')
    identities = {name: {'path': path.name, 'sha256': hashlib.sha256(path.read_bytes()).hexdigest()}
                  for name, path in (('fileprobe', probe), ('development-compiler', development),
                                     ('native-toolchain', controlled))}
    (inputs / 'fixtures.json').write_text(json.dumps(identities, indent=2) + '\n')
    listing = subprocess.run(['git', 'ls-files', '--cached', '--others', '--exclude-standard', '-z'],
                             cwd=ROOT, env=env, stdin=subprocess.DEVNULL, capture_output=True, check=True)
    source_names = set(source_identity()[0])
    source_names.update(os.fsdecode(raw) for raw in listing.stdout.split(b'\0') if raw)
    payloads = []
    for name in sorted(source_names):
        path = local(ROOT / name)
        if not path.is_file():
            continue
        payloads.append({'source': str(path), 'path': '/workspace/' + path.relative_to(ROOT).as_posix()})
    guest_inputs = '/workspace/' + inputs.relative_to(ROOT).as_posix()
    payloads.append({'source': str(inputs), 'path': guest_inputs})
    guest_package = '/workspace/' + package.relative_to(ROOT).as_posix()
    guest_manifest = guest_package + '/toolchain.json'
    case = {'id': 'semantic-linux-arm64', 'argv': [guest_inputs + '/integration.test', '-test.v=test2json',
            '-test.count=1', '-test.timeout=' + str(timeout) + 's', '-test.run=' + (pattern or test_pattern(matrix))],
            'cwd': '/workspace/tests/integration', 'environment': {'LO_TEST_BACKEND': 'llvm',
            'LO_TEST_COMPILER': guest_package + '/bin/lo', 'LO_LLVM_TOOLCHAIN': guest_manifest,
            'LO_SEMANTIC_MATRIX': '/workspace/' + matrix.relative_to(ROOT).as_posix(),
            'LO_TEST_PREBUILT_FIXTURES': guest_inputs}, 'stdin': '', 'timeout_seconds': timeout, 'outputs': []}
    manifest = work / 'batch.json'
    manifest.write_text(json.dumps({'files': payloads, 'cases': [case]}, indent=2) + '\n')
    report = run(preparation, package, manifest, work / 'guest', guest_package, accelerator)
    stdout = work / 'guest/0.stdout'
    events = work / 'semantic-linux-arm64.jsonl'
    with stdout.open('rb') as source, events.open('wb') as output:
        result = subprocess.run([go, 'tool', 'test2json', '-p', PACKAGE, '-t'], cwd=ROOT, env=env,
                                stdin=source, stdout=output, stderr=subprocess.PIPE, check=False)
    if result.returncode:
        raise ValueError(f'Linux semantic event conversion failed: {result.stderr.decode(errors="replace")}')
    report['events'] = str(events.relative_to(ROOT))
    report['test_binary_sha256'] = hashlib.sha256(test_binary.read_bytes()).hexdigest()
    report['fixture_identities'] = identities
    report['pattern'] = pattern or test_pattern(matrix)
    (work / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('preparation', 'package', 'matrix', 'work'):
        parser.add_argument('--' + name, required=True, type=Path)
    parser.add_argument('--go', default='go')
    parser.add_argument('--pattern')
    parser.add_argument('--timeout', type=int, default=7200)
    parser.add_argument('--accelerator', choices=('tcg', 'hvf'), default='tcg')
    args = parser.parse_args()
    try:
        if not 1 <= args.timeout <= 7200:
            raise ValueError('suite timeout must be 1..7200 seconds')
        report = qualify(args.preparation, args.package, args.matrix, args.work, args.go, args.pattern, args.timeout, args.accelerator)
        print(json.dumps(report))
        return 0 if report['cases'][0]['status'] == 0 else 1
    except (ValueError, OSError, KeyError, subprocess.SubprocessError) as error:
        print(f'Linux qualification suite: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
