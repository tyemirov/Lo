"""Exercise the actual timing driver with an opaque ABI and deterministic clock."""
from pathlib import Path
import hashlib
import json
import os
import platform
import subprocess

import pytest

ROOT = Path(__file__).resolve().parents[2]
FAKE_ABI = r'''
typedef __UINT64_TYPE__ U64;
typedef __UINT32_TYPE__ U32;
typedef __SIZE_TYPE__ Size;
typedef struct { U64 steps,value_bytes,live_bytes,data_bytes,list_items,file_entries,file_directories; } ContextOptions;
typedef struct Program { int live; } Program;
typedef struct Context { int live; } Context;
typedef struct Value { int live; } Value;
typedef struct { U32 status; U32 code; const Value *value; } ResultSlot;
struct timespec { long seconds; long nanoseconds; };
extern int lo_harness_main(int,const char **,const char **);
extern long write(int,const void *,Size);
static Program program;
static Context context;
static Value result_value;
static U64 admitted, created, ran, freed, released, clock_reads, context_checks, value_checks, verified;
static U64 clock_ns, cost=10000;
static int phase, mode, violation;
static int equal(const char *left,const char *right) {
 while(*left && *left==*right) {++left;++right;} return *left==*right;
}
const Program *lo_program_new(void) {
 if(program.live || phase) violation=1;
 program.live=1; ++admitted; return &program;
}
void lo_program_free(const Program *value) {
 if(value!=&program || !program.live || phase) violation=2;
 program.live=0;
}
Context *lo_context_from_program(const Program *value,const ContextOptions *options) {
 if(value!=&program || !program.live || !options || options->steps!=10000000 || options->value_bytes!=(16ULL<<20) || options->live_bytes!=(64ULL<<20) || options->data_bytes || options->list_items || options->file_entries || options->file_directories!=128 || phase) violation=3;
 context.live=1; phase=1; ++created; return &context;
}
void lo_context_run(Context *value,ResultSlot *out) {
 if(value!=&context || !context.live || phase!=1) violation=4;
 phase=2; ++ran; clock_ns+=cost; result_value.live=1;
 out->status=mode==5?1:0; out->code=out->status;
 out->value=mode==6?0:&result_value;
}
void lo_context_free(Context *value) {
 if(value!=&context || !context.live || phase!=2) violation=5;
 context.live=0; phase=3; ++freed;
}
void lo_value_release(const Value *value) {
 if(value!=&result_value || !result_value.live || phase!=3 || context.live) violation=6;
 result_value.live=0; phase=0; ++released;
}
U64 lo_runtime_live_contexts(void) {++context_checks;return context.live;}
U64 lo_runtime_live_values(void) {++value_checks;return (program.live?5:0)+result_value.live;}
int lo_test_clock(int clock,struct timespec *out) {
 (void)clock; ++clock_reads;
 if(phase) violation=7;
 if(mode==1) return -1;
 U64 value;
 if(mode==2) value=0;
 else if(mode==3) value=clock_reads%2?1000:999;
 else if(mode==4) value=1000;
 else {if(mode!=7 || clock_reads%2==0 || clock_reads==1)clock_ns+=100;value=clock_ns;}
 out->seconds=value/1000000000ULL;out->nanoseconds=value%1000000000ULL;return 0;
}
int lo_cli_main(int argc,const char **argv,const char **environment) {
 (void)environment; ++verified;
 if(argc!=4 || !equal(argv[1],"run") || !equal(argv[2],"--max-steps") || !equal(argv[3],"10000000")) return 18;
 return 17;
}
static void number(U64 value) {
 char buffer[32];unsigned start=sizeof(buffer),end=start;
 do {buffer[--start]='0'+value%10;value/=10;}while(value);
 write(2,buffer+start,end-start);
}
static void field(const char *name,U64 value) {
 const char *end=name;while(*end)++end;write(2,name,end-name);number(value);
}
int main(int argc,const char **argv,const char **environment) {
 if(argc==2 && equal(argv[1],"slow")) cost=600000;
 else if(argc==2 && equal(argv[1],"very-slow")) cost=2000000;
 else if(argc==2 && equal(argv[1],"clock-error")) mode=1;
 else if(argc==2 && equal(argv[1],"clock-zero")) mode=2;
 else if(argc==2 && equal(argv[1],"clock-backwards")) mode=3;
 else if(argc==2 && equal(argv[1],"clock-equal")) mode=4;
 else if(argc==2 && equal(argv[1],"bad-status")) mode=5;
 else if(argc==2 && equal(argv[1],"null-result")) mode=6;
 else if(argc==2 && equal(argv[1],"equal-boundary")) mode=7;
 const char *arguments[]={argv[0],0};
 int status=(argc==2 && equal(argv[1],"--verify"))?lo_harness_main(argc,argv,environment):lo_harness_main(1,arguments,environment);
 field("{\"created\":",created);field(",\"ran\":",ran);field(",\"freed\":",freed);field(",\"released\":",released);
 field(",\"clock_reads\":",clock_reads);field(",\"context_checks\":",context_checks);field(",\"value_checks\":",value_checks);
 field(",\"admitted\":",admitted);field(",\"verified\":",verified);field(",\"violation\":",violation);write(2,"}\n",2);
 return violation?90:status;
}
'''


def pinned(base: Path, identity: dict[str, str]) -> Path:
    path = (base / identity['path']).resolve()
    path.relative_to(ROOT)
    assert hashlib.sha256(path.read_bytes()).hexdigest() == identity['sha256'], path
    return path


@pytest.fixture(scope='module')
def driver(tmp_path_factory: pytest.TempPathFactory) -> Path:
    work = tmp_path_factory.mktemp('native-harness').resolve()
    work.relative_to(ROOT)
    preparation = Path(os.environ.get('LO_NATIVE_PREPARATION', ROOT / '_scratch/native-runtime')).resolve()
    preparation.relative_to(ROOT)
    tools = json.loads((preparation / 'tools.json').read_text())['tools']
    compiler = pinned(ROOT, tools['clang'])
    manifest = Path(os.environ.get('LO_LLVM_TOOLCHAIN', preparation / 'distribution/toolchain.json')).resolve()
    manifest.relative_to(ROOT)
    distribution = json.loads(manifest.read_text())
    target_name = platform.system().lower() + '/arm64'
    target = distribution['targets'][target_name]
    linker = pinned(ROOT, tools['ld64.lld' if target_name.startswith('darwin/') else 'ld.lld'])
    environment = {'CI': '1', 'PATH': '', 'TMPDIR': str(work), 'LC_ALL': 'C', 'ZERO_AR_DATE': '1'}

    def run(arguments: list[str]) -> None:
        result = subprocess.run(arguments, cwd=work, env=environment, stdin=subprocess.DEVNULL,
                                capture_output=True, text=True)
        assert result.returncode == 0, result.stderr

    abi = work / 'fake.c'
    abi.write_text(FAKE_ABI)
    harness_object, abi_object, executable = work / 'harness.o', work / 'fake.o', work / 'driver'
    flags = ['-target', target['triple'], '-O2', '-ffp-contract=off', '-nostdinc']
    if target_name.startswith('darwin/'):
        flags += ['-mmacosx-version-min=27.0']
    run([str(compiler), *flags, '-Dmain=lo_harness_main', '-Dclock_gettime=lo_test_clock',
         '-x', 'c', '-c', str(ROOT / 'tools/nativebench/harness.c.txt'), '-o', str(harness_object)])
    run([str(compiler), *flags, '-c', str(abi), '-o', str(abi_object)])
    arguments = []
    for item in target['link_args']:
        if item == '{object}':
            arguments += [str(harness_object), str(abi_object)]
        elif item == '{runtime}':
            continue
        elif item == '{output}':
            arguments.append(str(executable))
        elif item.startswith('{input:'):
            arguments.append(str(pinned(manifest.parent, target['inputs'][item[7:-1]])))
        else:
            arguments.append(item)
    run([str(linker), *arguments])
    return executable


def invoke(driver: Path, mode: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run([str(driver), mode], cwd=driver.parent,
                          env={'CI': '1', 'PATH': '', 'TMPDIR': str(driver.parent)},
                          stdin=subprocess.DEVNULL, text=True, capture_output=True, timeout=10)


@pytest.mark.parametrize(('mode', 'batch', 'cost'), [('fast', 64, 10000), ('slow', 2, 600000), ('very-slow', 1, 2000000), ('equal-boundary', 64, 10000)])
def test_calibration_amortizes_clock_and_preserves_fresh_sessions(driver: Path, mode: str, batch: int, cost: int) -> None:
    observed = invoke(driver, mode)
    assert observed.returncode == 0, observed.stderr
    report, abi = json.loads(observed.stdout), json.loads(observed.stderr)
    assert 'batch_size' in report, 'timing driver must report its calibrated batch size'
    assert report['batch_size'] == batch
    assert report['iterations'] > 0 and report['iterations'] % batch == 0
    assert report['ns'] == cost + 100 // batch
    assert report['total_ns'] == report['iterations'] // batch * (batch * cost + 100)
    calibration_rounds = batch.bit_length()
    calibration_iterations = 2 * batch - 1
    batches = report['iterations'] // batch
    assert abi['created'] == report['iterations'] + calibration_iterations
    assert abi['created'] == abi['ran'] == abi['freed'] == abi['released']
    assert abi['clock_reads'] == 2 * (calibration_rounds + batches)
    assert abi['context_checks'] == calibration_rounds + batches
    assert abi['value_checks'] == abi['context_checks'] + 2
    assert abi['violation'] == 0 and abi['admitted'] == 1 and abi['verified'] == 0


@pytest.mark.parametrize('mode', ['clock-error', 'clock-zero', 'clock-backwards', 'clock-equal'])
def test_driver_rejects_invalid_clock(driver: Path, mode: str) -> None:
    observed = invoke(driver, mode)
    assert observed.returncode != 0, observed.stdout
    assert observed.stdout == ''


@pytest.mark.parametrize('mode', ['bad-status', 'null-result'])
def test_every_iteration_checks_runtime_result(driver: Path, mode: str) -> None:
    observed = invoke(driver, mode)
    assert observed.returncode != 0 and observed.stdout == ''


def test_verify_uses_real_cli_outside_timing(driver: Path) -> None:
    observed = invoke(driver, '--verify')
    assert observed.returncode == 17 and observed.stdout == ''
    abi = json.loads(observed.stderr)
    assert abi['verified'] == 1
    assert abi['created'] == abi['clock_reads'] == abi['admitted'] == abi['violation'] == 0
