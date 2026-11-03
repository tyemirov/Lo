package integration_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func nativeShape(dimensions []int) []string {
	shape := make([]string, len(dimensions))
	for index, dimension := range dimensions {
		shape[index] = strconv.Itoa(dimension)
	}
	return shape
}

func nativeErrorKind(kind, message string) func(*testing.T, runtimeValue) {
	return func(t *testing.T, value runtimeValue) {
		t.Helper()
		valueJSON(t, recordField(t, value, "kind"), strconv.Quote(kind))
		valueJSON(t, recordField(t, value, "message"), strconv.Quote(message))
	}
}

func nativeValuesCheck(count int, check func(*testing.T, runtimeValue)) func(*testing.T, result) {
	return func(t *testing.T, got result) {
		t.Helper()
		if got.status != 0 || got.stderr != "" {
			t.Fatalf("native fixture failed: %+v", got)
		}
		decoder := json.NewDecoder(strings.NewReader(got.stdout))
		for index := 0; index < count; index++ {
			var envelope json.RawMessage
			if err := decoder.Decode(&envelope); err != nil {
				t.Fatalf("native result %d of %d: %v", index+1, count, err)
			}
			check(t, nativeValue(t, result{stdout: string(envelope)}))
		}
		var extra json.RawMessage
		if err := decoder.Decode(&extra); err != io.EOF {
			t.Fatalf("unexpected native result after %d envelopes: %s (%v)", count, extra, err)
		}
	}
}

func checkNativeTensor(t *testing.T, tensor runtimeValue, shape []string, expected []float64, atol, rtol float64) {
	t.Helper()
	checkNativeTensorMetadata(t, tensor, shape)
	checkNumbers(t, tensorNumbers(t, tensor), expected, atol, rtol)
}

func checkNativeTensorMetadata(t *testing.T, tensor runtimeValue, shape []string) {
	t.Helper()
	var metadata struct {
		Dtype, Device string
		Shape         []string
	}
	if err := json.Unmarshal(tensor.Value, &metadata); err != nil || metadata.Dtype != "fp32" || metadata.Device != "cpu" || !slices.Equal(metadata.Shape, shape) {
		t.Fatalf("native tensor metadata: %s (%v)", tensor.Value, err)
	}
}

func checkNativeTensorBits(t *testing.T, tensor runtimeValue, shape []string, bits []uint32) {
	t.Helper()
	checkNativeTensorMetadata(t, tensor, shape)
	numbers := tensorNumbers(t, tensor)
	if len(numbers) != len(bits) {
		t.Fatalf("native tensor length=%d, want %d", len(numbers), len(bits))
	}
	for index, number := range numbers {
		if actual := math.Float32bits(float32(number)); actual != bits[index] {
			t.Fatalf("native tensor bits[%d]=%08x, want %08x", index, actual, bits[index])
		}
	}
}

func nativeBytes(t *testing.T, value runtimeValue) []byte {
	t.Helper()
	if value.Type != "Bytes" {
		t.Fatalf("native byte type: %s", value.Type)
	}
	var encoded string
	if err := json.Unmarshal(value.Value, &encoded); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return decoded
}

func nativeCheckpoint(t *testing.T, value runtimeValue) runtimeValue {
	t.Helper()
	var decoded runtimeValue
	if err := json.Unmarshal(nativeBytes(t, value), &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func checkNativeCheckpointTransfer(t *testing.T, value runtimeValue, kind string) {
	t.Helper()
	before, after := recordField(t, value, "before"), recordField(t, value, "after")
	if string(nativeBytes(t, before)) != string(nativeBytes(t, after)) {
		t.Fatal("checkpoint changed during device transfer")
	}
	saved := nativeCheckpoint(t, after)
	parameters, expected := saved, []float64{1, -2}
	if kind != "Parameters" {
		parameters = recordField(t, saved, "parameters")
		valueJSON(t, recordField(t, saved, "step"), `"1"`)
		expected = []float64{.99, -1.98}
		if kind == "AdamW" {
			expected = []float64{.9890000224113464, -1.9880000352859497}
		}
	}
	if parameters.Type != "Parameters<Record{x: Tensor<FP32>}>" {
		t.Fatalf("checkpoint parameters type: %s", parameters.Type)
	}
	checkNativeTensor(t, recordField(t, parameters, "x"), []string{"2"}, expected, 1e-6, 1e-5)
}

type nativeTrainingStep struct {
	loss      float64
	gradients []float64
}

// This reference differentiates the fixed two-layer example analytically.
func nativeTrainingReference(t *testing.T) []nativeTrainingStep {
	t.Helper()
	parameters := []float64{1, 0, 0, 1, 0, 0, .25, .25, 0}
	inputs := [][2]float64{{-1, -1}, {-1, 1}, {1, -1}, {1, 1}}
	targets := []float64{0, 2, 1, 3}
	steps := make([]nativeTrainingStep, 200)
	for index := range steps {
		gradients := make([]float64, 9)
		var loss float64
		for row, input := range inputs {
			hidden := [2]float64{}
			for column := range hidden {
				hidden[column] = math.Max(0, input[0]*parameters[column]+input[1]*parameters[2+column]+parameters[4+column])
			}
			residual := hidden[0]*parameters[6] + hidden[1]*parameters[7] + parameters[8] - targets[row]
			loss += residual * residual / 4
			outputGradient := residual / 2
			gradients[8] += outputGradient
			for column, activation := range hidden {
				gradients[6+column] += outputGradient * activation
				if activation > 0 {
					hiddenGradient := outputGradient * parameters[6+column]
					gradients[4+column] += hiddenGradient
					gradients[column] += input[0] * hiddenGradient
					gradients[2+column] += input[1] * hiddenGradient
				}
			}
		}
		steps[index] = nativeTrainingStep{loss: loss, gradients: gradients}
		for parameter := range parameters {
			parameters[parameter] -= .05 * gradients[parameter]
		}
		if index == 0 {
			for _, reference := range trainingReferences(t) {
				checkNumbers(t, []float64{loss}, []float64{reference.InitialLoss}, 0, 0)
				checkNumbers(t, gradients, reference.InitialGradients, 1e-6, 1e-5)
				checkNumbers(t, parameters, reference.FirstParameters, 1e-6, 1e-5)
			}
		}
	}
	for _, reference := range trainingReferences(t) {
		checkNumbers(t, parameters, reference.FinalParameters, 1e-6, 1e-5)
	}
	return steps
}

func checkNativeTrainingGradients(t *testing.T, value runtimeValue) {
	t.Helper()
	var observed []runtimeValue
	if err := json.Unmarshal(value.Value, &observed); err != nil || len(observed) != 200 {
		t.Fatalf("native training observations: %d (%v)", len(observed), err)
	}
	for index, expected := range nativeTrainingReference(t) {
		checkNumbers(t, []float64{trainingFloat(t, recordField(t, observed[index], "loss"))}, []float64{expected.loss}, 1e-5, 1e-4)
		checkNumbers(t, trainingParameters(t, recordField(t, observed[index], "gradients")), expected.gradients, 1e-5, 1e-4)
	}
}
