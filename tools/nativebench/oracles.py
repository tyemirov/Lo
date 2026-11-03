"""Independent scalar numerical expectations for fixed native benchmark inputs."""

from __future__ import annotations

from functools import lru_cache
import json
import math
from pathlib import Path
import re

ROOT = Path(__file__).resolve().parents[2]


def close(
    actual: object, expected: float, *, atol: float = 1e-8, rtol: float = 1e-5
) -> None:
    value = float(actual)
    if not math.isfinite(value) or abs(value - expected) > atol + rtol * abs(expected):
        raise ValueError(f"numerical oracle differs: {value}, expected {expected}")


def unwrap(node: dict) -> dict:
    while node.get("status") == "ok":
        node = node["value"]
    return node


@lru_cache(maxsize=2)
def training_reference(kind: str) -> dict:
    if kind == "training_mvp":
        reference = json.loads(
            (ROOT / "docs/evidence/training-reference-results.json").read_text()
        )["binary64_reference"]
        flat = reference["final_parameters_w1_b1_w2_b2"]
        return {
            "initial_loss": reference["initial_loss"],
            "loss": reference["final_loss"],
            "parameters": {
                "w1": ([2, 2], flat[:4]),
                "b1": ([2], flat[4:6]),
                "w2": ([2, 1], flat[6:8]),
                "b2": ([1], flat[8:]),
            },
        }
    if kind != "training_dense":
        raise ValueError("unknown independent training oracle")
    # Read literal tensors only. Scalar matrix algebra and reverse derivatives below
    # are independent of Lo compilation, tensor operations, autodiff and execution.
    source = (ROOT / "tests/benchmark/testdata/training_dense.lo").read_text()
    tensors = {}
    for name, values, dimensions in re.findall(
        r"^  (inputs|targets|w1|b1|w2|b2) = Tensor.new\(List.new\(([^()]*)\), List.new\(([^()]*)\)\)",
        source,
        re.M,
    ):
        shape = [int(item) for item in dimensions.split(",")]
        numbers = [float(item) for item in values.split(",")]
        if math.prod(shape) != len(numbers):
            raise ValueError("Dense20 literal dimensions differ")
        tensors[name] = (shape, numbers)
    if set(tensors) != {"inputs", "targets", "w1", "b1", "w2", "b2"}:
        raise ValueError("Dense20 independent oracle requires its fixed six tensors")
    if tensors["inputs"][0] != [32, 16] or tensors["targets"][0] != [32, 8]:
        raise ValueError("Dense20 input shape differs")
    inputs, targets = tensors["inputs"][1], tensors["targets"][1]
    weights = {name: list(tensors[name][1]) for name in ("w1", "b1", "w2", "b2")}

    def forward() -> tuple[float, list[float], list[float]]:
        hidden = [
            max(
                0.0,
                weights["b1"][j]
                + sum(
                    inputs[i * 16 + k] * weights["w1"][k * 16 + j] for k in range(16)
                ),
            )
            for i in range(32)
            for j in range(16)
        ]
        residual = [
            weights["b2"][j]
            + sum(hidden[i * 16 + k] * weights["w2"][k * 8 + j] for k in range(16))
            - targets[i * 8 + j]
            for i in range(32)
            for j in range(8)
        ]
        return sum(value * value for value in residual) / 256, hidden, residual

    initial, _, _ = forward()
    for _ in range(20):
        _, hidden, residual = forward()
        output_gradient = [2 * value / 256 for value in residual]
        hidden_gradient = [
            (
                sum(
                    output_gradient[i * 8 + j] * weights["w2"][k * 8 + j]
                    for j in range(8)
                )
                if hidden[i * 16 + k] > 0
                else 0.0
            )
            for i in range(32)
            for k in range(16)
        ]
        gradients = {
            "b2": [
                sum(output_gradient[i * 8 + j] for i in range(32)) for j in range(8)
            ],
            "w2": [
                sum(hidden[i * 16 + k] * output_gradient[i * 8 + j] for i in range(32))
                for k in range(16)
                for j in range(8)
            ],
            "b1": [
                sum(hidden_gradient[i * 16 + j] for i in range(32)) for j in range(16)
            ],
            "w1": [
                sum(inputs[i * 16 + k] * hidden_gradient[i * 16 + j] for i in range(32))
                for k in range(16)
                for j in range(16)
            ],
        }
        weights = {
            name: [
                value - 0.05 * gradient
                for value, gradient in zip(values, gradients[name])
            ]
            for name, values in weights.items()
        }
    final, _, _ = forward()
    if not 0 < final < initial:
        raise ValueError("independent Dense20 SGD reference did not reduce finite loss")
    return {
        "initial_loss": initial,
        "loss": final,
        "parameters": {
            name: (tensors[name][0], values) for name, values in weights.items()
        },
    }


def verify_output(output: str, oracle: dict) -> None:
    kind = oracle["kind"]
    if kind == "exact":
        expected = oracle["expected"]
        try:
            node = unwrap(json.loads(output)["result"])
            if node["type"] != "Int":
                raise ValueError("independent scalar type differs")
            observed = node["value"]
        except (KeyError, TypeError, json.JSONDecodeError):
            observed = output.strip()
        if str(observed) != expected:
            raise ValueError(
                f"independent result differs: {observed}, expected {expected}"
            )
        return
    node = unwrap(json.loads(output)["result"])
    fields = node["value"]
    reference = training_reference(kind)
    if set(fields) != {"initial_loss", "loss", "parameters"}:
        raise ValueError("training result field inventory differs")
    for field in ("initial_loss", "loss"):
        if fields[field]["type"] != "Float":
            raise ValueError("training loss type differs")
        close(fields[field]["value"], reference[field])
    parameters = fields["parameters"]["value"]
    if set(parameters) != set(reference["parameters"]):
        raise ValueError("training parameter fields differ")
    for name, (shape, values) in reference["parameters"].items():
        tensor = parameters[name]
        metadata = tensor["value"]
        actual = metadata["values"]
        if (
            tensor["type"] != "Tensor<FP32>"
            or metadata["dtype"] != "fp32"
            or metadata["device"] != "cpu"
            or list(map(int, metadata["shape"])) != shape
            or len(actual) != len(values)
        ):
            raise ValueError("training tensor type or shape differs")
        for number, expected in zip(actual, values):
            close(number, expected, atol=1e-6)
