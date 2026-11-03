"""Public migration contracts for the sole compiled application runtime."""

from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]


def test_public_generator_has_no_interpreter_program(tmp_path: Path) -> None:
    result = subprocess.run(
        ["go", "run", "./tools/nativebench", "--output", str(tmp_path)],
        cwd=ROOT,
        stdin=subprocess.DEVNULL,
        capture_output=True,
        text=True,
    )
    assert result.returncode == 0, result.stderr
    fixture = tmp_path / "startup"
    assert not (fixture / "program_test.go").exists()
    assert (fixture / "oracle.json").is_file()
    assert '#include "lo_runtime.h"' in (fixture / "benchmark.c").read_text()
    assert "lo_context_close" in (fixture / "benchmark.c").read_text()


def test_filebench_rejects_removed_runtime_selector(tmp_path: Path) -> None:
    result = subprocess.run(
        [
            "go",
            "run",
            "./tools/filebench",
            "-backend",
            "native",
            "-work",
            str(tmp_path),
        ],
        cwd=ROOT,
        stdin=subprocess.DEVNULL,
        capture_output=True,
        text=True,
    )
    assert result.returncode != 0
    assert "flag provided but not defined: -backend" in result.stderr


def test_current_qualification_rejects_paired_samples() -> None:
    sys.path.insert(0, str(ROOT / "tools/nativebench"))
    import qualification

    report = {
        "sample_plan": dict(qualification.SAMPLE_PLAN),
        "warmup": 3,
        "measurement_complete": True,
        "concurrency": 1,
        "other_benchmark_jobs": [],
        "benchmark_job_observation": "owned fixture",
        "expected_inventory": {"process": ["startup"]},
        "complete_contract": False,
        "process": [
            {
                "name": "startup",
                "verified": True,
                "samples": [{"sample": index, "wall_ns": 40} for index in range(10)],
            }
        ],
    }
    assert qualification.evaluate(report) == []
    report["process"][0]["samples"][0] = {"sample": 0, "interpreter": 100, "native": 40}
    try:
        qualification.evaluate(report)
    except ValueError:
        return
    raise AssertionError("obsolete paired timing accepted")
