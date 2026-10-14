"""Measure the fixed native corpus through public CLI entry points."""
from __future__ import annotations
import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import platform
import signal
import statistics
import subprocess
import time


def measured(command: list[str], cwd: Path) -> tuple[int, str]:
    start = time.perf_counter_ns()
    result = subprocess.run(command, cwd=cwd, text=True, capture_output=True)
    elapsed = time.perf_counter_ns() - start
    if result.returncode or result.stderr:
        raise RuntimeError(f"{command}: exit {result.returncode}: {result.stderr}")
    return elapsed, result.stdout.strip()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--workdir', type=Path, required=True)
    parser.add_argument('--samples', type=int, default=10)
    parser.add_argument('--native', action='store_true')
    args = parser.parse_args()
    if args.samples < 10:
        parser.error('at least ten samples are required')
    root = Path(__file__).resolve().parents[1]
    binary = args.binary.resolve()
    work = args.workdir.resolve()
    work.mkdir(parents=True, exist_ok=False)
    corpus = json.loads(subprocess.check_output(['go', 'run', './tools/nativecorpus'], cwd=root))
    corpus.append({'name': 'file', 'source': 'flow F() -> Int! { text = UTF8.String.new(File.read(File.Path.new("input.txt"))); effect = File.write(File.Path.new("output.txt"), Bytes.new(text)); commit effect; return String.length(text) }\nreturn F()\n', 'expected': '30000'})
    payload = 'abc' * 10000
    (work / 'input.txt').write_text(payload)
    records: list[dict] = []
    for case in corpus:
        source = work / (case['name'] + '.lo')
        source.write_text(case['source'])
        command = [str(binary), 'run', str(source)]
        if args.native:
            artifact = work / case['name']
            measured([str(binary), 'build', str(source), '--output', str(artifact)], work)
            command = [str(artifact), 'run']
        command.extend(['--max-steps', '10000000'])
        if case['name'] == 'file':
            command.extend(['--read-root', str(work), '--write-root', str(work)])
        samples: list[int] = []
        for index in range(args.samples + 3):
            elapsed, result = measured(command, work)
            if result != case['expected']:
                raise RuntimeError(f"{case['name']}: {result!r} != {case['expected']!r}")
            if case['name'] == 'file' and (work / 'output.txt').read_text() != payload:
                raise RuntimeError('incorrect file output')
            if index >= 3:
                samples.append(elapsed)
        records.append({'name': case['name'], 'source_sha256': hashlib.sha256(case['source'].encode()).hexdigest(), 'samples_ns': samples, 'median_ns': statistics.median(samples), 'expected': case['expected']})
    for name, source_text in [('echo', (root / 'examples/http_echo.lo').read_text()), ('computed', 'flow H(r: HTTP.Request) -> HTTP.Response { n = String.length(r.path); return HTTP.Response.Text.new(200, Int.to_string(n)) }\nflow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(400, e.kind) }\nserver = HTTP.Server.new(0,H,E); commit server\n')]:
        source = work / ('http_' + name + '.lo')
        source.write_text(source_text)
        command = [str(binary), 'serve', str(source)]
        if args.native:
            artifact = work / ('http_' + name)
            measured([str(binary), 'build', str(source), '--output', str(artifact)], work)
            command = [str(artifact), 'serve']
        command.extend(['--listen', '0', '--format', 'json'])
        process = subprocess.Popen(command, cwd=work, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            assert process.stdout is not None
            readiness = json.loads(process.stdout.readline())
            address = readiness['servers'][0]['address']
            connection = http.client.HTTPConnection(address, timeout=5)
            blocks: list[dict] = []
            for block in range(args.samples + 3):
                times: list[int] = []
                start = time.perf_counter_ns()
                for _ in range(100):
                    request_start = time.perf_counter_ns()
                    connection.request('POST', '/benchmark', body=b'fixed varied payload 123')
                    response = connection.getresponse()
                    body = response.read()
                    expected = b'fixed varied payload 123' if name == 'echo' else b'10'
                    if response.status != 200 or body != expected:
                        raise RuntimeError(f'HTTP response: {response.status} {body!r}')
                    times.append(time.perf_counter_ns() - request_start)
                if block >= 3:
                    elapsed = time.perf_counter_ns() - start
                    blocks.append({'elapsed_ns': elapsed, 'requests_per_second': 100e9 / elapsed, 'median_ns': statistics.median(times), 'p95_ns': sorted(times)[94], 'samples_ns': times})
            connection.close()
            records.append({'name': 'http_' + name, 'concurrency': 1, 'requests_per_sample': 100, 'source_sha256': hashlib.sha256(source_text.encode()).hexdigest(), 'samples': blocks})
        finally:
            process.send_signal(signal.SIGTERM)
            output, errors = process.communicate(timeout=10)
            if process.returncode or errors:
                raise RuntimeError(f'HTTP shutdown: {process.returncode} {errors} {output}')
    print(json.dumps({'system': platform.platform(), 'machine': platform.machine(), 'processor': subprocess.check_output(['sysctl', '-n', 'machdep.cpu.brand_string'], text=True).strip() if platform.system() == 'Darwin' else platform.processor(), 'go': subprocess.check_output(['go', 'version'], text=True).strip(), 'binary_sha256': hashlib.sha256(binary.read_bytes()).hexdigest(), 'backend': 'native' if args.native else 'interpreter', 'max_steps': 10000000, 'warmup': 3, 'samples': args.samples, 'cache': 'warm filesystem and Go build cache', 'concurrency': 1, 'records': records}, indent=2))


if __name__ == '__main__':
    main()
