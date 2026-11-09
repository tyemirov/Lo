package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/MarcoPoloResearchLab/Lo/internal/catalog"
)

type builtinExample struct{ name, source, kind string }

func builtinExamples() []builtinExample {
	executablePath := repository + "/testdata/inert-process/executable"
	directoryPath := repository + "/testdata/inert-process/directory"
	examples := []builtinExample{
		{catalog.ProgramPath, `flow Main() -> Bool! { path = Program.path(); return File.Path.eq(path, path) }; return Main()`, "Bool!"},
		{catalog.LockFileNew, `return Lock.File.new(File.Path.new("/ungranted-private-lock"))`, "Lock.File"},
		{catalog.ConsoleLineNew, `return Console.Line.new("stdout", "catalog", true)`, "Console.Line!"},
		{catalog.ProgramExitNew, `flow Main() -> Record{terminal: Program.Exit}! { terminal = Program.Exit.new(2, Bytes.new("out"), Bytes.new("err")); return Record.new(terminal: terminal) }; return Main()`, "Record{terminal: Program.Exit}!"},
		{catalog.CLIParse, `return CLI.parse(List.new("--verbose"), List.new(Record.new(name: "--verbose", aliases: List.new("-v"), group: "", help: "Show detail")), "catalog", "", true)`, "CLI.Options!"},
		{catalog.ClockNowUnixNS, `flow Main() -> Bool! { now = Clock.now_unix_ns(); return Math.eq(now, now) }; return Main()`, "Bool!"},
		{catalog.FileSystemUsage, `flow Main() -> Bool! { usage = File.System.usage(File.Path.new("unobserved")); return Math.gte(usage.total_bytes, usage.free_bytes) }; return Main()`, "Bool!"},
		{"Host.Invocation.current", `flow Main() -> Int! { initial = Host.Invocation.current(); return List.length(initial.arguments) }; return Main()`, "Int!"},
		{"Process.Context.current", `return Process.Context.current()`, "Process.Context!"},
		{"Process.Context.new", fmt.Sprintf(`return Process.Context.new(Map.new(), File.Path.new(%q))`, directoryPath), "Process.Context!"},
		{"Process.Context.override_environment", fmt.Sprintf(`flow Main() -> Process.Context! { initial = Process.Context.new(Map.new(), File.Path.new(%q)); return Process.Context.override_environment(initial, Map.new(Record.new(key: "KEY", value: "value"))) }; return Main()`, directoryPath), "Process.Context!"},
		{"Process.Context.without_environment", fmt.Sprintf(`flow Main() -> Process.Context! { initial = Process.Context.new(Map.new(), File.Path.new(%q)); return Process.Context.without_environment(initial, List.new("KEY")) }; return Main()`, directoryPath), "Process.Context!"},
		{"Process.Context.with_directory", fmt.Sprintf(`flow Main() -> Process.Context! { initial = Process.Context.new(Map.new(), File.Path.new(%q)); return Process.Context.with_directory(initial, File.Path.new(%q)) }; return Main()`, directoryPath, repository), "Process.Context!"},
		{"Process.Context.environment_value", fmt.Sprintf(`flow Main() -> String! { initial = Process.Context.new(Map.new(Record.new(key: "KEY", value: "value")), File.Path.new(%q)); return Process.Context.environment_value(initial, "KEY") }; return Main()`, directoryPath), "String!"},
		{"Process.Executable.resolve", fmt.Sprintf(`flow Main() -> Process.Executable! { initial = Process.Context.new(Map.new(), File.Path.new(%q)); return Process.Executable.resolve(initial, %q) }; return Main()`, directoryPath, binary), "Process.Executable!"},
		{"Autodiff.grad", `flow Loss(p: Record{x: Tensor<FP32>}) -> Tensor<FP32>! { return Tensor.mul(p.x,p.x) }; flow Example() -> Record{value: Tensor<FP32>, gradients: Record{x: Tensor<FP32>}}! { x=Tensor.new(List.new(2.0),List.new()); return Autodiff.grad(Record.new(x:x),Loss) }; return Example()`, "Record{gradients: Record{x: Tensor<FP32>}, value: Tensor<FP32>}!"},
		{"Int.new", `return Int.new(123456789012345678901234567890)`, "Int"},
		{"String.new", `return String.new("héllo")`, "String"},
		{"Bool.new", `return Bool.new(false)`, "Bool"},
		{"Float.new", `return Float.new(-1.25e3)`, "Float"},
		{"Float.add", `return Float.add(1.25, 2.5)`, "Float!"},
		{"Float.sub", `return Float.sub(1.25, 2.5)`, "Float!"},
		{"Float.mul", `return Float.mul(1.25, 2.0)`, "Float!"},
		{"Float.div", `return Float.div(3.0, 2.0)`, "Float!"},
		{"Float.sqrt", `return Float.sqrt(2.25)`, "Float!"},
		{"Float.eq", `return Float.eq(-0.0, 0.0)`, "Bool"},
		{"Float.lt", `return Float.lt(1.0, 2.0)`, "Bool"},
		{"Float.lte", `return Float.lte(1.0, 1.0)`, "Bool"},
		{"Float.gt", `return Float.gt(1.0, 2.0)`, "Bool"},
		{"Float.gte", `return Float.gte(1.0, 1.0)`, "Bool"},
		{"Int.to_float", `return Int.to_float(7)`, "Float!"},
		{"Float.to_int", `return Float.to_int(-1.9)`, "Int"},
		{"Float.to_string", `return Float.to_string(-0.0)`, "String"},
		{"Bytes.new", `return Bytes.new("bytes")`, "Bytes"},
		{"Scalar.new", `return Scalar.new("界")`, "Scalar"},
		{"List.new", `return List.new(null, null)`, "List<Null>"},
		{"Map.new", `return Map.new(Record.new(key: true, value: 1))`, "Map<Bool, Int>"},
		{"Record.new", `return Record.new(x: 1, y: "a")`, "Record{x: Int, y: String}"},
		{"Range.new", `return Range.new("a", "z", true, false)`, "Range<String>"},
		{"Error.new", `return Error.new("Custom", "message", Record.new())`, "Error"},
		{"File.Path.new", `return File.Path.new("unread")`, "File.Path"},
		{"File.Path.eq", `return File.Path.eq(File.Path.new("a"), File.Path.new("a"))`, "Bool"},
		{"Process.Executable.new", fmt.Sprintf(`return Process.Executable.new(File.Path.new(%q))`, executablePath), "Process.Executable!"},
		{"Process.Termination.exit_code", `flow Read(value: Process.Termination) -> Int! { return Process.Termination.exit_code(value) }; return 0`, "Int"},
		{"Process.Termination.signal", `flow Read(value: Process.Termination) -> Int! { return Process.Termination.signal(value) }; return 0`, "Int"},
		{"Process.Command.new", fmt.Sprintf(`flow Main() -> Process.Command! { executable = Process.Executable.new(File.Path.new(%q)); return Process.Command.new(executable: executable, arguments: List.new("argument"), context: Process.Context.new(Map.new(Record.new(key: "TOKEN",value: "value")), File.Path.new(%q)), stdin: Bytes.new("stdin")) }; return Main()`, executablePath, directoryPath), "Process.Command!"},
		{"Process.Command.with_timeout", fmt.Sprintf(`flow Main() -> Process.Command! { executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(executable: executable, arguments: List.new("argument"), context: Process.Context.new(Map.new(Record.new(key: "TOKEN", value: "value")), File.Path.new(%q)), stdin: Bytes.new("stdin")); return Process.Command.with_timeout(command, 30000) }; return Main()`, executablePath, directoryPath), "Process.Command!"},
		{"Process.Command.with_output", fmt.Sprintf(`flow Main() -> Process.Command! { executable = Process.Executable.new(File.Path.new(%q)); command = Process.Command.new(executable: executable, arguments: List.new("argument"), context: Process.Context.new(Map.new(Record.new(key: "TOKEN", value: "value")), File.Path.new(%q)), stdin: Bytes.new("stdin")); return Process.Command.with_output(command, "forward") }; return Main()`, executablePath, directoryPath), "Process.Command!"},
		{"HTTP.Server.new", `flow H(r: HTTP.Request) -> HTTP.Response { return HTTP.Response.Text.new(200, "ok") }; flow E(e: Error) -> HTTP.Response { return HTTP.Response.Text.new(500, "error") }; return HTTP.Server.new(8080, H, E)`, "HTTP.Server"},
		{"HTTP.URL.new", `return HTTP.URL.new("http://127.0.0.1/resource")`, "HTTP.URL!"},
		{"HTTP.Method.new", `return HTTP.Method.new("GET")`, "HTTP.Method!"},
		{"HTTP.Client.Request.new", `flow Main() -> HTTP.Client.Request! { method=HTTP.Method.new("GET"); url=HTTP.URL.new("http://127.0.0.1/resource"); headers=HTTP.Headers.new(List.new()); return HTTP.Client.Request.new(method,url,headers,Bytes.new("")) }; return Main()`, "HTTP.Client.Request!"},
		{"HTTP.Headers.new", `return HTTP.Headers.new(List.new())`, "HTTP.Headers!"},
		{"HTTP.Headers.values", `flow Main() -> List<Bytes>! { headers = HTTP.Headers.new(List.new()); return HTTP.Headers.values(headers, "absent") }; return Main()`, "List<Bytes>!"},
		{"HTTP.Response.new", `flow Main() -> HTTP.Response! { headers = HTTP.Headers.new(List.new()); return HTTP.Response.new(200, headers, Bytes.new("ok")) }; return Main()`, "HTTP.Response!"},
		{"HTTP.Response.Text.new", `return HTTP.Response.Text.new(200, "ok")`, "HTTP.Response"},
		{"HTTP.Response.Bytes.new", `return HTTP.Response.Bytes.new(200, Bytes.new("ok"))`, "HTTP.Response"},
		{"UTF8.Bytes.new", `return UTF8.Bytes.new("ok")`, "Bytes"},
		{"UTF8.String.new", `return UTF8.String.new(Bytes.new("ok"))`, "String!"},
		{"String.FromScalars.new", `return String.FromScalars.new(List.new(Scalar.new("a")))`, "String"},
		{"String.concat", `return String.concat("a", "b")`, "String"},
		{"String.upper", `return String.upper("a")`, "String"},
		{"String.lower", `return String.lower("A")`, "String"},
		{"String.length", `return String.length("界")`, "Int"},
		{"String.eq", `return String.eq("a", "b")`, "Bool"},
		{"Scalar.eq", `return Scalar.eq(Scalar.new("a"), Scalar.new("b"))`, "Bool"},
		{"Int.to_string", `return Int.to_string(1)`, "String"},
		{"Math.add", `return Math.add(1, 2)`, "Int"},
		{"Math.sub", `return Math.sub(1, 2)`, "Int"},
		{"Math.mul", `return Math.mul(1, 2)`, "Int"},
		{"Math.div", `return Math.div(1, 0)`, "Int!"},
		{"Math.mod", `return Math.mod(1, 0)`, "Int!"},
		{"Math.lt", `return Math.lt(1, 2)`, "Bool"},
		{"Math.lte", `return Math.lte(1, 2)`, "Bool"},
		{"Math.gt", `return Math.gt(1, 2)`, "Bool"},
		{"Math.gte", `return Math.gte(1, 2)`, "Bool"},
		{"Math.eq", `return Math.eq(1, 2)`, "Bool"},
		{"Math.sum", `return Math.sum(List.new(1, 2))`, "Int"},
		{"Math.product", `return Math.product(List.new())`, "Int"},
		{"Math.min", `return Math.min(List.new())`, "Int!"},
		{"Math.max", `return Math.max(List.new())`, "Int!"},
		{"Range.contains", `return Range.contains(Range.new(1, 4, true, true), 3)`, "Bool"},
		{"Range.is_empty", `return Range.is_empty(Range.new(1, 4, true, true))`, "Bool"},
		{"Map.has_key", `return Map.has_key(Map.new(Record.new(key: "k", value: 1)), "k")`, "Bool"},
		{"Map.get", `return Map.get(Map.new(Record.new(key: null, value: 1)), null)`, "Int!"},
		{"Record.merge", `return Record.merge(Record.new(a: 1), Record.new(a: "a"))`, "Record{a: String}"},
		{"List.map", `flow F(x: Int) -> String { return Int.to_string(x) }; return List.map(List.new(1), F)`, "List<String>"},
		{"List.filter", `flow F(x: Scalar) -> Bool { return true }; return List.filter("abc", F)`, "List<Scalar>"},
		{"List.fold", `flow F(acc: Int, x: Int) -> Int { return Math.add(acc, x) }; return List.fold(List.new(1), 0, F)`, "Int"},
		{"Flow.fold", `flow F(acc: Int, x: Int) -> Int { return Math.add(acc, x) }; return Flow.fold(List.new(1), 0, F)`, "Int"},
		{"Flow.worklist", `flow F(state: Int, item: Int) -> Record{state: Int, items: List<Int>, stop: Bool} { return Record.new(state: Math.add(state, item), items: List.new(), stop: false) }; return Flow.worklist(List.new(1), 0, F)`, "Int"},
		{"List.flat_map", `flow F(x: Int) -> List<Int>! { return List.new(x) }; return List.flat_map(List.new(1), F)`, "List<Int>!"},
		{"List.unfold", `flow F(x: Int) -> Record{value: String, next_state: Int}! { return Record.new(value: Int.to_string(x), next_state: Math.add(x, 1)) }; return List.unfold(0, 2, F)`, "List<String>!"},
		{"List.length", `return List.length("héllo")`, "Int"},
		{"File.read", `return File.read(File.Path.new("missing"))`, "Bytes!"},
		{"File.list", `return File.list(File.Path.new("fixtures"))`, "List<File.Entry>!"},
		{"File.directory", `flow Main() -> Bool! { directory = File.directory(File.Path.new("fixtures")); return File.Path.eq(directory.path, File.Path.new("fixtures")) }; return Main()`, "Bool!"},
		{"File.scan", `flow Main() -> Int! { scan = File.scan(File.Path.new("fixtures")); return Math.add(List.length(scan.entries), List.length(scan.vanished_names)) }; return Main()`, "Int!"},
		{"File.open_paths", `return File.open_paths(File.Path.new("fixtures"))`, "List<File.Path>!"},
		{"File.Remove.new", `flow Empty() -> List<File.Entry> { return List.new() }; return File.Remove.new(Empty())`, "File.Remove!"},
		{"File.Remove.checked", `flow Eligible(original: File.Entry, current: File.Entry) -> Bool { return true }; flow Main() -> Int! { pending = File.Remove.checked(List.new(), Eligible); receipt = commit pending; return receipt.completed_count }; return Main()`, "Int!"},
		{"File.Append.new", `return File.Append.new(File.Path.new("unwritten"), UTF8.Bytes.new("data"))`, "File.Append"},
		{"File.Write.new", `return File.Write.new(File.Path.new("unwritten"), Bytes.new("data"))`, "File.Write"},
	}
	examples = append(examples, parameterBuiltinExamples()...)
	examples = append(examples, optimizerBuiltinExamples()...)
	examples = append(examples, checkpointBuiltinExamples()...)
	examples = append(examples, processorBuiltinExamples()...)
	examples = append(examples, retainedFileBuiltinExamples("fixtures")...)
	examples = append(examples, treeBuiltinExamples("fixtures")...)
	examples = append(examples, jsonFormattingBuiltinExamples()...)
	return append(examples, tensorBuiltinExamples()...)
}

func jsonFormattingBuiltinExamples() []builtinExample {
	return []builtinExample{
		{catalog.JSONDecode, `return JSON.decode(Bytes.new("null"))`, "JSON.Value!"},
		{catalog.JSONGet, `flow Main()->JSON.Value!{value=JSON.decode(Bytes.new("{\"key\":true}"));return JSON.get(value,"key")};return Main()`, "JSON.Value!"},
		{catalog.JSONAt, `flow Main()->JSON.Value!{value=JSON.decode(Bytes.new("[null]"));return JSON.at(value,0)};return Main()`, "JSON.Value!"},
		{catalog.JSONString, `flow Main()->String!{value=JSON.decode(Bytes.new("\"text\""));return JSON.string(value)};return Main()`, "String!"},
		{catalog.JSONBool, `flow Main()->Bool!{value=JSON.decode(Bytes.new("true"));return JSON.bool(value)};return Main()`, "Bool!"},
		{catalog.JSONInt, `flow Main()->Int!{value=JSON.decode(Bytes.new("123"));return JSON.int(value)};return Main()`, "Int!"},
		{catalog.JSONFloat, `flow Main()->Float!{value=JSON.decode(Bytes.new("1.25"));return JSON.float(value)};return Main()`, "Float!"},
		{catalog.JSONKind, `flow Main()->String!{value=JSON.decode(Bytes.new("[]"));return JSON.kind(value)};return Main()`, "String!"},
		{catalog.JSONLength, `flow Main()->Int!{value=JSON.decode(Bytes.new("[null,true]"));return JSON.length(value)};return Main()`, "Int!"},
		{catalog.IntParse, `return Int.parse("+00123")`, "Int!"},
		{catalog.FloatFormatFixed, `return Float.format_fixed(1.125,2)`, "String!"},
		{catalog.StringFormat, `return String.format(fields:Record.new(count:-1234),template:"{count:>8,}")`, "String!"},
	}
}

func treeBuiltinExamples(root string) []builtinExample {
	return []builtinExample{
		{catalog.FileTreeSnapshot, f034Predicates + f034Program(root, "Int", "", `return List.length(tree.entries)`), "Int!"},
		{catalog.FileTreeRemoveChecked, f034Predicates + f034Program(root, "Int", "", `pending = File.Tree.Remove.checked(tree, Eligible); receipt = commit pending; return receipt.completed_count`), "Int!"},
	}
}

func retainedFileBuiltinExamples(root string) []builtinExample {
	main := func(output, body string) string {
		return fmt.Sprintf(`flow Main() -> %s! { directory = File.directory(File.Path.new(%q)); %s }; return Main()`, output, root, body)
	}
	return []builtinExample{
		{"File.Directory.scan", main("Int", `scan = File.Directory.scan(directory); return Math.add(List.length(scan.entries), List.length(scan.vanished_names))`), "Int!"},
		{"File.Directory.refresh", main("Bool", `fresh = File.Directory.refresh(directory); return Math.eq(directory.inode, fresh.inode)`), "Bool!"},
		{"File.Directory.child", main("Bool", `entry = File.Directory.entry(directory, "child"); child = File.Directory.child(directory, entry); return Math.gt(child.inode, 0)`), "Bool!"},
		{"File.Directory.entry", main("String", `entry = File.Directory.entry(directory, "entry"); return entry.name`), "String!"},
		{"File.Entry.observe", main("Int", `entry = File.Directory.entry(directory, "entry"); observed = File.Entry.observe(entry); return List.length(observed)`), "Int!"},
	}
}

func processorBuiltinExamples() []builtinExample {
	return []builtinExample{
		{catalog.StringStartsWith, `return String.starts_with("abc","a")`, "Bool"},
		{catalog.StringEndsWith, `return String.ends_with("abc","c")`, "Bool"},
		{catalog.StringSlice, `return String.slice("a界b",1,2)`, "String!"},
		{catalog.StringCompare, `return String.compare("a","b")`, "Int"},
		{catalog.StringSplit, `return String.split("a:b",":")`, "List<String>!"},
		{catalog.StringSplitFirst, `return String.split_first("a:b",":")`, "Record{after: String, before: String}!"},
		{catalog.StringReplaceAll, `return String.replace_all("aba","a","x")`, "String!"},
		{catalog.StringTrim, `return String.trim(" a ")`, "String"},
		{catalog.StringIsWhitespace, `return String.is_whitespace(" \t")`, "Bool"},
		{catalog.StringSplitWhitespace, `return String.split_whitespace("a b")`, "List<String>"},
		{catalog.StringLines, `return String.lines("a\nb")`, "List<String>"},
		{catalog.BytesLength, `return Bytes.length(Bytes.new("abc"))`, "Int"},
		{catalog.BytesStartsWith, `return Bytes.starts_with(Bytes.new("abc"),Bytes.new("a"))`, "Bool"},
		{catalog.BytesSlice, `return Bytes.slice(Bytes.new("abc"),1,2)`, "Bytes!"},
		{catalog.BytesSplit, `return Bytes.split(Bytes.new("a:b"),Bytes.new(":"))`, "List<Bytes>!"},
		{catalog.BytesTrimStart, `return Bytes.trim_start(Bytes.new("ababx"),Bytes.new("ab"))`, "Bytes!"},
		{catalog.ListAt, `return List.at(List.new(1,2),1)`, "Int!"},
		{catalog.ListSlice, `return List.slice(List.new(1,2),1,2)`, "List<Int>!"},
		{catalog.ListSort, `flow Compare(left:Int,right:Int)->Int{return Math.sub(left,right)};return List.sort(List.new(2,1),Compare)`, "List<Int>"},
		{catalog.FilePathToString, `return File.Path.to_string(File.Path.new("a/b"))`, "String"},
		{catalog.FilePathJoin, `return File.Path.join(File.Path.new("a"),"b")`, "File.Path!"},
		{catalog.FilePathParent, `return File.Path.parent(File.Path.new("a/b"))`, "File.Path"},
		{catalog.FilePathName, `return File.Path.name(File.Path.new("a/b"))`, "String"},
		{catalog.GlobNew, `return Glob.new("*.lo")`, "Glob!"},
		{catalog.GlobMatches, `flow Main()->Bool!{pattern=Glob.new("*.lo");return Glob.matches(pattern,"a.lo")};return Main()`, "Bool!"},
		{catalog.ProcessArgumentsFormatShell, `return Process.Arguments.format_shell(List.new("a b"))`, "String!"},
	}
}

func TestVerifierBuiltinContracts(t *testing.T) {
	covered := map[string]bool{}
	for _, tc := range builtinExamples() {
		covered[tc.name] = true
		t.Run(tc.name, func(t *testing.T) {
			got := run(t, "check", sourceFile(t, tc.source), "--format", "json")
			if got.status != 0 || got.stderr != "" {
				t.Fatalf("%+v", got)
			}
			var result struct {
				ResultType string `json:"result_type"`
			}
			if err := json.Unmarshal([]byte(got.stdout), &result); err != nil {
				t.Fatal(err)
			}
			if result.ResultType != tc.kind {
				t.Fatalf("got %s, want %s", result.ResultType, tc.kind)
			}
		})
	}
	for _, op := range catalog.All() {
		if !covered[op.Name()] {
			t.Errorf("no public signature example for %s", op.Name())
		}
	}
}

func tensorBuiltinExamples() []builtinExample {
	examples := []builtinExample{{"Device.new", `return Device.new("cpu")`, "Device!"}, {"Tensor.new", `return Tensor.new(List.new(4.0),List.new())`, "Tensor<FP32>!"}}
	for _, tc := range []struct{ name, expression, typ string }{
		{"Tensor.to", `Tensor.to(a,Device.new("cpu"))`, "Tensor<FP32>"},
		{"Tensor.shape", "Tensor.shape(a)", "List<Int>"},
		{"Tensor.rank", "Tensor.rank(a)", "Int"},
		{"Tensor.dtype", "Tensor.dtype(a)", "String"},
		{"Tensor.device", "Tensor.device(a)", "Device"},
		{"Tensor.to_float", "Tensor.to_float(a)", "Float"},
		{"Tensor.values", "Tensor.values(a)", "List<Float>"},
		{"Tensor.add", "Tensor.add(a,b)", "Tensor<FP32>"},
		{"Tensor.sub", "Tensor.sub(a,b)", "Tensor<FP32>"},
		{"Tensor.mul", "Tensor.mul(a,b)", "Tensor<FP32>"},
		{"Tensor.div", "Tensor.div(a,b)", "Tensor<FP32>"},
		{"Tensor.sqrt", "Tensor.sqrt(a)", "Tensor<FP32>"},
		{"Tensor.relu", "Tensor.relu(a)", "Tensor<FP32>"},
		{"Tensor.matmul", "Tensor.matmul(Tensor.reshape(a,List.new(1,1)),Tensor.reshape(b,List.new(1,1)))", "Tensor<FP32>"},
		{"Tensor.broadcast", "Tensor.broadcast(a,List.new(1))", "Tensor<FP32>"},
		{"Tensor.reshape", "Tensor.reshape(a,List.new(1))", "Tensor<FP32>"},
		{"Tensor.transpose", "Tensor.transpose(a,List.new())", "Tensor<FP32>"},
		{"Tensor.sum", "Tensor.sum(a,List.new())", "Tensor<FP32>"},
		{"Tensor.mean", "Tensor.mean(a,List.new())", "Tensor<FP32>"},
	} {
		examples = append(examples, builtinExample{tc.name, fmt.Sprintf(`flow Example() -> %s! { a=Tensor.new(List.new(4.0),List.new()); b=Tensor.new(List.new(2.0),List.new()); return %s }; return Example()`, tc.typ, tc.expression), tc.typ + "!"})
	}
	return examples
}

func parameterBuiltinExamples() []builtinExample {
	var examples []builtinExample
	for _, tc := range []struct{ name, expression, typ string }{
		{"Parameters.new", "Parameters.new(Record.new(x:x))", "Parameters<Record{x: Tensor<FP32>}>"},
		{"Parameters.values", "Parameters.values(p)", "Record{x: Tensor<FP32>}"},
		{"Gradients.new", "Gradients.new(p,Record.new(x:x))", "Gradients<Record{x: Tensor<FP32>}>"},
		{"Gradients.values", "Gradients.values(g)", "Record{x: Tensor<FP32>}"},
		{"Gradients.zero", "Gradients.zero(p)", "Gradients<Record{x: Tensor<FP32>}>"},
		{"Gradients.add", "Gradients.add(g,g)", "Gradients<Record{x: Tensor<FP32>}>"},
		{"Gradients.scale", "Gradients.scale(g,0.5)", "Gradients<Record{x: Tensor<FP32>}>"},
		{"Autodiff.stop", "Autodiff.stop(x)", "Tensor<FP32>"},
	} {
		examples = append(examples, builtinExample{tc.name, fmt.Sprintf(`flow Example() -> %s! { x=Tensor.new(List.new(2.0),List.new()); p=Parameters.new(Record.new(x:x)); g=Gradients.new(p,Record.new(x:x)); return %s }; return Example()`, tc.typ, tc.expression), tc.typ + "!"})
	}
	return examples
}

func optimizerBuiltinExamples() []builtinExample {
	var examples []builtinExample
	for _, optimizer := range []string{"SGD", "AdamW"} {
		config := "Optimizer.SGD.Config.new(0.25)"
		if optimizer == "AdamW" {
			config = "Optimizer.AdamW.Config.new(0.25,0.5,0.5,0.5,0.5)"
		}
		configType := "Optimizer." + optimizer + ".Config"
		stateType := "Optimizer." + optimizer + ".State<Record{x: Tensor<FP32>}>"
		examples = append(examples, builtinExample{configType + ".new", "return " + config, configType + "!"})
		for _, op := range []string{"State.new", "step"} {
			name := "Optimizer." + optimizer + "." + op
			expression := "Optimizer." + optimizer + ".State.new(p,config)"
			if op == "step" {
				expression = "Optimizer." + optimizer + ".step(state,g)"
			}
			source := fmt.Sprintf(`flow Example() -> %s! { x=Tensor.new(List.new(2.0),List.new()); p=Parameters.new(Record.new(x:x)); config=%s; state=Optimizer.%s.State.new(p,config); g=Gradients.new(p,Record.new(x:x)); return %s }; return Example()`, stateType, config, optimizer, expression)
			examples = append(examples, builtinExample{name, source, stateType + "!"})
		}
	}
	return examples
}
