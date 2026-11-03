package integration_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func f030MinimumLive(t *testing.T, source string) uint64 {
	t.Helper()
	artifact, built := buildForTest(t, sourceFile(t, source))
	if built.status != 0 {
		t.Fatalf("build resource fixture: %+v", built)
	}
	admitted := func(quota uint64) bool {
		got := runExecutable(t, artifact, "run", "--max-steps", "10000000", "--max-live-bytes", strconv.FormatUint(quota, 10))
		if got.status == 0 && got.stderr == "" {
			return true
		}
		if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
			t.Fatalf("unexpected live quota outcome: %+v", got)
		}
		return false
	}
	lower, upper := uint64(1), uint64(1<<20)
	if !admitted(upper) {
		t.Fatal("resource fixture exceeds search bound")
	}
	for lower < upper {
		middle := lower + (upper-lower)/2
		if admitted(middle) {
			upper = middle
		} else {
			lower = middle + 1
		}
	}
	if admitted(lower-1) || !admitted(lower) {
		t.Fatal("inconsistent live quota boundary")
	}
	return lower
}

func TestF030TemporaryResourceAdmission(t *testing.T) {
	pattern, text := strings.Repeat("?", 128), strings.Repeat("x", 8192)
	for _, fixture := range []struct{ name, control, subject string }{
		{"glob-tokens", fmt.Sprintf(`return String.length(%q)`, pattern), fmt.Sprintf(`return Glob.new(%q)`, pattern)},
		{"glob-match-scratch",
			fmt.Sprintf(`flow Main() -> Bool! { mask=Glob.new(%q); text=%q; unused=String.length(text); return true }; return Main()`, pattern, text),
			fmt.Sprintf(`flow Main() -> Bool! { mask=Glob.new(%q); text=%q; unused=String.length(text); return Glob.matches(mask,text) }; return Main()`, pattern, text)},
		{"sort-scratch",
			`flow Step(state:Int)->Record{value:Int,next_state:Int}{return Record.new(value:state,next_state:Math.sub(state,1))}; flow Main()->Int{values=List.unfold(64,64,Step);return List.length(values)};return Main()`,
			`flow Step(state:Int)->Record{value:Int,next_state:Int}{return Record.new(value:state,next_state:Math.sub(state,1))};flow Compare(left:Int,right:Int)->Int{return Math.sub(left,right)};flow Main()->Int{values=List.unfold(64,64,Step);return List.length(List.sort(values,Compare))};return Main()`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			control := f030MinimumLive(t, fixture.control)
			subject := f030MinimumLive(t, fixture.subject)
			if subject <= control {
				t.Fatalf("temporary or compiled storage is uncharged: control=%d subject=%d", control, subject)
			}
			t.Logf("minimum live bytes: control=%d operation=%d", control, subject)
		})
	}
}

func TestF030ComparatorTemporaryRelease(t *testing.T) {
	source := fmt.Sprintf(`flow Failed(error:Error)->Int{return 0};flow Compare(left:Int,right:Int)->Int onError Failed{temporary=String.replace_all(%q,"a",%q);length=String.length(temporary);return Math.sub(left,right)};flow Step(state:Int)->Record{value:Int,next_state:Int}{return Record.new(value:state,next_state:Math.sub(state,1))};flow Main()->Int{values=List.unfold(128,128,Step);return List.length(List.sort(values,Compare))};return Main()`, strings.Repeat("a", 128), strings.Repeat("b", 32))
	got := runProgram(t, sourceFile(t, source), "run", "--max-steps", "10000000", "--max-live-bytes", "32768")
	if got.status != 0 || got.stdout != "128\n" || got.stderr != "" {
		t.Fatalf("comparison temporaries accumulated across calls: %+v", got)
	}
}

func TestF030SplitItemQuota(t *testing.T) {
	for _, fixture := range []struct{ name, source string }{
		{"text", `return String.split("a:b:c", ":")`},
		{"bytes", `return Bytes.split(Bytes.new("a\u0000b\u0000c"), Bytes.new("\u0000"))`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			artifact, built := buildForTest(t, sourceFile(t, fixture.source))
			if built.status != 0 {
				t.Fatalf("build split quota: %+v", built)
			}
			got := runExecutable(t, artifact, "run", "--max-list-items", "2")
			if got.status != 2 || got.stdout != "" || !strings.Contains(got.stderr, "runtime.limit") {
				t.Fatalf("split must enforce item quota: %+v", got)
			}
			got = runExecutable(t, artifact, "run", "--max-list-items", "3")
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("split exact item quota: %+v", got)
			}
		})
	}
}
