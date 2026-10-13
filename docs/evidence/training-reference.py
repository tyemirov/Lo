"""Independent arithmetic reference for the P001 design, without Lo execution."""

import json
import math
import struct
from collections.abc import Callable, Iterable


def fp32(value: float) -> float:
    result = struct.unpack("!f", struct.pack("!f", value))[0]
    assert math.isfinite(result)
    return result


def calculate(round_value: Callable[[float], float]) -> dict[str, object]:
    q = round_value
    add = lambda a, b: q(a + b)
    sub = lambda a, b: q(a - b)
    mul = lambda a, b: q(a * b)
    div = lambda a, b: q(a / b)

    def total(values: Iterable[float]) -> float:
        result = q(0)
        for value in values:
            result = add(result, value)
        return result

    inputs = [(-1, -1), (-1, 1), (1, -1), (1, 1)]
    targets = [0, 2, 1, 3]
    initial = [1, 0, 0, 1, 0, 0, 0.25, 0.25, 0]

    def evaluate(parameters: list[float]) -> tuple[float, list[float], list[float]]:
        w00, w01, w10, w11, b0, b1, v0, v1, bias = parameters
        rows = []
        for (x0, x1), target in zip(inputs, targets):
            z0 = add(add(mul(x0, w00), mul(x1, w10)), b0)
            z1 = add(add(mul(x0, w01), mul(x1, w11)), b1)
            h0, h1 = max(0.0, z0), max(0.0, z1)
            prediction = add(add(mul(h0, v0), mul(h1, v1)), bias)
            residual = sub(prediction, target)
            # Reverse contributions from both edges of residual * residual.
            d = add(div(residual, 4), div(residual, 4))
            dz0 = mul(d, v0) if z0 > 0 else 0.0
            dz1 = mul(d, v1) if z1 > 0 else 0.0
            rows.append((prediction, mul(residual, residual), [
                mul(x0, dz0), mul(x0, dz1),
                mul(x1, dz0), mul(x1, dz1),
                dz0, dz1, mul(h0, d), mul(h1, d), d,
            ]))
        loss = div(total(row[1] for row in rows), 4)
        gradients = [total(row[2][i] for row in rows) for i in range(9)]
        return loss, gradients, [row[0] for row in rows]

    parameters = [q(x) for x in initial]
    initial_loss, initial_gradients, initial_predictions = evaluate(parameters)
    assert initial_predictions == [0, 0.25, 0.25, 0.5]
    assert initial_loss == 2.46875
    assert initial_gradients == [
        -0.40625, -0.09375, -0.21875, -0.53125,
        -0.40625, -0.53125, -1.625, -2.125, -2.5,
    ]
    first_parameters = None
    for _ in range(200):
        loss, gradients, _ = evaluate(parameters)
        assert all(math.isfinite(x) for x in [loss, *gradients])
        parameters = [sub(p, mul(q(0.05), g)) for p, g in zip(parameters, gradients)]
        if first_parameters is None:
            first_parameters = parameters[:]
    final_loss, _, final_predictions = evaluate(parameters)
    assert final_loss < 0.001
    assert abs(first_parameters[6] - 0.33125) < 1e-7
    assert abs(first_parameters[7] - 0.35625) < 1e-7
    assert first_parameters[8] == 0.125

    # Move away from ReLU boundaries before central finite differences.
    smooth = [q(x) for x in [1, 0.1, 0.2, 1, 0.3, 0.4, 0.25, 0.5, 0.1]]
    _, analytic, _ = evaluate(smooth)
    step = 0.001 if q is fp32 else 1e-6
    differences = []
    for i, expected in enumerate(analytic):
        plus, minus = smooth[:], smooth[:]
        plus[i], minus[i] = q(plus[i] + step), q(minus[i] - step)
        derivative = (evaluate(plus)[0] - evaluate(minus)[0]) / (2 * step)
        error = abs(derivative - expected)
        tolerance = (0.002 + 0.02 * abs(expected)) if q is fp32 else (1e-8 + 1e-7 * abs(expected))
        assert error <= tolerance
        differences.append(error)

    # AdamW with two fixed gradients, including a zero-gradient coordinate.
    parameters = [q(1), q(-2)]
    first, second = [q(0), q(0)], [q(0), q(0)]
    power1, power2 = q(1), q(1)
    rate, beta1, beta2, epsilon, decay = map(q, [0.01, 0.9, 0.999, 1e-8, 0.1])
    adamw_steps = []
    for gradient in [[0.1, -0.2], [0.0, 0.3]]:
        gradient = [q(x) for x in gradient]
        first = [add(mul(beta1, m), mul(sub(1, beta1), g)) for m, g in zip(first, gradient)]
        second = [add(mul(beta2, v), mul(sub(1, beta2), mul(g, g))) for v, g in zip(second, gradient)]
        power1, power2 = mul(power1, beta1), mul(power2, beta2)
        corrected1 = [div(m, sub(1, power1)) for m in first]
        corrected2 = [div(v, sub(1, power2)) for v in second]
        parameters = [
            sub(sub(p, mul(mul(rate, decay), p)), mul(rate, div(m, add(q(math.sqrt(v)), epsilon))))
            for p, m, v in zip(parameters, corrected1, corrected2)
        ]
        adamw_steps.append({"parameters": parameters[:], "first_moment": first[:], "second_moment": second[:]})

    return {
        "initial_loss": initial_loss,
        "initial_gradients_w1_b1_w2_b2": initial_gradients,
        "first_parameters_w1_b1_w2_b2": first_parameters,
        "final_loss": final_loss,
        "final_predictions": final_predictions,
        "finite_difference_max_error": max(differences),
        "adamw_steps": adamw_steps,
    }


if __name__ == "__main__":
    print(json.dumps({"binary64_reference": calculate(float), "fp32_reference": calculate(fp32)}, indent=2))
