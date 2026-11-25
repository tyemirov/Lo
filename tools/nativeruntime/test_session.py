"""Public execution contracts for the persistent Linux test guest."""
from __future__ import annotations

import base64
import json
import os
from pathlib import Path
import platform
import socket
import struct
import subprocess
import sys
import threading

import pytest

ROOT = Path(__file__).resolve().parents[2]
SCRIPT = Path(__file__).with_name("session.py")


def test_session_rejects_unbounded_workers_before_workspace_creation(tmp_path: Path) -> None:
    work = tmp_path / "guest"
    result = subprocess.run(
        [sys.executable, "-B", str(SCRIPT), "--preparation", str(ROOT / "_scratch/native-runtime"),
         "--work", str(work), "--socket", str(tmp_path / "s"), "--workers", "0",
         "--accelerator", "hvf"],
        cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False,
    )
    assert result.returncode == 1
    assert "guest workers must be between 1 and 4" in result.stderr
    assert not work.exists()


@pytest.mark.parametrize("workers", [3, 4])
def test_session_accepts_bounded_workers_without_starting_guests(tmp_path: Path, workers: int) -> None:
    import session

    work = tmp_path / "guest"
    pool = session.GuestPool(ROOT / "_scratch/native-runtime", work, workers, accelerator="hvf")
    assert pool.workers == workers
    assert not work.exists()


def test_session_rejects_five_workers_before_workspace_creation(tmp_path: Path) -> None:
    work = tmp_path / "guest"
    result = subprocess.run(
        [sys.executable, "-B", str(SCRIPT), "--preparation", str(ROOT / "_scratch/native-runtime"),
         "--work", str(work), "--socket", str(tmp_path / "s"), "--workers", "5", "--accelerator", "hvf"],
        cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, text=True, check=False,
    )
    assert result.returncode == 1
    assert "guest workers must be between 1 and 4" in result.stderr
    assert not work.exists()


@pytest.mark.parametrize("response", [b"", b"short", struct.pack("!8I", 0x4C4F5231, 99, 0, 1, 0, 0, 0, 0)])
def test_guest_transport_rejects_truncated_or_wrong_case_result(response: bytes) -> None:
    import session

    host, guest = socket.socketpair()
    def respond() -> None:
        with guest:
            header = session.receive(guest, session.HEADER.size)
            fields = session.HEADER.unpack(header)
            session.receive(guest, sum(fields[3:7]))
            guest.sendall(response)
    thread = threading.Thread(target=respond)
    thread.start()
    with host, pytest.raises(ValueError, match="guest (transport|result)"):
        session.exchange(host, 1, b"ELF", b"", b"probe\0", b"", 1000, False)
    thread.join()


def test_session_executes_cases_without_filesystem_or_descendant_leakage(tmp_path: Path) -> None:
    import session

    supplied = os.environ.get("LO_NATIVE_PREPARATION")
    if supplied is None:
        pytest.skip("LO_NATIVE_PREPARATION selects the prepared Linux guest")
    preparation = session.local(Path(supplied))
    distribution = preparation / "distribution"
    manifest = json.loads((distribution / "toolchain.json").read_text())
    target = manifest["targets"]["linux/arm64"]
    source = tmp_path / "probe.c"
    source.write_text(r'''
extern int access(const char *,int), close(int),rename(const char *,const char *),unlink(const char *);
typedef struct stream FILE;extern FILE *fopen(const char *,const char *);extern int fclose(FILE *);
extern int fork(void), setsid(void), usleep(unsigned int), puts(const char *),execv(const char *,char *const[]);
extern long read(int,void *,unsigned long),write(int,const void *,unsigned long);
extern char *getenv(const char *),*getcwd(char *,unsigned long);
extern long readlink(const char *,char *,unsigned long);extern int strcmp(const char *,const char *);
int main(int argc,char **argv){
 if(argc>1&&!strcmp(argv[1],"/companion")){execv(argv[1],argv+1);return 79;}
 if(argc>1&&argv[1][0]=='r'){if(rename("/probe","/moved"))return 76;puts("renamed");return 0;}
 if(argc>1&&argv[1][0]=='c'){if(unlink(".lo-native-capture"))return 77;puts("capture");return 0;}
 if(argc>1&&argv[1][0]=='t'){int child=fork();if(child<0)return 78;if(!child)setsid();for(;;)usleep(1000);}
 if(argc>1 && argv[1][0]=='w'){
  FILE *file=fopen("/sentinel","wb");if(!file)return 70;fclose(file);
  int child=fork();if(child<0)return 71;
  if(!child){setsid();for(;;)usleep(10000);}
  puts("written");return 7;
 }
 if(access("/sentinel",0)==0)return 72;
 if(!getenv("POOL_CASE")||strcmp(getenv("POOL_CASE"),"second"))return 73;
 char cwd[128];if(!getcwd(cwd,sizeof(cwd))||strcmp(cwd,"/work"))return 74;
 char image[128]={0};if(readlink("/proc/self/exe",image,sizeof(image)-1)<0||strcmp(image,!strcmp(argv[0],"/companion")?"/companion":"/probe"))return 75;
 char data[32];long count=read(0,data,sizeof(data));if(count>0)write(1,data,count);
 return 0;
}
''')
    executable = tmp_path / "probe"
    obj = tmp_path / "probe.o"
    values = {"{object}": str(obj), "{output}": str(executable),
              "{runtime}": str(distribution / target["runtime"]["path"]),
              **{"{input:" + name + "}": str(distribution / pin["path"])
                 for name, pin in target["inputs"].items()}}
    for command in [
        [str(distribution / manifest["compiler"]["path"]), "--target=" + target["triple"],
         "-c", str(source), "-o", str(obj)],
        [str(distribution / target["linker"]["path"]), *[values.get(arg, arg) for arg in target["link_args"]]],
    ]:
        result = subprocess.run(command, cwd=ROOT, stdin=subprocess.DEVNULL, capture_output=True, check=False)
        assert result.returncode == 0, result.stderr
    accelerator = "hvf" if sys.platform == "darwin" and platform.machine() == "arm64" else "tcg"
    with session.GuestPool(preparation, tmp_path / "pool", 1, accelerator=accelerator) as pool:
        command = pool.guests[0].process.args
        assert command[command.index("-accel") + 1] == accelerator
        assert command[command.index("-cpu") + 1] == ("host" if accelerator == "hvf" else "cortex-a72")
        first = pool.execute({"executable": str(executable), "arguments": ["write"], "stdin": "",
                              "environment": {"POOL_CASE": "first"}})
        assert first["status"] == 7
        assert base64.b64decode(first["stdout"]) == b"written\n"
        second = pool.execute({"executable": str(executable), "arguments": [], "cwd": "/work",
                               "environment": {"POOL_CASE": "second"},
                               "stdin": base64.b64encode(b"fresh\n").decode()})
        assert second["status"] == 0
        assert base64.b64decode(second["stdout"]) == b"fresh\n"
        renamed = pool.execute({"executable": str(executable), "arguments": ["rename"]})
        assert renamed["status"] == 0
        capture = tmp_path / ".lo-native-capture"
        capture.write_bytes(b"")
        captured = pool.execute({"executable": str(executable), "arguments": ["capture"],
                                 "capture_file": str(capture), "cwd": "/work"})
        assert captured["status"] == 0
        assert base64.b64decode(captured["stdout"]) == b"capture\n"
        timed_out = pool.execute({"executable": str(executable), "arguments": ["timeout"], "timeout_ms": 25})
        assert timed_out["status"] == 124
        companion = pool.execute({"executable": str(executable), "companion": str(executable), "arguments": [],
                                  "cwd": "/work", "environment": {"POOL_CASE": "second"},
                                  "stdin": base64.b64encode(b"companion\n").decode()})
        assert companion["status"] == 0
        assert base64.b64decode(companion["stdout"]) == b"companion\n"
        assert pool.boots == 1
    assert not (tmp_path / "pool").exists()
