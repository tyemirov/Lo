# XOR Neural Network

The [CPU example](../examples/xor_cpu.lo) trains a neural network on the complete XOR truth table.
The [Metal example](../examples/xor_metal.lo) executes the same network with the explicit `metal:0` backend.
The examples differ only in their selected device.
Both examples contain their data and initial weights.
They need no external model, dataset, or Python dependency.

## Network and Training

XOR returns one when exactly one input is one.

| First input | Second input | Target |
| --- | --- | --- |
| 0 | 0 | 0 |
| 0 | 1 | 1 |
| 1 | 0 | 1 |
| 1 | 1 | 0 |

The network has two inputs, two ReLU hidden units, and one linear output.
It has nine trainable values: four hidden weights, two hidden biases, two output weights, and one output bias.
The hidden weights and output weights start with different fixed values.
Each run uses the same input order and initial values.

`Forward` calculates the hidden layer and output:

```lo
hidden_linear = Tensor.matmul(inputs, values.w1)
hidden_biased = Tensor.add(hidden_linear, values.b1)
hidden = Tensor.relu(hidden_biased)
output_linear = Tensor.matmul(hidden, values.w2)
return Tensor.add(output_linear, values.b2)
```

`Loss` calculates mean squared error across the four predictions.
`TrainStep` uses `Autodiff.grad` to calculate gradients for all weights and biases.
`Optimizer.SGD.step` returns updated Parameters and immutable optimizer state.
`List.fold` applies 500 updates with learning rate `0.1`.
The output contains the initial loss, final loss, four predictions, step count, and trained Parameters.

The predictions are numeric scores from a linear output.
A score at least `0.5` represents one, and a lower score represents zero.
The example returns raw scores so the numerical error remains visible.

## CPU Execution

Build the compiler and execute the example:

```bash
make build
./bin/lo parse examples/xor_cpu.lo --format json
./bin/lo check examples/xor_cpu.lo --format json
./bin/lo inspect examples/xor_cpu.lo --format json
./bin/lo run examples/xor_cpu.lo --format json
```

For a standalone CPU executable, use these commands:

```bash
LO_BUILD_ROOT="$PWD/_scratch/build-state" ./bin/lo build examples/xor_cpu.lo --output ./bin/xor-cpu
./bin/xor-cpu run --format json
```

## Metal Execution

Use the [selected Metal toolchain](metal-transfers.md) for local Metal execution:

```bash
make build-metal
./bin/lo-metal run examples/xor_metal.lo --allow-device metal:0 --format json
LO_BUILD_ROOT="$PWD/_scratch/build-state" ./bin/lo-metal build examples/xor_metal.lo --device-backend metal --output ./bin/xor-metal
./bin/xor-metal run --allow-device metal:0 --format json
```

The Metal example uploads the inputs, targets, and four initial Parameter tensors once.
The forward graph, gradients, and optimizer updates stay on Metal during all 500 steps.
Seven explicit downloads return the initial loss, final loss, predictions, and four trained Parameter tensors.
The CPU example uses the same transfer calls with `cpu`, where each transfer is a no-op.
The standard compiler returns `Device.Unavailable` for authorized Metal execution.

## Observed Results

On October 16, 2026, CPU execution and the independent Metal executor produced these results:

| Input | Target | Prediction after training | Classification |
| --- | --- | --- | --- |
| `[0,0]` | 0 | `0.00000066410286` | 0 |
| `[0,1]` | 1 | `0.9999993` | 1 |
| `[1,0]` | 1 | `0.9999995` | 1 |
| `[1,1]` | 0 | `0.00000072148754` | 0 |

Mean squared error decreased from `0.3561249971` to approximately `4.25e-13`.
Each execution completed 500 updates and returned finite Parameters.
These observations cover all four possible binary input pairs.
They do not establish accuracy for inputs outside the XOR truth table.

The [software results](evidence/xor-software.txt) record three identical CPU runs and three identical Metal model runs.
The [local Metal results](evidence/xor-metal.txt) record matching interpreter and native execution with the Go race detector.
The local environment used Apple M5 Pro, macOS 27.0.1, and the current `metal-fp32` profile.
The [complete result](evidence/xor-result.json) includes all nine trained Parameter values.
The [execution identity](evidence/xor-identity.json) records the source digests, toolchain versions, and kernel digest.
The [initial test failure](evidence/xor-red.txt) records the missing examples before implementation.
The [full CI result](evidence/xor-ci.txt) records passing software tests, native tests, static checks, and the compiler build.
The [documentation review](evidence/xor-review.json) records the prose review and local link checks.

The [integration tests](../tests/integration/xor_test.go) require correct classification of every row and final loss below `0.001`.
They compare the reported loss with an independent calculation from the returned predictions.
They also require identical results across three executions and complete cleanup in the Metal executor.
The [local Metal tests](../tests/integration/xor_metal_live_test.go) compare interpreter and native results with the CPU result.

Run the focused tests with these commands:

```bash
make test-integration TEST_ARGS='-run TestXOR -count=1 -v'
make test-native TEST_ARGS='-run TestXOR -count=1 -v'
make test-metal TEST_ARGS='-run TestMetalLiveXOR -count=1 -race -v'
make test-native-metal TEST_ARGS='-run TestNativeMetalLiveXOR -count=1 -race -v'
```

The software tests provide acceptance without a physical-device requirement.
The Metal targets supply optional local execution evidence.

## Other Training Examples

| Example or fixture | Purpose |
| --- | --- |
| [CPU regression](../examples/training_cpu.lo) and [Metal regression](../examples/training_metal.lo) | The same two-input network fits targets `[0,2,1,3]` with 200 SGD updates. |
| [Dense20 benchmark](../tests/benchmark/testdata/training_dense.lo) | A network with 16 inputs, 16 hidden units, and eight outputs executes 20 updates on 32 input rows. |
| [Metal arithmetic](../examples/metal_arithmetic.lo) | One gradient and SGD update for the quadratic loss `sum(x*x)`. |
| [Optimizer fixtures](../tests/integration/optimizer_test.go) | Fixed SGD and AdamW transitions, including moments and weight decay. |
| [Tensor forward example](../examples/tensor_forward.lo) | A forward pass and loss calculation with fixed weights. It does not train. |
