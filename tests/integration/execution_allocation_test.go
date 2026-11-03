package integration_test

const allocationSequence = `flow Next(state: Int) -> Record{value: Int, next_state: Int} { return Record.new(value: state, next_state: Math.add(state, 1)) }; `
