package integration_test

import "testing"

func TestListFilterReleasesPredicateResults(t *testing.T) {
	text := `flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; flow Reject(item: Int) -> Bool { return Math.lt(item, 0) }; return List.length(List.filter(List.unfold(0, 1024, Next), Reject))`
	valueJSON(t, runValue(t, text, "--max-live-bytes", "46080"), `"0"`)
}
