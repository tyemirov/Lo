"""Prepare and measure paired Linux benchmarks in the offline software guest."""
from __future__ import annotations

import argparse
from dataclasses import dataclass
import hashlib
import json
import os
import platform
from pathlib import Path
import shutil
import subprocess
import sys
from typing import Any

from batch import ROOT, local, run as run_batch, verified
from suite import environment

sys.path.insert(0, str(ROOT / 'tools/nativebench'))
import qualification
from qualification import SAMPLE_PLAN, validate_sample_plan, LANES, WARMUP, completion, expected_inventory  # noqa: E402
from identity import CACHE, capture_provenance, merge_identity, verify_provenance  # noqa: E402

GUEST_INPUT = '/input/performance'
GUEST_WORK = '/work/performance'
GUEST_REPORT = '/result/performance.json'
PRODUCER_SOURCES = ('tools/loqualification/performance.py',
                    'tools/loqualification/performance-runner/main.go',
                    'tools/loqualification/batch.py', 'tools/loqualification/batch_init.c',
                    'tools/loqualification/suite.py', 'tools/lodistribution/prepare.py',
                    'tools/nativecorpus/main.go')


@dataclass(frozen=True)
class Inputs:
    preparation: Path
    package: Path
    frontend: Path
    work: Path
    go: str
    lanes: tuple[str, ...]
    cases: tuple[str, ...]
    accelerator: str = 'tcg'


def digest(path: Path) -> str:
    return hashlib.sha256(local(path).read_bytes()).hexdigest()


def execute(argv: list[str], env: dict[str, str], log: Path) -> bytes:
    result = subprocess.run(argv, cwd=ROOT, env=env, stdin=subprocess.DEVNULL,
                            capture_output=True, check=False)
    log.write_bytes(result.stdout + result.stderr)
    if result.returncode:
        raise ValueError(f'performance preparation failed: {argv}; inspect {log}')
    return result.stdout


def resolve(package: Path, identity: dict[str, str]) -> Path:
    path = local(package / identity['path'])
    if not path.is_relative_to(package) or digest(path) != identity['sha256']:
        raise ValueError(f'performance input identity differs: {path}')
    return path


def native_fixture(inputs: Inputs, path: Path, env: dict[str, str], manifest: dict) -> tuple[Path, Path]:
    tools = json.loads(local(inputs.preparation / 'tools.json').read_text())['tools']
    compiler, linker = (verified(tools[name]) for name in ('clang', 'ld.lld'))
    selected = manifest['targets']['linux/arm64']
    obj, module = path / 'program.o', path / 'program.ll'
    execute([str(inputs.frontend), 'build', str(path / 'source.lo'), '--emit', 'llvm',
             '--target', 'linux/arm64', '--output', str(module)], env, path / 'emit.log')
    flags = ['--target=' + selected['triple'], '-O2', '-g', '-fPIC', '-ffp-contract=off', '-nostdinc']
    execute([str(compiler), *flags, '-c', str(module), '-o', str(obj)], env, path / 'object.log')
    def link(source: Path, output: Path) -> Path:
        harness = output.with_suffix('.o')
        execute([str(compiler), *flags, '-c', str(source), '-o', str(harness)], env,
                output.with_suffix('.compile.log'))
        args: list[str] = []
        for argument in selected['link_args']:
            if argument == '{object}':
                args.extend([str(obj), str(harness)])
            elif argument == '{runtime}':
                args.append(str(resolve(inputs.package, selected['runtime'])))
            elif argument == '{output}':
                args.append(str(output))
            elif argument.startswith('{input:') and argument.endswith('}'):
                args.append(str(resolve(inputs.package, selected['inputs'][argument[7:-1]])))
            else:
                args.append(argument)
        execute([str(linker), *args], env, output.with_suffix('.link.log'))
        return output
    benchmark = link(path / 'benchmark.c', path / 'native-benchmark')
    allocation_source = path / 'allocation.c'
    if not allocation_source.is_file():
        raise ValueError(f'missing generated allocation driver: {allocation_source}')
    allocation = link(allocation_source, path / 'native-allocation')
    return benchmark, allocation


def prepare(inputs: Inputs) -> tuple[Path, dict[str, Any]]:
    inventory = expected_inventory(inputs.lanes, inputs.cases)
    if not inputs.frontend.is_file():
        raise ValueError(f'missing host frontend: {inputs.frontend}')
    if inputs.work.exists():
        raise ValueError(f'performance work must be fresh: {inputs.work}')
    tools = json.loads(local(inputs.preparation / 'tools.json').read_text())['tools']
    go_path = shutil.which(inputs.go)
    if go_path is None:
        raise ValueError('performance preparation requires the declared Go producer')
    tool_paths = {'python': Path(sys.executable), 'go': Path(go_path), 'frontend_emitter': inputs.frontend,
                  'host_tools': inputs.preparation / 'tools.json',
                  'host_clang': verified(tools['clang']), 'host_linker': verified(tools['ld.lld']),
                  'qemu': inputs.preparation / 'emulation/qemu-system-aarch64',
                  'guest_kernel': inputs.preparation / 'vmlinuz-virt',
                  'qemu_libraries': inputs.preparation / 'emulation/lib',
                  'guest_libraries': inputs.preparation / 'linux-sysroot/lib'}
    for name in ('crt1.o', 'crti.o', 'crtn.o', 'libc_nonshared.a'):
        tool_paths['guest_startup_' + name.replace('.', '_')] = inputs.preparation / 'linux-sysroot/usr/lib/aarch64-linux-gnu' / name
    frozen = capture_provenance(ROOT, inputs.package / 'bin/lo', inputs.package / 'toolchain.json',
                                'linux/arm64', PRODUCER_SOURCES, tool_paths)
    manifest = json.loads(local(inputs.package / 'toolchain.json').read_text())
    if manifest['host'] != 'linux/arm64' or manifest['llvm'] != '21.1.8' or manifest['abi'] != 'lo-native-2':
        raise ValueError('performance requires the installed Linux LLVM distribution')
    resolve(inputs.package, manifest['targets']['linux/arm64']['runtime'])
    if not (inputs.package / 'bin/lo').is_file():
        raise ValueError('performance requires the installed Linux frontend')
    env = environment()
    env['GOMAXPROCS'] = '1'
    env['GOARM64'] = 'v8.0'
    host_env = {name: value for name, value in env.items() if name not in ('GOOS', 'GOARCH')}
    inputs.work.mkdir(parents=True)
    generated = inputs.work / 'generated'
    execute([inputs.go, 'run', './tools/nativebench', '--output', str(generated)], host_env,
            inputs.work / 'generator.log')
    corpus = json.loads(execute([inputs.go, 'run', './tools/nativecorpus'], host_env,
                               inputs.work / 'corpus.log'))
    process_cases = [*corpus, {'name': 'file', 'source': 'flow F()->Int!{text=UTF8.String.new(File.read(File.Path.new("input.txt")));effect=File.Write.new(File.Path.new("output.txt"),Bytes.new(text));commit effect;return String.length(text)};return F()', 'expected': '30000'}]
    build_cases = [*corpus, *({'name': name, 'source': (ROOT / source).read_text(), 'expected': ''}
                             for name, source in (('training_mvp', 'examples/training_cpu.lo'),
                                                  ('training_dense', 'tests/benchmark/testdata/training_dense.lo')))]
    http_cases = [{'name': 'echo', 'source': (ROOT / 'examples/http_echo.lo').read_text(), 'expected': 'fixed varied payload 123'},
                  {'name': 'computed', 'source': 'flow H(r:HTTP.Request)->HTTP.Response{n=String.length(r.path);return HTTP.Response.Text.new(200,Int.to_string(n))};flow E(e:Error)->HTTP.Response{return HTTP.Response.Text.new(400,e.kind)};server=HTTP.Server.new(0,H,E);commit server', 'expected': '10'}]
    available = {case['name'] for case in [*build_cases, *process_cases, *http_cases]}
    if set(inputs.cases) - available:
        raise ValueError(f'unknown performance cases: {sorted(set(inputs.cases) - available)}')
    runner = inputs.work / 'performance-runner'
    execute([inputs.go, 'build', '-o', str(runner), str(Path(__file__).with_name('performance-runner'))], env,
            inputs.work / 'runner-build.log')
    payload = inputs.work / 'payload'
    payload.mkdir()
    config: dict[str, Any] = {'sample_plan': dict(SAMPLE_PLAN), 'warmup': WARMUP, 'compiler': '/compiler/bin/lo',
                              'output': GUEST_REPORT, 'execution': [], 'process': [], 'http': [], 'build': []}
    identities: dict[str, Any] = {}
    def selected(case: dict[str, Any]) -> bool:
        return not inputs.cases or case['name'] in inputs.cases
    def fixture(case: dict[str, Any], lane: str) -> dict[str, Any]:
        name, source = case['name'], case['source']
        directory = payload / lane / name
        directory.mkdir(parents=True)
        (directory / 'source.lo').write_text(source)
        guest_input = f'{GUEST_INPUT}/{lane}/{name}'
        guest_work = f'{GUEST_WORK}/{lane}/{name}'
        # A payload directory creates each declared working directory before launch.
        (payload / 'working' / lane / name).mkdir(parents=True)
        return {'name': name, 'source': guest_input + '/source.lo', 'expected': case['expected'],
                'source_sha256': hashlib.sha256(source.encode()).hexdigest(), 'reference': '', 'native': '',
                'allocation': '', 'artifact': guest_work + '/program', 'work': guest_work}
    if 'execution' in inputs.lanes:
        for case in filter(selected, build_cases):
            path = generated / case['name']
            reference = path / 'reference.test'
            execute([inputs.go, 'test', '-c', '-o', str(reference), str(path / 'program_test.go')], env,
                    path / 'reference-build.log')
            native, allocation = native_fixture(inputs, path, host_env, manifest)
            record = fixture(case, 'execution')
            directory = payload / 'execution' / case['name']
            for name, source in [('reference.test', reference), ('native-benchmark', native),
                                 ('native-allocation', allocation)]:
                shutil.copy2(source, directory / name)
            base = f'{GUEST_INPUT}/execution/{case["name"]}/'
            record['reference'], record['native'] = base + 'reference.test', base + 'native-benchmark'
            record['allocation'] = base + 'native-allocation'
            config['execution'].append(record)
    for lane, cases in [('process', process_cases), ('http', http_cases), ('build', build_cases)]:
        if lane in inputs.lanes:
            config[lane] = [fixture(case, lane) for case in cases if selected(case)]
    if not any(config[lane] for lane in inputs.lanes):
        raise ValueError('selected cases do not belong to the requested performance lanes')
    if {lane: [case['name'] for case in config[lane]] for lane in inputs.lanes if config[lane]} != inventory:
        raise ValueError('generated benchmark cases differ from the fixed qualification inventory')
    configuration = payload / 'config.json'
    configuration.write_text(json.dumps(config, indent=2) + '\n')
    for path in payload.rglob('*'):
        if path.is_file():
            identities[path.relative_to(payload).as_posix()] = {'bytes': path.stat().st_size, 'sha256': digest(path)}
    batch = inputs.work / 'batch.json'
    batch.write_text(json.dumps({'files': [{'source': str(payload), 'path': GUEST_INPUT},
                                         {'source': str(payload / 'working'), 'path': GUEST_WORK},
                                         {'source': str(runner), 'path': '/input/performance-runner'}],
                                'cases': [{'id': 'linux-performance',
                                           'argv': ['/input/performance-runner', '--config', GUEST_INPUT + '/config.json'],
                                           'cwd': '/work', 'environment': {}, 'stdin': '',
                                           'timeout_seconds': 7200, 'outputs': [GUEST_REPORT]}]}, indent=2) + '\n')
    report = {'target': 'linux/arm64', 'package': str(inputs.package.relative_to(ROOT)),
              'compiler_sha256': digest(inputs.package / 'bin/lo'),
              'manifest_sha256': digest(inputs.package / 'toolchain.json'),
              'runtime_sha256': manifest['targets']['linux/arm64']['runtime']['sha256'],
              'frontend_emitter_sha256': digest(inputs.frontend), 'runner_sha256': digest(runner),
              'measurement_source_identity': {'tools/loqualification/performance.py': digest(Path(__file__)),
                                             'tools/nativebench/qualification.py': digest(Path(qualification.__file__))},
              'payload_identities': identities, 'sample_plan': dict(SAMPLE_PLAN), 'warmup': WARMUP,
              'lanes': inputs.lanes, 'cases': inputs.cases, 'accelerator': inputs.accelerator,
              'expected_inventory': inventory,
              'batch': str(batch.relative_to(ROOT)),
              'complete_contract': set(inputs.lanes) == set(LANES) and not inputs.cases}
    reference_files = [ROOT / 'go.mod', ROOT / 'go.sum', ROOT / 'tools/nativebench/main.go',
                       ROOT / 'tools/nativebench/harness.c.txt', ROOT / 'tools/nativebench/allocation.c.txt',
                       *sorted((ROOT / 'internal').rglob('*.go')), *sorted((ROOT / 'tests/corpus').rglob('*.go'))]
    reference_identity = {str(path.relative_to(ROOT)): digest(path) for path in reference_files if path.is_file()}
    report['reference_source_identity'] = reference_identity
    report['reference_go'] = execute([inputs.go, 'version'], host_env, inputs.work / 'go-version.log').decode().strip()
    report['host_tools_sha256'] = digest(inputs.preparation / 'tools.json')
    verify_provenance(ROOT, frozen)
    frozen['files'][str(runner)] = digest(runner)
    frozen['tools']['runner_sha256'] = digest(runner)
    report['frozen_provenance'] = frozen
    (inputs.work / 'preparation.json').write_text(json.dumps(report, indent=2) + '\n')
    return batch, report



def qualify(inputs: Inputs, prepare_only: bool) -> dict[str, Any]:
    manifest, identity = prepare(inputs)
    if prepare_only:
        return {'prepared': True, 'measurement_complete': False, 'qualification_complete': False, **identity}
    batch = run_batch(inputs.preparation, inputs.package, manifest, inputs.work / 'guest', accelerator=inputs.accelerator)
    exported = inputs.work / 'guest/0.file0'
    if not exported.is_file():
        raise ValueError(f'performance report missing; inspect {inputs.work / "guest/0.stderr"}')
    verify_provenance(ROOT, identity['frozen_provenance'])
    report = json.loads(exported.read_text())
    validate_sample_plan(report.get('sample_plan'))
    report.update(identity)
    report['cache'] = CACHE
    report['scope'] = 'diagnostic selection' if inputs.cases else 'full fixed corpus'
    report['merge_identity'] = merge_identity(
        root=ROOT, compiler=inputs.package / 'bin/lo', manifest=inputs.package / 'toolchain.json',
        target='linux/arm64', environment=report['environment'], accelerator=inputs.accelerator,
        producer_kind='linux-guest',
        producer_tools={'python': platform.python_version(), **identity['frozen_provenance']['tools']},
        producer_sources=PRODUCER_SOURCES,
        reference_go=identity['reference_go'])
    report['reference_source_identity'] = report['merge_identity']['reference']['source_identity']
    report['measurement_source_identity'] = {**report['merge_identity']['producer']['source_identity'],
                                             **report['merge_identity']['evaluator']}
    report['batch_report'] = str((inputs.work / 'guest/report.json').relative_to(ROOT))
    completion(report, batch['cases'][0]['status'])
    (inputs.work / 'report.json').write_text(json.dumps(report, indent=2) + '\n')
    return report


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('preparation', 'package', 'frontend', 'work'):
        parser.add_argument('--' + name, type=Path, required=True)
    parser.add_argument('--go', default='go')
    parser.add_argument('--case', dest='cases', action='append', default=[])
    parser.add_argument('--lane', choices=('execution', 'process', 'http', 'build', 'all'), default='all')
    parser.add_argument('--prepare-only', action='store_true')
    parser.add_argument('--accelerator', choices=('tcg', 'hvf'), default='tcg')
    args = parser.parse_args()
    try:
        lanes = LANES if args.lane == 'all' else (args.lane,)
        inputs = Inputs(local(args.preparation), local(args.package), local(args.frontend), local(args.work),
                        args.go, lanes, tuple(args.cases), args.accelerator)
        report = qualify(inputs, args.prepare_only)
        print(json.dumps(report, indent=2))
        return 0 if args.prepare_only or report['gates_passed'] else 1
    except (ValueError, OSError, KeyError, TypeError, subprocess.SubprocessError) as error:
        print(f'Linux performance qualification: {error}', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
