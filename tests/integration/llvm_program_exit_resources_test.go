package integration_test

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestLLVMProgramExitRetainedBytes(t *testing.T) {
	if os.Getenv("LO_TEST_BACKEND") != "llvm" {
		t.Skip("requires LLVM runtime inputs")
	}
	stdout, stderr, extra := strings.Repeat("a", 400), strings.Repeat("b", 400), strings.Repeat("c", 800)
	fixtures := []struct {
		name, makeBody, output, useBody, mainBody string
		live, value                               uint64
	}{
		{"distinct-return", fmt.Sprintf(`stdout=Bytes.new(%q);stderr=Bytes.new(%q);return Program.Exit.new(0,stdout,stderr)`, stdout, stderr), "Program.Exit", fmt.Sprintf(`extra=Bytes.new(%q);return 1`, extra), `terminal=Make();return Use(terminal)`, 4242, 872},
		{"shared-return", fmt.Sprintf(`stdout=Bytes.new(%q);return Program.Exit.new(0,stdout,stdout)`, stdout), "Program.Exit", fmt.Sprintf(`extra=Bytes.new(%q);return 1`, extra), `terminal=Make();return Use(terminal)`, 3826, 872},
		{"record-alias-return", fmt.Sprintf(`stdout=Bytes.new(%q);stderr=Bytes.new(%q);terminal=Program.Exit.new(0,stdout,stderr);return Record.new(terminal:terminal,stdout:stdout)`, stdout, stderr), "Record{terminal:Program.Exit,stdout:Bytes}", fmt.Sprintf(`extra=Bytes.new(%q);return 1`, extra), `container=Make();return Use(container)`, 6028, 1318},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			parameter := "terminal"
			if fixture.name == "record-alias-return" {
				parameter = "container"
			}
			source := fmt.Sprintf(`flow Make()->%s!{%s};flow Use(%s:%s)->Int!{%s};flow Main()->Int!{%s};return Main()`, fixture.output, fixture.makeBody, parameter, fixture.output, fixture.useBody, fixture.mainBody)
			path := sourceFile(t, source)
			artifact, built := buildForTest(t, path)
			if built.status != 0 {
				t.Fatalf("build retained terminal fixture: %+v", built)
			}
			for _, limit := range []struct {
				flag    string
				quota   uint64
				success bool
			}{
				{"--max-live-bytes", 3410, false}, {"--max-live-bytes", fixture.live - 1, false}, {"--max-live-bytes", fixture.live, true},
				{"--max-value-bytes", fixture.value - 1, false}, {"--max-value-bytes", fixture.value, true},
			} {
				t.Run(fmt.Sprintf("%s-%d", limit.flag, limit.quota), func(t *testing.T) {
					for _, backend := range []struct {
						name, executable string
						arguments        []string
					}{
						{"interpreter", binary, []string{"run", path}}, {"native", artifact, []string{"run"}},
					} {
						t.Run(backend.name, func(t *testing.T) {
							arguments := append(append([]string{}, backend.arguments...), limit.flag, fmt.Sprint(limit.quota))
							got := runExecutable(t, backend.executable, arguments...)
							if limit.success {
								if got.status != 0 || got.stdout != "1\n" || got.stderr != "" {
									t.Fatalf("exact resource boundary: %+v", got)
								}
							} else if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
								t.Fatalf("retained byte identities escaped quota: %+v", got)
							}
						})
					}
				})
			}
		})
	}
}
