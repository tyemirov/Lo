# Autodiff, Training, And Checkpoints

[Documentation home](../README.md) · [Operation index](README.md) · [Tensor operations](tensors.md)

All training values are immutable.
A successful update returns new Parameters or state. Earlier values do not change.
The examples use CPU data and need no device grant.

In signatures, `R` denotes a nonempty exact record of `Tensor<FP32>` fields.
`T` denotes `Parameters<R>`, `Optimizer.SGD.State<R>`, or `Optimizer.AdamW.State<R>` for checkpoint operations.
These symbols are reference notation, not Lo type aliases.
Every source example uses a full structural type.

All operations obey the [tensor resource contract](../../specification.md#244-resource-and-diagnostic-contracts).
Host cancellation and resource exhaustion bypass Lo error handlers.
No operation publishes a partial gradient, optimizer state, or checkpoint.
Device computation and transfers also obey the [device contract](../../specification.md#248-device-resources-and-explicit-transfers).


## Parameters.new

```text
Parameters.new(values: R) -> Parameters<R>!
```

Construct Parameters from an exact nonempty tensor record.
All fields must have one shared device location.
Field names identify separate Parameters, even when tensors share storage.

**Failures:** Evaluation can produce `Gradient.SchemaMismatch`.

**Authority:** Pure value operation. No host observation, file access, or commit.

**Example:**

```lo
flow Main() -> Parameters<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    return Parameters.new(Record.new(x: value))
}
return Main()
```

[Contract](../../specification.md#246-parameters-and-gradient-lifecycle).


## Parameters.values

```text
Parameters.values(parameters: Parameters<R>) -> R
```

Return the exact record of Parameter tensors.
This operation does not transfer, download, or change the tensors.

**Failures:** No operation-specific Lo error.

**Authority:** Pure value operation. No host observation, file access, or commit.

**Example:**

```lo
flow Main() -> Record{x: Tensor<FP32>}! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    return Parameters.values(parameters)
}
return Main()
```

[Contract](../../specification.md#246-parameters-and-gradient-lifecycle).


## Gradients.new

```text
Gradients.new(parameters: Parameters<R>, values: R) -> Gradients<R>!
```

Construct gradients with exactly the Parameter keys, tensor shapes, and device location.
Schema validation occurs before any dependent numeric work.

**Failures:** Evaluation can produce `Gradient.SchemaMismatch`.

**Authority:** Pure value operation. No host observation, file access, or commit.

**Example:**

```lo
flow Main() -> Gradients<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    return Gradients.new(parameters, Record.new(x: value))
}
return Main()
```

[Contract](../../specification.md#246-parameters-and-gradient-lifecycle).


## Gradients.values

```text
Gradients.values(gradients: Gradients<R>) -> R
```

Return the exact record of gradient tensors.
This operation does not transfer, download, or change the tensors.

**Failures:** No operation-specific Lo error.

**Authority:** Pure value operation. No host observation, file access, or commit.

**Example:**

```lo
flow Main() -> Record{x: Tensor<FP32>}! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    gradients = Gradients.new(parameters, Record.new(x: value))
    return Gradients.values(gradients)
}
return Main()
```

[Contract](../../specification.md#246-parameters-and-gradient-lifecycle).


## Gradients.zero

```text
Gradients.zero(parameters: Parameters<R>) -> Gradients<R>!
```

Construct positive-zero gradients with the Parameter keys, shapes, and device location.
This operation does not reset an earlier gradient value.

**Failures:** Evaluation can produce `Gradient.SchemaMismatch`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Gradients<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    return Gradients.zero(parameters)
}
return Main()
```

[Contract](../../specification.md#246-parameters-and-gradient-lifecycle).


## Gradients.add

```text
Gradients.add(left: Gradients<R>, right: Gradients<R>) -> Gradients<R>!
```

Add corresponding gradient fields in FP32.
Both records must have the same keys, shapes, and device location.
Schema validation precedes arithmetic.

**Failures:** Evaluation can produce `Gradient.SchemaMismatch`, `Numeric.NonFinite`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Gradients<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    gradients = Gradients.new(parameters, Record.new(x: value))
    return Gradients.add(gradients, gradients)
}
return Main()
```

[Contract](../../specification.md#246-parameters-and-gradient-lifecycle).


## Gradients.scale

```text
Gradients.scale(gradients: Gradients<R>, factor: Float) -> Gradients<R>!
```

Convert the factor to FP32 once, then multiply each gradient element.
Every result must remain finite.
The result keeps the gradient schema and device location.

**Failures:** Evaluation can produce `Gradient.SchemaMismatch`, `Numeric.NonFinite`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Gradients<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    gradients = Gradients.new(parameters, Record.new(x: value))
    return Gradients.scale(gradients, 0.5)
}
return Main()
```

[Contract](../../specification.md#246-parameters-and-gradient-lifecycle).


## Autodiff.stop

```text
Autodiff.stop(value: Tensor<FP32>) -> Tensor<FP32>
```

Keep a tensor value and remove its derivative path.
The value keeps its shape, data, and device location.
An outer gradient treats this path as constant.

**Failures:** No operation-specific Lo error.

**Authority:** Pure value operation. No host observation, file access, or commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    value = Tensor.new(List.new(2.0), List.new())
    return Autodiff.stop(value)
}
return Main()
```

[Contract](../../specification.md#246-parameters-and-gradient-lifecycle).


## Autodiff.grad

```text
Autodiff.grad(targets: R, context..., Loss) -> Record{value: Tensor<FP32>, gradients: R}!
```

Evaluate a named loss flow, then calculate first-order reverse-mode gradients.
`Loss` accepts `(context..., R)` and returns a rank-zero `Tensor<FP32>`, with optional fallibility.
Arguments are positional. Context values are constants for differentiation.
Disconnected fields receive positive-zero gradients with their original shapes.

The loss accepts only the documented differentiable subset.
Boundary reads, commits, tensor construction, transfers, nested gradients, and callbacks are outside that subset.

**Failures:** Evaluation can produce `Autodiff.InvalidLoss`, `Autodiff.UndefinedDerivative`, `Numeric.NonFinite`, `Numeric.DivideByZero`, `Numeric.Domain`, `Tensor.ShapeMismatch`, `Tensor.InvalidShape`, `Tensor.EmptyReduction`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Loss(parameters: Record{x: Tensor<FP32>}) -> Tensor<FP32>! {
    return Tensor.mul(parameters.x, parameters.x)
}
flow Main() -> Record{value: Tensor<FP32>, gradients: Record{x: Tensor<FP32>}}! {
    value = Tensor.new(List.new(2.0), List.new())
    return Autodiff.grad(Record.new(x: value), Loss)
}
return Main()
```

[Contract](../../specification.md#245-reverse-mode-autodiff-and-backpropagation).


## Optimizer.SGD.Config.new

```text
Optimizer.SGD.Config.new(learning_rate: Float) -> Optimizer.SGD.Config!
```

Construct SGD configuration after one FP32 conversion of the learning rate.
The converted learning rate must be positive.
This optimizer has no momentum or weight decay.

**Failures:** Evaluation can produce `Optimizer.InvalidConfig`.

**Authority:** Pure value operation. No host observation, file access, or commit.

**Example:**

```lo
return Optimizer.SGD.Config.new(0.25)
```

[Contract](../../specification.md#247-sgd-and-adamw).


## Optimizer.SGD.State.new

```text
Optimizer.SGD.State.new(parameters: Parameters<R>, config: Optimizer.SGD.Config) -> Optimizer.SGD.State<R>!
```

Construct immutable SGD state at step zero.
The state retains Parameters and its validated configuration.
Source projection exposes `parameters` and `step`.

**Failures:** Evaluation can produce `Gradient.SchemaMismatch`, `Numeric.NonFinite`, `Numeric.DivideByZero`, `Numeric.Domain`, `Device.UnsupportedOperation`.

**Authority:** Pure value operation. No host observation, file access, or commit.

**Example:**

```lo
flow Main() -> Optimizer.SGD.State<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    config = Optimizer.SGD.Config.new(0.25)
    return Optimizer.SGD.State.new(parameters, config)
}
return Main()
```

[Contract](../../specification.md#247-sgd-and-adamw).


## Optimizer.SGD.step

```text
Optimizer.SGD.step(state: Optimizer.SGD.State<R>, gradients: Gradients<R>) -> Optimizer.SGD.State<R>!
```

Return the next SGD state with `p_next = p - learning_rate * gradient`.
Multiplication and subtraction use separate FP32 rounding.
The successful result increases the step count by one.
All gradient fields must agree with the Parameter schema before arithmetic starts.

**Failures:** Evaluation can produce `Gradient.SchemaMismatch`, `Numeric.NonFinite`, `Numeric.DivideByZero`, `Numeric.Domain`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Optimizer.SGD.State<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    config = Optimizer.SGD.Config.new(0.25)
    state = Optimizer.SGD.State.new(parameters, config)
    gradients = Gradients.new(parameters, Record.new(x: value))
    return Optimizer.SGD.step(state, gradients)
}
return Main()
```

[Contract](../../specification.md#247-sgd-and-adamw).


## Optimizer.SGD.State.to

```text
Optimizer.SGD.State.to(state: Optimizer.SGD.State<R>, device: Device) -> Optimizer.SGD.State<R>!
```

Transfer all state tensors to the selected device.
Parameters, configuration, and step count retain their values.
AdamW transfer also keeps moments and beta powers.
A failed transfer publishes no partial state.

**Failures:** Evaluation can produce `Tensor.DeviceMismatch`, `Device.Unavailable`, `Device.AllocationFailed`, `Device.TransferFailed`, `Device.UnsupportedProfile`, `Device.LibraryFailed`, `Device.Lost`.

**Authority:** Each non-CPU location needs its own device grant. CPU-to-CPU transfer needs no grant. No commit.

**Example:**

```lo
flow Main() -> Optimizer.SGD.State<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    config = Optimizer.SGD.Config.new(0.25)
    state = Optimizer.SGD.State.new(parameters, config)
    gradients = Gradients.new(parameters, Record.new(x: value))
    cpu = Device.new("cpu")
    return Optimizer.SGD.State.to(state, cpu)
}
return Main()
```

[Contract](../../specification.md#249-model-checkpoints-and-training-state-transfers).


## Optimizer.AdamW.Config.new

```text
Optimizer.AdamW.Config.new(learning_rate: Float, beta1: Float, beta2: Float, epsilon: Float, weight_decay: Float) -> Optimizer.AdamW.Config!
```

Construct AdamW configuration after one FP32 conversion of each field.
The learning rate and epsilon must be positive. Betas must be in `[0,1)`.
Weight decay must be nonnegative. Its product with the learning rate must remain finite.
Underflow of a required positive value and a beta rounded to one are invalid.

**Failures:** Evaluation can produce `Optimizer.InvalidConfig`.

**Authority:** Pure value operation. No host observation, file access, or commit.

**Example:**

```lo
return Optimizer.AdamW.Config.new(0.01, 0.9, 0.999, 0.00000001, 0.01)
```

[Contract](../../specification.md#247-sgd-and-adamw).


## Optimizer.AdamW.State.new

```text
Optimizer.AdamW.State.new(parameters: Parameters<R>, config: Optimizer.AdamW.Config) -> Optimizer.AdamW.State<R>!
```

Construct AdamW state at step zero, with positive-zero moments and unit beta powers.
All state tensors use the Parameter device.
Source projection exposes `parameters` and `step`. Checkpoint encoding retains the full state.

**Failures:** Evaluation can produce `Gradient.SchemaMismatch`, `Numeric.NonFinite`, `Numeric.DivideByZero`, `Numeric.Domain`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Optimizer.AdamW.State<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    config = Optimizer.AdamW.Config.new(0.01, 0.9, 0.999, 0.00000001, 0.01)
    return Optimizer.AdamW.State.new(parameters, config)
}
return Main()
```

[Contract](../../specification.md#247-sgd-and-adamw).


## Optimizer.AdamW.step

```text
Optimizer.AdamW.step(state: Optimizer.AdamW.State<R>, gradients: Gradients<R>) -> Optimizer.AdamW.State<R>!
```

Return the next AdamW state after bias-corrected moment updates and separate weight decay.
Epsilon is outside the square root. Weight decay applies to every Parameter field.
Each arithmetic operation uses FP32 rounding in the specified order.
All gradient fields must agree with the Parameter schema before arithmetic starts.
The successful result increases the step count by one.

**Failures:** Evaluation can produce `Gradient.SchemaMismatch`, `Numeric.NonFinite`, `Numeric.DivideByZero`, `Numeric.Domain`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Optimizer.AdamW.State<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    config = Optimizer.AdamW.Config.new(0.01, 0.9, 0.999, 0.00000001, 0.01)
    state = Optimizer.AdamW.State.new(parameters, config)
    gradients = Gradients.new(parameters, Record.new(x: value))
    return Optimizer.AdamW.step(state, gradients)
}
return Main()
```

[Contract](../../specification.md#247-sgd-and-adamw).


## Optimizer.AdamW.State.to

```text
Optimizer.AdamW.State.to(state: Optimizer.AdamW.State<R>, device: Device) -> Optimizer.AdamW.State<R>!
```

Transfer all state tensors to the selected device.
Parameters, configuration, and step count retain their values.
AdamW transfer also keeps moments and beta powers.
A failed transfer publishes no partial state.

**Failures:** Evaluation can produce `Tensor.DeviceMismatch`, `Device.Unavailable`, `Device.AllocationFailed`, `Device.TransferFailed`, `Device.UnsupportedProfile`, `Device.LibraryFailed`, `Device.Lost`.

**Authority:** Each non-CPU location needs its own device grant. CPU-to-CPU transfer needs no grant. No commit.

**Example:**

```lo
flow Main() -> Optimizer.AdamW.State<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    config = Optimizer.AdamW.Config.new(0.01, 0.9, 0.999, 0.00000001, 0.01)
    state = Optimizer.AdamW.State.new(parameters, config)
    gradients = Gradients.new(parameters, Record.new(x: value))
    cpu = Device.new("cpu")
    return Optimizer.AdamW.State.to(state, cpu)
}
return Main()
```

[Contract](../../specification.md#249-model-checkpoints-and-training-state-transfers).


## Parameters.to

```text
Parameters.to(parameters: Parameters<R>, device: Device) -> Parameters<R>!
```

Transfer every Parameter tensor to the selected device with its shape and FP32 bits unchanged.
Field names and the Parameter schema do not change.
A failed transfer publishes no partial Parameters value.

**Failures:** Evaluation can produce `Tensor.DeviceMismatch`, `Device.Unavailable`, `Device.AllocationFailed`, `Device.TransferFailed`, `Device.UnsupportedProfile`, `Device.LibraryFailed`, `Device.Lost`.

**Authority:** Each non-CPU location needs its own device grant. CPU-to-CPU transfer needs no grant. No commit.

**Example:**

```lo
flow Main() -> Parameters<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    cpu = Device.new("cpu")
    return Parameters.to(parameters, cpu)
}
return Main()
```

[Contract](../../specification.md#249-model-checkpoints-and-training-state-transfers).


## Checkpoint.encode

```text
Checkpoint.encode(value: T) -> Bytes!
```

Encode CPU Parameters or full optimizer state as canonical typed JSON bytes.
All nested tensors must be on CPU.
This operation does not write a file.
A separate committed File target can persist the returned bytes.

**Failures:** Evaluation can produce `Tensor.HostObservationRequired`.

**Authority:** Pure value operation. No host observation, file access, or commit.

**Example:**

```lo
flow Main() -> Bytes! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    return Checkpoint.encode(parameters)
}
return Main()
```

[Contract](../../specification.md#249-model-checkpoints-and-training-state-transfers).


## Checkpoint.decode

```text
Checkpoint.decode(data: Bytes, template: T) -> T!
```

Decode canonical checkpoint bytes into the same container type as the CPU template.
The template supplies types, field names, and shapes.
The checkpoint supplies numeric values, configuration, and optimizer history.
The result contains CPU tensors. Duplicate keys, unknown fields, trailing data, and noncanonical numbers are invalid.

**Failures:** Evaluation can produce `Checkpoint.InvalidData`, `Checkpoint.SchemaMismatch`, `Tensor.HostObservationRequired`, `Device.AllocationFailed`.

**Authority:** Pure value operation. No host observation, file access, or commit.

**Example:**

```lo
flow Main() -> Parameters<Record{x: Tensor<FP32>}>! {
    value = Tensor.new(List.new(2.0), List.new())
    parameters = Parameters.new(Record.new(x: value))
    saved = Checkpoint.encode(parameters)
    return Checkpoint.decode(saved, parameters)
}
return Main()
```

[Contract](../../specification.md#249-model-checkpoints-and-training-state-transfers).
