package value

// Visit walks each immutable value node once, including private retained children.
func Visit(values []Value, visit func(Value)) {
	VisitWhile(values, func(current Value) bool {
		visit(current)
		return true
	})
}

// VisitWhile walks each immutable node once and skips its children when visit returns false.
func VisitWhile(values []Value, visit func(Value) bool) {
	seen := map[uint64]bool{}
	var local [8]Value
	pending := append(local[:0], values...)
	for len(pending) != 0 {
		current := pending[len(pending)-1]
		pending[len(pending)-1] = nil
		pending = pending[:len(pending)-1]
		if current == nil {
			continue
		}
		if identity := Identity(current); identity != 0 {
			if seen[identity] {
				continue
			}
			seen[identity] = true
		}
		if !visit(current) {
			continue
		}
		switch item := current.(type) {
		case list:
			pending = append(pending, item.items...)
		case record:
			for _, field := range item.fields {
				pending = append(pending, field.Value)
			}
		case mapValue:
			for _, entry := range item.entries {
				pending = append(pending, entry.Key, entry.Value)
			}
		case success:
			pending = append(pending, item.result)
		case failure:
			pending = append(pending, item.cause)
		case loError:
			pending = append(pending, item.input)
			for _, trace := range item.trace {
				pending = append(pending, trace)
			}
			for _, progress := range item.flowProgress {
				pending = append(pending, progress)
			}
		case fileEntry:
			pending = append(pending, item.fields)
		case publicFileEntry:
			pending = append(pending, item.fields)
		case fileDirectoryObservation:
			pending = append(pending, item.fields)
		case publicFileValue:
			pending = append(pending, item.fields)
		case fileScan:
			pending = append(pending, item.fields)
		case removalEffect:
			pending = append(pending, item.entries)
			if item.guard != nil {
				pending = append(pending, item.guard.(removalGuard).context...)
			}
		case OptimizerState:
			pending = append(pending, item.Contents(), item.Parameters(), item.Config(), item.Step())
		case OptimizerConfig:
			pending = append(pending, item.Contents())
		case TensorBundle:
			pending = append(pending, item.Contents())
		case processExecutable:
			pending = append(pending, item.path)
		case processCommand:
			pending = append(pending, item.context, item.executable, item.arguments, item.stdin)
		case processContext:
			pending = append(pending, item.environment, item.directory)
		case processResult:
			pending = append(pending, item.termination, item.stdout, item.stderr)
		case processTermination:
			pending = append(pending, item.kind, item.payload)
		case hostInvocation:
			pending = append(pending, item.fields)
		case receipt:
			pending = append(pending, item.fields)
		case httpValue:
			pending = append(pending, item.fields)
		case clientRequest:
			pending = append(pending, item.method, item.url, item.headers, item.body)
		case headers:
			pending = append(pending, item.entries)
		case effect:
			pending = append(pending, item.path, item.content)
		case server:
			pending = append(pending, item.port)
		case rangeValue:
			pending = append(pending, item.start, item.end)
		}
	}
}
