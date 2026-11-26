package integration_test

import "testing"

const consoleSpawnSource = `
flow Selected(selection: Int, item: Int) -> Bool { return Math.eq(selection, item) }
flow PrintLine(previous: Int, item: Int) -> Int! {
    line = Console.Line.new("stdout", "console", true)
    commit line
    return 0
}
flow Probe(previous: Int, item: Int) -> Int! {
    paths = File.open_paths(File.Path.new("."))
    return List.length(paths)
}
flow Main() -> Program.Exit! {
    invocation = Host.Invocation.current()
    choice = List.length(invocation.arguments) |> Match.apply(Match.new(List.new(Match.Case.new(MainChoiceChoiceTest1, MainChoiceChoiceResult1)), MainChoiceChoiceDefault))
    first = Flow.fold(List.filter(List.new(choice), 1, Selected), 0, PrintLine)
    second = Flow.fold(List.filter(List.new(choice), 2, Selected), first, Probe)
    flow MainChoiceChoiceTest1(value: Int) -> Bool {
        return Math.eq(value, 0)
    }

    flow MainChoiceChoiceResult1(value: Int) -> Int {
        return 1
    }

    flow MainChoiceChoiceDefault(value: Int) -> Int {
        return 2
    }

    return Program.Exit.new(0, Bytes.new(""), Bytes.new(""))
}
return Main()`

func TestLLVMConsoleSpawnInheritance(t *testing.T) {
	runLLVMRuntimeTargetScenarios(t, semanticNativeTarget(), []llvmRuntimeScenario{{
		name: "liveness-endpoint-excluded-from-concurrent-probe", source: consoleSpawnSource,
		harness: "testdata/native-runtime/console-spawn-inheritance.c",
	}})
}

func TestLLVMConsoleSpawnQueuedDeadline(t *testing.T) {
	if semanticNativeTarget() != "darwin/arm64" {
		t.Skip("the selected native Process backend is Darwin")
	}
	runLLVMRuntimeTargetScenarios(t, "darwin/arm64", []llvmRuntimeScenario{{
		name: "process-deadline-while-console-owns-creation", source: consoleSpawnSource,
		harness: "testdata/native-runtime/console-spawn-deadline.c",
	}})
}
