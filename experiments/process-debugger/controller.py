"""Capture only bounded stop metadata from the parent's owned remote process."""
from __future__ import annotations

import json
from pathlib import Path
import time

import lldb


def capture(debugger: lldb.SBDebugger, expected_pid: int, port: int, output: str) -> None:
    report: dict[str, object] = {"connected": False, "continued": False, "destroyed": False}
    process = None
    destroy_owned_process = False
    try:
        debugger.SetAsync(True)
        target = debugger.GetSelectedTarget()
        error = lldb.SBError()
        process = target.ConnectRemote(debugger.GetListener(), f"connect://127.0.0.1:{port}", "gdb-remote", error)
        if not error.Success():
            raise ValueError("remote connection failed")
        report["connected"] = True
        if process.GetProcessID() != expected_pid:
            raise ValueError("remote process identity differs")
        destroy_owned_process = True
        if process.GetState() != lldb.eStateStopped:
            raise ValueError("remote process is not stopped")
        initial_stop_id = process.GetStopID()
        if not process.Continue().Success():
            raise ValueError("owned process continue failed")
        report["continued"] = True
        deadline = time.monotonic() + 4
        event = lldb.SBEvent()
        while time.monotonic() < deadline:
            debugger.GetListener().WaitForEvent(1, event)
            state = process.GetState()
            if state in (lldb.eStateStopped, lldb.eStateCrashed, lldb.eStateExited, lldb.eStateDetached):
                if state in (lldb.eStateStopped, lldb.eStateCrashed) and process.GetStopID() <= initial_stop_id:
                    continue
                report["state"] = int(state)
                if state == lldb.eStateDetached:
                    raise ValueError("owned process detached unexpectedly")
                threads: list[dict[str, object]] = []
                if state != lldb.eStateExited:
                    for thread_index in range(min(process.GetNumThreads(), 4)):
                        thread = process.GetThreadAtIndex(thread_index)
                        frames = []
                        for frame_index in range(min(thread.GetNumFrames(), 12)):
                            frame = thread.GetFrameAtIndex(frame_index)
                            frames.append({"name": (frame.GetFunctionName() or "")[:160], "address": hex(frame.GetPC())})
                        threads.append({"stop_reason": int(thread.GetStopReason()), "stop_description": (thread.GetStopDescription(256) or "")[:256], "frames": frames})
                report["threads"] = threads
                if state == lldb.eStateExited:
                    report["exit_status"] = process.GetExitStatus()
                break
        else:
            raise ValueError("owned process stop deadline exceeded")
    except Exception as error:
        report["error"] = str(error)[:256]
    finally:
        if process is not None and destroy_owned_process:
            try:
                report["destroyed"] = process.Destroy().Success()
            except Exception:
                report["cleanup_error"] = "owned process destroy failed"
        with Path(output).open("x") as destination:
            destination.write(json.dumps(report, indent=2, sort_keys=True) + "\n")
