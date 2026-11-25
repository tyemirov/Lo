"""Installed artifacts execute through the shared isolated guest session."""
from __future__ import annotations

import base64
import json
from pathlib import Path
import socket
import threading
import uuid

import pytest

import acceptance


@pytest.mark.parametrize("problem", [None, "truncated", "status", "base64", "unknown", "error"])
def test_linux_execution_uses_shared_session_and_validates_response(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch, problem: str | None
) -> None:
    endpoint = acceptance.ROOT / "_scratch" / (".guest-" + uuid.uuid4().hex[:8])
    executable = tmp_path / "program"
    executable.write_bytes(b"\x7fELFcontrolled artifact")
    expected = '{"result":{"value":7}}\n'
    response: dict[str, object] = {"status": 0, "stdout": base64.b64encode(expected.encode()).decode(),
                                    "stderr": "", "elapsed_ns": 1234}
    if problem == "status": response["status"] = True
    elif problem == "base64": response["stdout"] = "invalid-base64!"
    elif problem == "unknown": response["unexpected"] = 1
    elif problem == "error": response = {"error": "declared guest worker failure"}
    monkeypatch.setenv("LO_TEST_GUEST_SOCKET", str(endpoint))
    def obsolete(*_args: object) -> None:
        raise AssertionError("Linux acceptance must reuse the shared guest")
    monkeypatch.setattr(acceptance, "execute", obsolete)
    requests: list[dict] = []
    with socket.socket(socket.AF_UNIX, socket.SOCK_STREAM) as listener:
        listener.bind(str(endpoint)); listener.listen(1); listener.settimeout(2)
        def serve() -> None:
            try:
                connection, _ = listener.accept()
            except TimeoutError:
                return
            with connection, connection.makefile("rb") as source:
                requests.append(json.loads(source.readline()))
                wire = json.dumps(response).encode()
                connection.sendall(wire if problem == "truncated" else wire + b"\n")
        thread = threading.Thread(target=serve); thread.start()
        try:
            if problem:
                with pytest.raises(ValueError):
                    acceptance.linux_run(tmp_path, executable, tmp_path / "work")
            else:
                result = acceptance.linux_run(tmp_path, executable, tmp_path / "work")
                assert result["status"] == 0 and result["stdout"] == expected and result["stderr"] == ""
                assert result["seconds"] > 0
        finally:
            thread.join(timeout=3)
            endpoint.unlink(missing_ok=True)
    assert len(requests) == 1
    assert requests[0]["executable"] == str(executable)
    assert requests[0]["arguments"] == ["run", "--format", "json"]


def test_linux_execution_requires_selected_session(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("LO_TEST_GUEST_SOCKET", raising=False)
    monkeypatch.setattr(acceptance, "execute", lambda *_args: pytest.fail("obsolete one-shot guest"))
    with pytest.raises(ValueError, match="LO_TEST_GUEST_SOCKET"):
        acceptance.linux_run(tmp_path, tmp_path / "program", tmp_path / "work")
