# Tensors And Devices

[Documentation home](../README.md) · [Operation index](README.md) · [Training operations](training.md)

`Tensor<FP32>` is an immutable row-major array of finite FP32 values.
Shape and device location are metadata, not type arguments.
Each operation below has a signature and an example.
The examples use CPU tensors and need no device grant.

All tensor operations obey the [resource contract](../../specification.md#244-resource-and-diagnostic-contracts).
Host cancellation and quota exhaustion are terminal diagnostics, outside Lo error handlers.
A failed operation publishes no partial tensor.
Earlier values do not change.

For non-CPU arithmetic, an implemented backend and an explicit device grant are necessary.
The software backend implements `cuda-sim:0` transfers, but no arithmetic there.
An authorized unavailable backend produces `Device.Unavailable`.
The [backend table](../../specification.md#backend-support) records the implemented operations.


## Device.new

```text
Device.new(selector: String) -> Device!
```

Construct a device descriptor from `cpu`, `cuda:0`, `cuda-sim:0`, or `metal:0`.
Selector validation does not establish backend availability.

**Failures:** Evaluation can produce `Device.InvalidSelector`.

**Authority:** No host grant. This operation does not initialize a device or activate a commit.

**Example:**

```lo
return Device.new("cpu")
```

[Contract](../../specification.md#242-tensor-values-and-shape-boundaries).


## Tensor.new

```text
Tensor.new(values: List<Float>, shape: List<Int>) -> Tensor<FP32>!
```

Construct a CPU tensor from Float values and the requested shape.
Conversion to FP32 uses ties-to-even rounding.
A rank-zero tensor has one element. A zero dimension has zero elements.
The value count must equal the dimension product.
Dimensions must not be negative and must obey the resource limits.

**Failures:** Evaluation can produce `Tensor.InvalidShape`, `Numeric.NonFinite`.

**Authority:** Pure construction on CPU. No device grant or commit.

**Example:**

```lo
return Tensor.new(List.new(1.0, 2.0), List.new(2))
```

[Contract](../../specification.md#242-tensor-values-and-shape-boundaries).


## Tensor.shape

```text
Tensor.shape(value: Tensor<FP32>) -> List<Int>
```

Return the dimensions in order. A rank-zero tensor returns an empty list.

**Failures:** No operation-specific Lo error.

**Authority:** Pure metadata access. No host observation, device transfer, or commit.

**Example:**

```lo
flow Main() -> List<Int>! {
    value = Tensor.new(List.new(4.0), List.new())
    return Tensor.shape(value)
}
return Main()
```

[Contract](../../specification.md#242-tensor-values-and-shape-boundaries).


## Tensor.rank

```text
Tensor.rank(value: Tensor<FP32>) -> Int
```

Return the number of dimensions. A rank-zero tensor returns `0`.

**Failures:** No operation-specific Lo error.

**Authority:** Pure metadata access. No host observation, device transfer, or commit.

**Example:**

```lo
flow Main() -> Int! {
    value = Tensor.new(List.new(4.0), List.new())
    return Tensor.rank(value)
}
return Main()
```

[Contract](../../specification.md#242-tensor-values-and-shape-boundaries).


## Tensor.dtype

```text
Tensor.dtype(value: Tensor<FP32>) -> String
```

Return `"fp32"`. No other tensor dtype is implemented.

**Failures:** No operation-specific Lo error.

**Authority:** Pure metadata access. No host observation, device transfer, or commit.

**Example:**

```lo
flow Main() -> String! {
    value = Tensor.new(List.new(4.0), List.new())
    return Tensor.dtype(value)
}
return Main()
```

[Contract](../../specification.md#242-tensor-values-and-shape-boundaries).


## Tensor.device

```text
Tensor.device(value: Tensor<FP32>) -> Device
```

Return the actual device location. This operation does not transfer or download data.

**Failures:** No operation-specific Lo error.

**Authority:** Pure metadata access. No host observation, device transfer, or commit.

**Example:**

```lo
flow Main() -> Device! {
    value = Tensor.new(List.new(4.0), List.new())
    return Tensor.device(value)
}
return Main()
```

[Contract](../../specification.md#242-tensor-values-and-shape-boundaries).


## Tensor.to_float

```text
Tensor.to_float(value: Tensor<FP32>) -> Float!
```

Return the one CPU rank-zero element as binary64 without a change to its numeric value.
A one-element tensor with positive rank is not rank zero.

**Failures:** Evaluation can produce `Tensor.ShapeMismatch`, `Tensor.HostObservationRequired`.

**Authority:** Pure conversion of CPU data. Device input needs an explicit transfer to CPU first. No commit.

**Example:**

```lo
flow Main() -> Float! {
    value = Tensor.new(List.new(4.0), List.new())
    return Tensor.to_float(value)
}
return Main()
```

[Contract](../../specification.md#242-tensor-values-and-shape-boundaries).


## Tensor.values

```text
Tensor.values(value: Tensor<FP32>) -> List<Float>!
```

Return all CPU elements as binary64 values in row-major order, without a change to their numeric values.
The result is a new Lo list.

**Failures:** Evaluation can produce `Tensor.HostObservationRequired`.

**Authority:** Pure conversion of CPU data. Device input needs an explicit transfer to CPU first. No commit.

**Example:**

```lo
flow Main() -> List<Float>! {
    value = Tensor.new(List.new(4.0), List.new())
    return Tensor.values(value)
}
return Main()
```

[Contract](../../specification.md#242-tensor-values-and-shape-boundaries).


## Tensor.to

```text
Tensor.to(value: Tensor<FP32>, device: Device) -> Tensor<FP32>!
```

Transfer a tensor to the selected device with FP32 bits and shape unchanged.
A same-location transfer shares the initial immutable storage.
Authorization precedes device initialization and transfer.
Device tensors must return to CPU before numeric result publication.

**Failures:** Evaluation can produce `Tensor.DeviceMismatch`, `Device.Unavailable`, `Device.AllocationFailed`, `Device.TransferFailed`, `Device.UnsupportedProfile`, `Device.LibraryFailed`, `Device.Lost`.

**Authority:** Each non-CPU location needs its own device grant. CPU-to-CPU transfer needs no grant. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    value = Tensor.new(List.new(1.0, 2.0), List.new(2))
    cpu = Device.new("cpu")
    return Tensor.to(value, cpu)
}
return Main()
```

[Contract](../../specification.md#248-device-resources-and-explicit-transfers).


## Tensor.add

```text
Tensor.add(left: Tensor<FP32>, right: Tensor<FP32>) -> Tensor<FP32>!
```

Add corresponding elements with trailing-dimension broadcasting.
Aligned dimensions must be equal or have size one.
Each arithmetic operation uses FP32 rounding and must produce a finite result.

**Failures:** Evaluation can produce `Tensor.ShapeMismatch`, `Numeric.NonFinite`, `Tensor.DeviceMismatch`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    left = Tensor.new(List.new(4.0, 8.0), List.new(2))
    right = Tensor.new(List.new(2.0), List.new())
    return Tensor.add(left, right)
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.sub

```text
Tensor.sub(left: Tensor<FP32>, right: Tensor<FP32>) -> Tensor<FP32>!
```

Subtract right elements from left elements with trailing-dimension broadcasting.
Aligned dimensions must be equal or have size one.
Each arithmetic operation uses FP32 rounding and must produce a finite result.

**Failures:** Evaluation can produce `Tensor.ShapeMismatch`, `Numeric.NonFinite`, `Tensor.DeviceMismatch`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    left = Tensor.new(List.new(4.0, 8.0), List.new(2))
    right = Tensor.new(List.new(2.0), List.new())
    return Tensor.sub(left, right)
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.mul

```text
Tensor.mul(left: Tensor<FP32>, right: Tensor<FP32>) -> Tensor<FP32>!
```

Multiply corresponding elements with trailing-dimension broadcasting.
Aligned dimensions must be equal or have size one.
Each arithmetic operation uses FP32 rounding and must produce a finite result.

**Failures:** Evaluation can produce `Tensor.ShapeMismatch`, `Numeric.NonFinite`, `Tensor.DeviceMismatch`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    left = Tensor.new(List.new(4.0, 8.0), List.new(2))
    right = Tensor.new(List.new(2.0), List.new())
    return Tensor.mul(left, right)
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.div

```text
Tensor.div(left: Tensor<FP32>, right: Tensor<FP32>) -> Tensor<FP32>!
```

Divide left elements by right elements with trailing-dimension broadcasting.
A zero divisor produces `Numeric.DivideByZero`.
Aligned dimensions must be equal or have size one.
Each arithmetic operation uses FP32 rounding and must produce a finite result.

**Failures:** Evaluation can produce `Tensor.ShapeMismatch`, `Numeric.NonFinite`, `Tensor.DeviceMismatch`, `Device.UnsupportedOperation`, `Numeric.DivideByZero`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    left = Tensor.new(List.new(4.0, 8.0), List.new(2))
    right = Tensor.new(List.new(2.0), List.new())
    return Tensor.div(left, right)
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.sqrt

```text
Tensor.sqrt(value: Tensor<FP32>) -> Tensor<FP32>!
```

Return the elementwise square root, rounded to FP32.
Negative elements produce `Numeric.Domain`.
A zero input is valid for the forward operation.

**Failures:** Evaluation can produce `Numeric.Domain`, `Numeric.NonFinite`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    value = Tensor.new(List.new(0.0, 4.0, 9.0), List.new(3))
    return Tensor.sqrt(value)
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.relu

```text
Tensor.relu(value: Tensor<FP32>) -> Tensor<FP32>!
```

Keep positive elements and replace all other elements with positive zero.
The shape and device location do not change.

**Failures:** Evaluation can produce `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    value = Tensor.new(List.new(-1.0, 0.0, 2.0), List.new(3))
    return Tensor.relu(value)
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.matmul

```text
Tensor.matmul(left: Tensor<FP32>, right: Tensor<FP32>) -> Tensor<FP32>!
```

Multiply rank-two shapes `[m,k]` and `[k,n]` to produce `[m,n]`.
Each dot product uses increasing contracted-index order.
Multiplication and addition use separate FP32 rounding.
A zero contracted dimension produces positive zeros.

**Failures:** Evaluation can produce `Tensor.ShapeMismatch`, `Numeric.NonFinite`, `Tensor.DeviceMismatch`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    left = Tensor.new(List.new(1.0, 2.0), List.new(1, 2))
    right = Tensor.new(List.new(3.0, 4.0), List.new(2, 1))
    return Tensor.matmul(left, right)
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.broadcast

```text
Tensor.broadcast(value: Tensor<FP32>, shape: List<Int>) -> Tensor<FP32>!
```

Broadcast a tensor to exactly the requested shape.
Dimensions align from the right. Missing leading dimensions have size one.
Size one can expand to size zero.
An incompatible target shape produces `Tensor.ShapeMismatch`.

**Failures:** Evaluation can produce `Tensor.InvalidShape`, `Tensor.ShapeMismatch`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    value = Tensor.new(List.new(2.0), List.new(1))
    return Tensor.broadcast(value, List.new(2, 3))
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.reshape

```text
Tensor.reshape(value: Tensor<FP32>, shape: List<Int>) -> Tensor<FP32>!
```

Change the shape without a change to the element count or row-major order.
The result can share immutable storage with the input.
The initial tensor keeps its shape.

**Failures:** Evaluation can produce `Tensor.InvalidShape`, `Tensor.ShapeMismatch`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    value = Tensor.new(List.new(1.0, 2.0, 3.0, 4.0), List.new(4))
    return Tensor.reshape(value, List.new(2, 2))
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.transpose

```text
Tensor.transpose(value: Tensor<FP32>, axes: List<Int>) -> Tensor<FP32>!
```

Permute all axes exactly once and construct a new row-major result.
The axis list must contain every input axis without duplicates.
For a rank-zero tensor, the axis list is empty.

**Failures:** Evaluation can produce `Tensor.InvalidShape`, `Tensor.ShapeMismatch`, `Device.UnsupportedOperation`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    value = Tensor.new(List.new(1.0, 2.0, 3.0, 4.0), List.new(2, 2))
    return Tensor.transpose(value, List.new(1, 0))
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.sum

```text
Tensor.sum(value: Tensor<FP32>, axes: List<Int>) -> Tensor<FP32>!
```

Sum selected axes and remove those dimensions.
Axes must be unique, nonnegative, and strictly increasing.
An empty axis list preserves the shape and values.
Reduction order follows input row-major order.
An empty reduction region returns positive zero.

**Failures:** Evaluation can produce `Tensor.InvalidShape`, `Tensor.ShapeMismatch`, `Device.UnsupportedOperation`, `Numeric.NonFinite`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    value = Tensor.new(List.new(1.0, 2.0, 3.0, 4.0), List.new(2, 2))
    return Tensor.sum(value, List.new(1))
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).


## Tensor.mean

```text
Tensor.mean(value: Tensor<FP32>, axes: List<Int>) -> Tensor<FP32>!
```

Sum selected axes, divide by their element count, and remove those dimensions.
Axes must be unique, nonnegative, and strictly increasing.
An empty axis list preserves the shape and values.
Reduction order follows input row-major order.
An empty reduction region produces `Tensor.EmptyReduction`, even for an empty output.

**Failures:** Evaluation can produce `Tensor.InvalidShape`, `Tensor.ShapeMismatch`, `Device.UnsupportedOperation`, `Numeric.NonFinite`, `Tensor.EmptyReduction`, `Device.Unavailable`, `Device.UnsupportedProfile`, `Device.AllocationFailed`, `Device.LibraryFailed`, `Device.ExecutionFailed`, `Device.Lost`, `Tensor.DeviceMismatch`.

**Authority:** CPU computation needs no device grant. Non-CPU computation uses the explicitly granted device session. No commit.

**Example:**

```lo
flow Main() -> Tensor<FP32>! {
    value = Tensor.new(List.new(1.0, 2.0, 3.0, 4.0), List.new(2, 2))
    return Tensor.mean(value, List.new(1))
}
return Main()
```

[Contract](../../specification.md#243-tensor-operation-set).
