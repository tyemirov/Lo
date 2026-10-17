"""Run the fixed native backend experiment through its public CLI."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
SOURCE = ROOT / 'experiments/native-backends'
SCRATCH = ROOT / '_scratch/native-backends-20261016'
EVIDENCE = ROOT / 'docs/evidence/native-backends'
IMAGE = 'lo-native-probe:20261016'
BASE = 'rust@sha256:e51d0265072d2d9d5d320f6a44dde6b9ef13653b035098febd68cce8fa7c0bc4'
EXPECTED = '23 0\n0 7\n'
TARGETS = ['aarch64-apple-darwin', 'aarch64-unknown-linux-gnu']
BACKENDS = ['llvm', 'cranelift']
SANDBOX = ['/usr/bin/sandbox-exec', '-p', '(version 1)(allow default)(deny network*)']


def command(args: list[str]) -> dict[str, object]:
    start = time.monotonic()
    result = subprocess.run(args, stdin=subprocess.DEVNULL, capture_output=True, text=True,
                            env={**os.environ, 'CI': '1'})
    return {'command': args, 'exit_code': result.returncode, 'seconds': round(time.monotonic() - start, 6),
            'stdout': result.stdout, 'stderr': result.stderr}


def require(args: list[str]) -> str:
    result = command(args)
    if result['exit_code'] != 0:
        raise RuntimeError(f"command failed: {args!r}: {result['stderr']}")
    return str(result['stdout']).strip()


def digest(path: Path) -> str:
    return hashlib.sha256(path.read_bytes()).hexdigest()


def container(image: str, script: str) -> list[str]:
    return ['docker', 'run', '--rm', '--network=none', '--read-only', '--tmpfs', '/tmp',
            '-e', 'CI=1', '-v', f'{SOURCE}:/input:ro', '-v', f'{SCRATCH}:/output',
            image, 'sh', '-eu', '-c', script]


def prepared_sources() -> dict[str, str]:
    paths = [SOURCE / 'Dockerfile', *sorted((SOURCE / 'cranelift').rglob('*'))]
    return {str(path.relative_to(ROOT)): digest(path) for path in paths if path.is_file()}


def bootstrap() -> None:
    SCRATCH.mkdir(parents=True, exist_ok=True)
    require(['docker', 'pull', BASE])
    require(['docker', 'build', '--build-arg', f'RUST_IMAGE={BASE}', '-t', IMAGE, str(SOURCE)])
    image = require(['docker', 'image', 'inspect', '--format', '{{.Id}}', IMAGE])
    lock = require(['docker', 'run', '--rm', '--network=none', image, 'cat', '/probe/src/Cargo.lock'])
    if lock + '\n' != (SOURCE / 'cranelift/Cargo.lock').read_text():
        raise RuntimeError('bootstrap image Cargo.lock differs from declared source')
    receipt = {'image': image, 'base_image': BASE, 'source_sha256': prepared_sources(), 'network': 'enabled during bootstrap only',
               'reproduction': 'reuse image digest; apt transitive package versions recorded, not separately pinned',
               'packages': require(['docker', 'run', '--rm', '--network=none', image, 'cat', '/probe/packages.txt']),
               'rustc': require(['docker', 'run', '--rm', '--network=none', image, 'rustc', '--version'])}
    (SCRATCH / 'bootstrap.json').write_text(json.dumps(receipt, indent=2) + '\n')


def run() -> int:
    receipt_path = SCRATCH / 'bootstrap.json'
    if not receipt_path.is_file():
        raise RuntimeError(f'missing bootstrap input: {receipt_path}; run bootstrap explicitly')
    receipt = json.loads(receipt_path.read_text())
    if receipt.get('source_sha256') != prepared_sources():
        raise RuntimeError('stale bootstrap input: source hashes differ; prepare explicitly')
    image = receipt['image']
    require(['docker', 'image', 'inspect', image])
    sdk_result = command(['/usr/bin/xcrun', '--sdk', 'macosx', '--show-sdk-path'])
    sdk = str(sdk_result['stdout']).strip()
    report: dict[str, object] = {
        'contract': {'expected_stdout': EXPECTED, 'inputs': [7, -1], 'arithmetic': '3*x+2',
                     'negative_input': {'value': 0, 'status': 7}, 'success_status': 0},
        'scope': 'fixed-width scalar backend probe; not Lo lowering or compiler acceptance',
        'assignment': {'requested': 'mpr-worker gpt-6.1-sol/medium', 'agent': '/root/backend_experiment',
                       'actual': 'host role assignment; runtime telemetry unavailable'},
        'external_inputs': {'bootstrap': receipt, 'apple_sdk': sdk_result, 'clean_sdk_free': False,
                            'llvm_linux': command(container(image, 'llc-14 --version')),
                            'lld': command(container(image, 'command -v ld.lld-14')),
                            'tool_sha256': command(container(image, 'sha256sum /usr/bin/llc-14 /usr/bin/clang-14 /probe/src/target/release/lo-native-probe')),
                            'apple_clang_sha256': digest(Path('/usr/bin/clang')),
                            'apple_clang': require(['/usr/bin/clang', '--version']),
                            'apple_sdk_version': command(['/usr/bin/xcrun', '--sdk', 'macosx', '--show-sdk-version']),
                            'darwin_dependencies': ['Apple SDK headers', 'Apple linker', 'libSystem', 'printf'],
                            'linux_dependencies': ['Debian glibc headers', 'GNU linker', 'glibc', 'printf'],
                            'production_version_recommendation': False},
        'source_sha256': {str(p.relative_to(ROOT)): digest(p) for p in sorted(SOURCE.rglob('*'))
                          if p.is_file() and '__pycache__' not in str(p) and '.pytest_cache' not in str(p)}, 'cells': []}
    cells = report['cells']
    for target in TARGETS:
        for backend in BACKENDS:
            stem = f'{backend}-{target}'
            obj = SCRATCH / f'{stem}.o'
            exe = SCRATCH / stem
            apple = target == TARGETS[0]
            cell: dict[str, object] = {'backend': backend, 'target': target,
                'offline_isolation': {
                    'object': 'docker --network=none' if backend == 'cranelift' or not apple else 'sandbox-exec deny network*',
                    'link': 'sandbox-exec deny network*' if apple else 'docker --network=none',
                    'run': 'sandbox-exec deny network*' if apple else 'docker --network=none'},
                'go': 'not invoked; host absence unproved' if apple else 'not invoked; container absence checked', 'object': None, 'link': None, 'run': None}
            if backend == 'cranelift':
                emit = container(image, f'/probe/src/target/release/lo-native-probe {target} /output/{stem}.o')
            elif apple:
                emit = SANDBOX + ['/usr/bin/clang', '-target', target, '-c', str(SOURCE / 'probe.ll'), '-o', str(obj)]
            else:
                emit = container(image, f'llc-14 -opaque-pointers -mtriple={target} -filetype=obj /input/probe.ll -o /output/{stem}.o')
            if apple and sdk_result['exit_code'] != 0:
                cell['object'] = {'exit_code': 1, 'stderr': 'missing Apple SDK input', 'input': sdk_result}
                cell['passed'] = False
                cells.append(cell)
                continue
            cell['object'] = command(emit)
            if cell['object']['exit_code'] == 0:
                cell['object_sha256'] = digest(obj)
                if apple:
                    link = SANDBOX + ['/usr/bin/clang', '-isysroot', sdk, str(SOURCE / 'runtime.c'), str(obj), '-o', str(exe)]
                else:
                    link = container(image, f'clang-14 /input/runtime.c /output/{stem}.o -o /output/{stem}')
                cell['link'] = command(link)
                if cell['link']['exit_code'] == 0:
                    cell['executable_sha256'] = digest(exe)
                    cell['run'] = command(SANDBOX + [str(exe)] if apple else container(image, f'/output/{stem}'))
            cell['passed'] = (cell['run'] is not None and cell['run']['exit_code'] == 0
                              and cell['run']['stdout'] == EXPECTED)
            cells.append(cell)
    report['isolation_checks'] = {
        'linux': command(container(image, 'test ! -e /usr/local/go; ! command -v go; cat /proc/net/route')),
        'darwin_network_denial': command(SANDBOX + [sys.executable, '-c', 'import socket; socket.socket().connect(("1.1.1.1",80))'])}
    isolation = report['isolation_checks']
    report['isolation_passed'] = (isolation['linux']['exit_code'] == 0
                                  and isolation['darwin_network_denial']['exit_code'] != 0
                                  and 'Operation not permitted' in isolation['darwin_network_denial']['stderr'])
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    (EVIDENCE / 'experiment.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'passed': sum(bool(c['passed']) for c in cells), 'total': len(cells)}))
    return 0 if all(c['passed'] for c in cells) and report['isolation_passed'] else 1


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument('action', choices=['bootstrap', 'run', 'check'])
    action = parser.parse_args().action
    if action == 'check':
        for name in ['probe.ll', 'runtime.c', 'Dockerfile', 'cranelift/Cargo.toml', 'cranelift/Cargo.lock', 'cranelift/src/main.rs']:
            if not (SOURCE / name).is_file():
                raise RuntimeError(f'missing source input: {name}')
        if (EVIDENCE / 'experiment.json').exists():
            report = json.loads((EVIDENCE / 'experiment.json').read_text())
            if {(c['backend'], c['target']) for c in report['cells']} != {(b, t) for b in BACKENDS for t in TARGETS}:
                raise RuntimeError('experiment report does not contain the four required cells')
            for cell in report['cells']:
                observed = cell['run']
                if cell['passed'] and (observed is None or observed['exit_code'] != 0 or observed['stdout'] != EXPECTED):
                    raise RuntimeError('experiment report declares success with an incorrect result')
            hashes = report.get('source_sha256')
            if not isinstance(hashes, dict):
                raise RuntimeError('experiment report has no source hashes')
            for name, recorded in hashes.items():
                source = ROOT / name
                if not source.is_file() or digest(source) != recorded:
                    raise RuntimeError(f'experiment source changed: {name}; run experiment explicitly')
        print(json.dumps({'expected_stdout': EXPECTED, 'targets': TARGETS, 'backends': BACKENDS}))
        return 0
    if action == 'bootstrap':
        bootstrap()
        return 0
    return run()


if __name__ == '__main__':
    try:
        sys.exit(main())
    except (RuntimeError, OSError, ValueError) as error:
        sys.stderr.write(f'native backend probe: {error}\n')
        sys.exit(1)
