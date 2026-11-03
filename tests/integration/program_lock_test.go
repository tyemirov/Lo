package integration_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProgramLockEntry(t *testing.T) {
	path := sourceFile(t, `flow Main() -> File.Path! { return Program.path() }; return Main()`)
	artifact, built := buildForTest(t, path)
	if built.status != 0 {
		t.Fatalf("build entry observation: %+v", built)
	}
	got := runExecutable(t, artifact, "run")
	if got.status != 0 || got.stdout != artifact+"\n" || got.stderr != "" {
		t.Fatalf("compiled entry observation: %+v", got)
	}
}

func TestProgramLockPureTarget(t *testing.T) {
	path := sourceFile(t, `return Lock.File.new(File.Path.new("/ungranted-private-lock"))`)
	got := runProgram(t, path, "run", "--format", "json")
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, `"type":"Lock.File"`) || strings.Contains(got.stdout, "/ungranted-private-lock") {
		t.Fatalf("pure redacted lock target: %+v", got)
	}
}

func TestProgramLockAuthority(t *testing.T) {
	root := fileFixtureRoot(t)
	lockPath := root + "/lock"
	file, err := os.Create(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteString("existing")
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		t.Fatalf("create lock fixture: %v, %v", writeErr, closeErr)
	}
	path := sourceFile(t, fmt.Sprintf(`flow Main() -> Lock.Lease! { target = Lock.File.new(File.Path.new(%q)); lease = commit target; return lease }; return Main()`, lockPath))
	for _, grants := range [][]string{nil, {"--read-root", root}, {"--write-root", root}} {
		args := append([]string{"run", path}, grants...)
		got := runProgram(t, args[1], args[0], args[2:]...)
		if got.status != 2 {
			t.Fatalf("ordinary path authority granted lock activation: %+v", got)
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		got := runProgram(t, path, "run", "--lock-file", lockPath, "--format", "json")
		if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, `"type":"Lock.Lease"`) {
			t.Fatalf("lock acquisition and release: %+v", got)
		}
	}
	data, err := os.ReadFile(lockPath)
	if err != nil || string(data) != "existing" {
		t.Fatalf("lock activation changed file content: %q, %v", data, err)
	}
}

func TestProgramLockEntryGrant(t *testing.T) {
	path := sourceFile(t, `flow Main() -> Lock.Lease! { target = Lock.File.new(Program.path()); lease = commit target; return lease }; return Main()`)
	got := runProgram(t, path, "run", "--lock-entry", "--format", "json")
	if got.status != 0 || got.stderr != "" || !strings.Contains(got.stdout, `"type":"Lock.Lease"`) {
		t.Fatalf("entry lock authority: %+v", got)
	}
}

func programLockFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(fileFixtureRoot(t), "lock")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func programLockAcquire(path string) string {
	return fmt.Sprintf(`flow Failed(error: Error) -> String { return error.kind }; flow Main() -> String onError Failed { target = Lock.File.new(File.Path.new(%q)); lease = commit target; return "acquired" }; return Main()`, path)
}

func programLockServer(path string) string {
	return fmt.Sprintf(`flow Acquire() -> Int! { target = Lock.File.new(File.Path.new(%q)); lease = commit target; return 1 }; flow Handle(request: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200,"ok") }; flow OnError(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(500,error.kind) }; flow Main() -> Int! { ignored = Acquire(); server = HTTP.Server.new(0,Handle,OnError); commit server; return 0 }; return Main()`, path)
}

func TestProgramLockContentionAndCancellation(t *testing.T) {
	lockPath := programLockFile(t)
	holder := startServer(t, programLockServer(lockPath), "--lock-file", lockPath)
	contender := sourceFile(t, programLockAcquire(lockPath))
	assert := func(got result, want string) {
		t.Helper()
		if got.status != 0 || got.stdout != want+"\n" || got.stderr != "" {
			t.Fatalf("lock contention, want %s: %+v", want, got)
		}
	}
	assert(runProgram(t, contender, "run", "--lock-file", lockPath), "Lock.Busy")
	// A native holder also competes with an interpreted caller for this inode.
	assert(runProgram(t, contender, "run", "--lock-file", lockPath), "Lock.Busy")
	hardlink := filepath.Join(filepath.Dir(lockPath), "alias")
	if err := os.Link(lockPath, hardlink); err != nil {
		t.Fatal(err)
	}
	alias := sourceFile(t, programLockAcquire(hardlink))
	assert(runProgram(t, alias, "run", "--lock-file", hardlink), "Lock.Busy")
	stopServer(t, holder)
	assert(runProgram(t, contender, "run", "--lock-file", lockPath), "acquired")
}

func TestProgramLockRepeatedAcquisition(t *testing.T) {
	path := programLockFile(t)
	program := fmt.Sprintf(`flow Failed(error: Error) -> String { return error.kind }; flow Main() -> String onError Failed { target = Lock.File.new(File.Path.new(%q)); first = commit target; second = commit target; return "unexpected" }; return Main()`, path)
	got := runProgram(t, sourceFile(t, program), "run", "--lock-file", path)
	if got.status != 0 || got.stdout != "Lock.Busy\n" || got.stderr != "" {
		t.Fatalf("repeat acquisition bypassed independent kernel lock: %+v", got)
	}
	got = runProgram(t, sourceFile(t, programLockAcquire(path)), "run", "--lock-file", path)
	if got.status != 0 || got.stdout != "acquired\n" || got.stderr != "" {
		t.Fatalf("recoverable error retained lease after shutdown: %+v", got)
	}
}

func TestProgramLockReplacementGuarantee(t *testing.T) {
	path := programLockFile(t)
	holder := startServer(t, programLockServer(path), "--lock-file", path)
	original := filepath.Join(filepath.Dir(path), "original")
	if err := os.Rename(path, original); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ path, want string }{{original, "Lock.Busy"}, {path, "acquired"}} {
		got := runProgram(t, sourceFile(t, programLockAcquire(test.path)), "run", "--lock-file", test.path)
		if got.status != 0 || got.stdout != test.want+"\n" || got.stderr != "" {
			t.Fatalf("inode-scoped replacement guarantee, want %s: %+v", test.want, got)
		}
	}
	stopServer(t, holder)
}

func TestProgramLockSymlinkGrantDenied(t *testing.T) {
	path := programLockFile(t)
	alias := filepath.Join(filepath.Dir(path), "symlink")
	if err := os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	got := runProgram(t, sourceFile(t, programLockAcquire(alias)), "run", "--lock-file", alias)
	if got.status != 2 || got.stdout != "" {
		t.Fatalf("symlink lock grant accepted: %+v", got)
	}
}

func TestProgramLockObservationPurity(t *testing.T) {
	source := `flow Observe(index: Int) -> File.Path! { return Program.path() }; return List.map(List.new(1),Observe)`
	got := run(t, "check", sourceFile(t, source), "--format", "json")
	if got.status != 1 || !strings.Contains(got.stderr, "check.effectful-callback") {
		t.Fatalf("entry observation admitted in pure callback: %+v", got)
	}
}

func TestProgramLockNativeRelocation(t *testing.T) {
	path := sourceFile(t, `flow Main() -> File.Path! { target = Lock.File.new(Program.path()); lease = commit target; return Program.path() }; return Main()`)
	artifact, built := buildForTest(t, path)
	if built.status != 0 {
		t.Fatalf("build native entry fixture: %+v", built)
	}
	moved := filepath.Join(t.TempDir(), "relocated-program")
	if err := os.Rename(artifact, moved); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "image-alias")
	if err := os.Symlink(moved, alias); err != nil {
		t.Fatal(err)
	}
	for _, executable := range []string{moved, alias} {
		command := exec.Command(executable, "run", "--lock-entry")
		command.Args[0] = "/untrusted-argv-zero"
		command.Dir = repository
		var stdout, stderr bytes.Buffer
		command.Stdout, command.Stderr = &stdout, &stderr
		err := command.Run()
		if err != nil || stdout.String() != moved+"\n" || stderr.Len() != 0 {
			t.Fatalf("actual relocated image identity with forged argv[0]: %q, %q, %v", stdout.String(), stderr.String(), err)
		}
	}
}

func TestProgramLockSeparateContentAuthority(t *testing.T) {
	path := programLockFile(t)
	source := fmt.Sprintf(`flow Main() -> Int! { lock = Lock.File.new(File.Path.new(%q)); lease = commit lock; write = File.Write.new(File.Path.new(%q),Bytes.new("authorized content")); receipt = commit write; return receipt.bytes_written }; return Main()`, path, path)
	got := runProgram(t, sourceFile(t, source), "run", "--lock-file", path, "--write-root", filepath.Dir(path))
	if got.status != 0 || got.stdout != "18\n" || got.stderr != "" {
		t.Fatalf("lock grant changed independently admitted File content authority: %+v", got)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "authorized content" {
		t.Fatalf("authorized locked-file content: %q, %v", content, err)
	}
}

func TestProgramLockSameArtifactEntryContention(t *testing.T) {
	path := sourceFile(t, nativeEntryLockServer())
	artifact, built := buildForTest(t, path)
	if built.status != 0 {
		t.Fatalf("build entry contention: %+v", built)
	}
	holder := startServerArtifact(t, artifact, "--lock-entry")
	got := runExecutable(t, artifact, "run", "--lock-entry")
	if got.status != 0 || got.stdout != "Lock.Busy\n" || got.stderr != "" {
		t.Fatalf("same artifact bypassed its entry lease: %+v", got)
	}
	stopServer(t, holder)
}

func nativeEntryLockServer() string {
	return `flow Failed(error:Error)->String{return error.kind};flow Handle(request:HTTP.Request)->HTTP.Response{return HTTP.Response.Text.new(200,"ok")};flow OnError(error:Error)->HTTP.Response{return HTTP.Response.Text.new(500,error.kind)};flow Main()->String onError Failed{target=Lock.File.new(Program.path());lease=commit target;server=HTTP.Server.new(0,Handle,OnError);commit server;return "acquired"};return Main()`
}

func TestProgramLockSeparateArtifactsHaveSeparateEntries(t *testing.T) {
	path := sourceFile(t, nativeEntryLockServer())
	var artifacts []string
	for index := 0; index < 2; index++ {
		artifact := filepath.Join(t.TempDir(), fmt.Sprintf("entry-%d", index))
		built := run(t, "build", path, "--output", artifact, "--format", "json")
		if built.status != 0 {
			t.Fatalf("build independent entry: %+v", built)
		}
		artifacts = append(artifacts, artifact)
	}
	first := startServerArtifact(t, artifacts[0], "--lock-entry")
	second := startServerArtifact(t, artifacts[1], "--lock-entry")
	stopServer(t, second)
	stopServer(t, first)
}

func TestProgramLockFailedRepeatPreservesLease(t *testing.T) {
	path := programLockFile(t)
	source := strings.Replace(programLockServer(path), "flow Acquire() -> Int!", "flow Acquire() -> Int onError Repeated", 1)
	source = strings.Replace(source, "lease = commit target; return 1", "lease = commit target; again = commit target; return 1", 1)
	source = `flow Repeated(error: Error) -> Int { return 1 }; ` + source
	holder := startServer(t, source, "--lock-file", path)
	got := runProgram(t, sourceFile(t, programLockAcquire(path)), "run", "--lock-file", path)
	if got.status != 0 || got.stdout != "Lock.Busy\n" || got.stderr != "" {
		t.Fatalf("failed repeated acquisition released the original lease: %+v", got)
	}
	stopServer(t, holder)
}

func TestProgramLockExecutableEntryAlias(t *testing.T) {
	path := sourceFile(t, `flow Main() -> File.Path! { target = Lock.File.new(Program.path()); lease = commit target; return Program.path() }; return Main()`)
	artifact, built := buildForTest(t, path)
	if built.status != 0 {
		t.Fatalf("build entry alias: %+v", built)
	}
	alias := filepath.Join(t.TempDir(), "artifact-alias")
	if err := os.Symlink(artifact, alias); err != nil {
		t.Fatal(err)
	}
	got := runExecutable(t, alias, "run", "--lock-entry")
	if got.status != 0 || got.stdout != artifact+"\n" || got.stderr != "" {
		t.Fatalf("captured actual executable identity through alias: %+v", got)
	}
}

func TestProgramLockAncestorReplacement(t *testing.T) {
	root := fileFixtureRoot(t)
	ancestor, err := os.MkdirTemp(root, "ancestor-")
	if err != nil {
		t.Fatal(err)
	}
	parent, err := os.MkdirTemp(ancestor, "parent-")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "lock")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`flow Handle(request: HTTP.Request) -> HTTP.Response! { target = Lock.File.new(File.Path.new(%q)); lease = commit target; return HTTP.Response.Text.new(200,"acquired") }; flow OnError(error: Error) -> HTTP.Response { return HTTP.Response.Text.new(200,error.kind) }; server = HTTP.Server.new(0,Handle,OnError); commit server`, path)
	server := startServer(t, source, "--lock-file", path)
	original := ancestor + "-original"
	if err := os.Rename(ancestor, original); err != nil {
		t.Fatal(err)
	}
	replacement, err := os.MkdirTemp(root, "replacement-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, ancestor); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(original, filepath.Base(parent)), parent); err != nil {
		t.Fatal(err)
	}
	status, body := response(t, server, "GET", "/", "")
	if status != 200 || body != "Lock.Failed" {
		t.Fatalf("same leaf and parent bypassed changed ancestor identity: %d %q", status, body)
	}
	stopServer(t, server)
}

func TestProgramLockGrantLimit(t *testing.T) {
	path := programLockFile(t)
	arguments := []string{"run", sourceFile(t, "return 0"), "--format", "json"}
	for count := 0; count < 129; count++ {
		arguments = append(arguments, "--lock-file", path)
	}
	got := runProgram(t, arguments[1], arguments[0], arguments[2:]...)
	if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
		t.Fatalf("lock grant quota classification: %+v", got)
	}
}
