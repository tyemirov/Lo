"""Compile public LLVM output and execute independent bounded ABI fixtures."""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import time
from typing import Any
from prepare import ROOT, TOOL_SHA256, digest, local
from oracle import FIXTURES, expected


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--lo', required=True)
    parser.add_argument('--tools', required=True)
    parser.add_argument('--state', required=True)
    args = parser.parse_args()
    state, lo, manifest_path = local(args.state), local(args.lo), local(args.tools)
    if not state.is_relative_to(ROOT / '_scratch'):
        raise ValueError('execution state must be under Lo/_scratch')
    manifest = json.loads(manifest_path.read_text())
    pin = json.loads((ROOT / 'experiments/native-backends/preparation.lock.json').read_text())['third_party_artifacts'][0]
    if manifest['version'] != '21.1.8' or manifest['archive'] != pin:
        raise ValueError('tools manifest does not match the repository LLVM pin')
    tools: dict[str, str] = {}
    for name in ('clang', 'opt', 'llc', 'ld64.lld', 'llvm-dwarfdump'):
        item = manifest['tools'][name]
        path = local(item['path'])
        if item['sha256'] != TOOL_SHA256[name] or digest(path) != item['sha256']:
            raise ValueError(f'tool digest mismatch: {name}')
        tools[name] = str(path)
    protected = [lo, manifest_path, *(Path(p) for p in tools.values()), ROOT / 'testdata/native-lowering', ROOT / 'tools/nativelowering']
    if any(path == state or path.is_relative_to(state) or state.is_relative_to(path) for path in protected):
        raise ValueError('execution state overlaps an input')
    if state.exists() and any(state.iterdir()):
        raise ValueError('execution state must be absent or empty')
    state.mkdir(parents=True, exist_ok=True)
    temporary = state / 'tmp'; temporary.mkdir()
    env = {**os.environ, 'CI': '1', 'TMPDIR': str(temporary), 'HOME': str(state), 'XDG_CACHE_HOME': str(state / 'cache')}
    commands: list[dict[str, Any]] = []
    def run(command: list[str]) -> str:
        start = time.monotonic()
        result = subprocess.run(command, cwd=ROOT, env=env, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=120)
        commands.append({'argv': command, 'seconds': time.monotonic() - start, 'returncode': result.returncode, 'stderr': result.stderr})
        if result.returncode:
            raise ValueError(f'command failed ({result.returncode}): {command}\n{result.stderr}')
        return result.stdout
    report: dict[str, Any] = {'schema': 'lo-native-lowering-execution-1', 'scope': 'bounded C test runtime; Darwin execution; Linux object generation only', 'tools_manifest_sha256': digest(manifest_path), 'compiler_sha256': digest(lo), 'sources': {}, 'cells': [], 'commands': commands}
    source_paths = list((ROOT / 'tools/nativelowering').glob('*.py')) + [ROOT / 'tools/nativelowering/runtime.c', ROOT / 'tools/nativelowering/libSystem.tbd']
    for directory in ('internal/nativeplan', 'internal/llvmemit', 'internal/nativebuild', 'internal/runner', 'internal/check', 'internal/syntax', 'internal/frontend', 'internal/types', 'internal/catalog', 'internal/ir', 'cmd/lo'):
        source_paths.extend(path for path in (ROOT / directory).rglob('*.go') if not path.name.endswith('_test.go'))
    for path in source_paths:
        report['sources'][str(path.relative_to(ROOT))] = digest(path)
    report['tool_versions'] = {name: run([path, '--version']).splitlines()[0] for name, path in tools.items()}
    invalid_cases: list[list[str]] = []
    try:
        for target in ('darwin/arm64', 'linux/arm64'):
            for fixture in FIXTURES:
                source = ROOT / 'testdata/native-lowering' / (fixture.name + '.lo')
                report['sources'][str(source.relative_to(ROOT))] = digest(source)
                cell = state / target.replace('/', '-') / fixture.name
                cell.mkdir(parents=True)
                module = cell / 'module.ll'
                receipt = json.loads(run([str(lo), 'build', str(source), '--emit', 'llvm', '--target', target, '--output', str(module), '--format', 'json']))
                if any(receipt.get(key) != value for key, value in {'command': 'build', 'status': 'ok', 'target': target, 'emit': 'llvm', 'abi': 'lo-native-3'}.items()) or local(receipt.get('artifact', '')) != module:
                    raise ValueError(f'emission failed: {receipt}')
                run([tools['opt'], '-passes=verify', '-disable-output', str(module)])
                for optimization in (0, 2):
                    optimized, obj = cell / f'O{optimization}.bc', cell / f'O{optimization}.o'
                    run([tools['opt'], f'-passes=default<O{optimization}>', str(module), '-o', str(optimized)])
                    run([tools['llc'], f'-O{optimization}', '-relocation-model=pic', '-filetype=obj', str(optimized), '-o', str(obj)])
                    debug_dump = run([tools['llvm-dwarfdump'], '--debug-line', str(obj)])
                    if source.name not in debug_dump:
                        raise ValueError(f'missing object source map for {source.name}: {debug_dump}')
                    (cell / f'O{optimization}.debug-line.txt').write_text(debug_dump)
                    entry: dict[str, Any] = {'fixture': fixture.name, 'target': target, 'optimization': optimization, 'module_sha256': digest(module), 'object_sha256': digest(obj), 'cases': []}
                    report['cells'].append(entry)
                    if target != 'darwin/arm64':
                        continue
                    runtime, executable = cell / f'runtime-O{optimization}.o', cell / f'program-O{optimization}'
                    run([tools['clang'], '-target', 'arm64-apple-macos27.0', f'-O{optimization}', '-nostdinc', '-ffreestanding', '-fno-builtin', '-Wall', '-Wextra', '-Werror', '-Wno-unused-parameter', '-c', str(ROOT / 'tools/nativelowering/runtime.c'), '-o', str(runtime)])
                    run([tools['ld64.lld'], '-arch', 'arm64', '-platform_version', 'macos', '27.0', '27.0', '-syslibroot', str(state), str(runtime), str(obj), str(ROOT / 'tools/nativelowering/libSystem.tbd'), '-o', str(executable)])
                    minimum = fixture.events.count('step')
                    cases = [(minimum - 1, -1, 67108864), (minimum, -1, 67108864), (minimum + 1, -1, 67108864), (0, 0, 67108864), (1, 1, 67108864), (minimum, -1, 16)]
                    for budget, cancel, weight in cases:
                        actual = json.loads(run([str(executable), str(budget), str(cancel), str(weight)]))
                        wanted = expected(fixture, budget, cancel, weight)
                        if actual != wanted:
                            raise ValueError(f'{fixture.name} O{optimization} budget={budget} cancel={cancel} weight={weight}\nexpected {json.dumps(wanted)}\nactual   {json.dumps(actual)}')
                        entry['cases'].append({'budget': budget, 'cancel_after_steps': cancel, 'weight_limit': weight, 'result': actual})
                    if fixture.name == 'literal':
                        invalid_cases.append([str(executable), '2', '-1', '67108864', 'invalid-result'])
                    entry['executable_sha256'] = digest(executable)
        for command in invalid_cases:
            actual = json.loads(run(command))
            wanted = dict(events=['step', 'step', 'invalid-result', 'invalid-status'], status=2, code=5, value=None, steps=2, weight=0, live=0, scopes=0, depth=0)
            if actual != wanted:
                raise ValueError(f'unknown status payload was not rejected: {actual}')
        if digest(lo) != report['compiler_sha256'] or any(digest(ROOT / path) != value for path, value in report['sources'].items()):
            raise ValueError('compiler or source inputs changed during execution')
        report['invalid_status_payload_cases'] = len(invalid_cases)
        report['status'] = 'passed' 
    except (ValueError, OSError, subprocess.TimeoutExpired) as error:
        report['status'], report['error'] = 'failed', str(error)
        raise
    finally:
        (state / 'results.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps({'status': 'passed', 'cells': len(report['cells']), 'result': str((state / 'results.json').relative_to(ROOT))}))
    return 0

if __name__ == '__main__':
    try:
        sys.exit(main())
    except (ValueError, OSError, KeyError, json.JSONDecodeError) as error:
        print(str(error), file=sys.stderr)
        sys.exit(1)
