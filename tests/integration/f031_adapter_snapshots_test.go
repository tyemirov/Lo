package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestF031GuardAdapterSnapshots(t *testing.T) {
	executable := repository + "/testdata/inert-process/executable"
	directory := repository + "/testdata/inert-process/directory"
	for _, test := range []struct {
		name, typ, setup, want string
	}{
		{
			name:  "headers",
			typ:   "HTTP.Headers",
			setup: `context=HTTP.Headers.new(List.new(Record.new(name:"authorization",value:Bytes.new("fixture-private-header"))))`,
			want:  `{"entries":{"type":"List<Record{name: String, value: Bytes}>","value":[{"type":"Record{name: String, value: Bytes}","value":{"name":{"type":"String","value":"authorization"},"value":{"type":"Bytes","value":""}}}]}}`,
		},
		{
			name:  "url",
			typ:   "HTTP.URL",
			setup: `context=HTTP.URL.new("https://example.invalid/private?token=fixture-private-query")`,
			want:  `"https://example.invalid:443/"`,
		},
		{
			name:  "command",
			typ:   "Process.Command",
			setup: fmt.Sprintf(`executable=Process.Executable.new(File.Path.new(%q));initial=Process.Command.new(executable:executable,arguments:List.new("fixture-private-argument"),context:Process.Context.new(Map.new(Record.new(key:"FIXTURE",value:"fixture-private-environment")),File.Path.new(%q)),stdin:Bytes.new("fixture-private-stdin"));context=Process.Command.with_timeout(initial,1234)`, executable, directory),
			want:  fmt.Sprintf(`{"context":{"type":"Process.Context","value":{"origin":"explicit","redacted":true}},"executable":{"type":"Process.Executable","value":{"path":{"type":"File.Path","value":%q}}},"arguments":{"type":"List<String>","value":[]},"stdin":{"type":"Bytes","value":""},"timeout_ms":{"type":"Int","value":"1234"},"output":{"type":"String","value":"capture"}}`, executable),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := fmt.Sprintf(`flow Eligible(context:%s,original:File.Entry,current:File.Entry)->Bool{return true};flow Main()->File.Remove!{%s;return File.Remove.checked(List.new(),context,Eligible)};return Main()`, test.typ, test.setup)
			value := successfulValue(t, runValue(t, source))
			var target struct {
				Guard struct {
					Context []runtimeValue `json:"context"`
				} `json:"guard"`
			}
			if err := json.Unmarshal(value.Value, &target); err != nil {
				t.Fatal(err)
			}
			if value.Type != "File.Remove" || len(target.Guard.Context) != 1 || target.Guard.Context[0].Type != test.typ {
				t.Fatalf("guard context missing from returned target: %s", value.Value)
			}
			valueJSON(t, target.Guard.Context[0], test.want)
		})
	}
}
