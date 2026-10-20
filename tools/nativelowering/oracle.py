"""Independent, manually authored semantic schedules for the bounded fixtures.

Schedules follow the evaluator's statement/expression/loop rules. They never read
emitted IR, metadata, executable events or evaluator output to obtain expectations.
"""
from __future__ import annotations
from dataclasses import dataclass
from typing import Any

@dataclass(frozen=True)
class Fixture:
    name: str
    value: Any
    events: tuple[str, ...]

STEP = ['step']
def literal(number: int) -> list[str]:
    return ['step', f'literal:{number}', f'charge:{16 + (abs(number).bit_length() + 7) // 8}']
def reference(weight: int = 17) -> list[str]:
    return ['step', f'charge:{weight}']
def operation(name: str, children: list[str], weight: int = 17) -> list[str]:
    return ['step', *children, f'op:{name}', f'charge:{weight}']

# Literal source arguments are evaluated in source order; parameters bind by name.
named = STEP + STEP + operation('Math.mul', literal(2) + literal(3)) + operation('Math.add', literal(10) + literal(1)) + STEP + operation('Math.sub', reference() + reference()) + ['charge:17']
lazy = STEP + STEP + literal(1) + literal(1) + ['match:true'] + literal(7) + ['charge:17']
stored = STEP + operation('List.new', operation('Math.div', literal(8) + literal(0), 151), 167)
recover = STEP + STEP + STEP + STEP + STEP + operation('Math.div', literal(8) + literal(0), 151) + ['propagate:error', 'frame:Attempt'] + STEP + literal(41) + ['charge:17']
map_events = STEP + STEP + operation('List.new', literal(1) + literal(2), 50) + ['op:List.map']
for index, item in enumerate((1, 2)):
    map_events += STEP + [f'callback:{index}'] + STEP + operation('Math.mul', reference() + literal(2))
map_events += ['charge:50']  # Infallible callback gives a plain List<Int>.
fold_events = STEP + STEP + operation('List.new', literal(1) + literal(2), 50) + literal(0) + ['op:Flow.fold', 'progress:156']
for index, state in enumerate((0, 1)):
    fold_events += STEP + [f'callback:{index}'] + STEP + operation('Math.add', reference(16 if state == 0 else 17) + reference()) + ['progress:159']
fold_events += ['charge:17']
# Math.div's error weighs135; its opaque Failure adds16. The List.map
# Divide adds139; collection input(index1,element0) weighs61/frame142;
# wrapped error432. No third callback or List.length is evaluated.
callback_error = STEP + STEP + STEP + STEP + STEP + STEP + operation('List.new', literal(2) + literal(0) + literal(4), 66) + ['op:List.map']
callback_error += STEP + ['callback:0'] + STEP + operation('Math.div', literal(8) + reference(), 33) + ['flow-return:success']
callback_error += STEP + ['callback:1'] + STEP + operation('Math.div', literal(8) + reference(16), 151) + ['flow-return:error']
callback_error += ['frame:Divide', 'frame:Attempt', 'charge:432', 'propagate:error', 'frame:Attempt'] + STEP + literal(23) + ['charge:17']
terminal = STEP + STEP + STEP + literal(7) + STEP + operation('Math.add', literal(8) + literal(9)) + ['charge:17']
FIXTURES = (
    Fixture('literal', 7, tuple(STEP + literal(7))),
    Fixture('no-return', None, tuple(STEP + literal(7) + ['literal:null'])),
    Fixture('named-arguments', 5, tuple(named)),
    Fixture('lazy-branch', 7, tuple(lazy)),
    Fixture('stored-fallible', [{'failure': {'kind': 'Math.DivideByZero', 'frames': []}}], tuple(stored)),
    Fixture('recoverable-handler', 41, tuple(recover)),
    Fixture('callback-map', [2, 4], tuple(map_events)),
    Fixture('callback-error', 23, tuple(callback_error)),
    Fixture('fallible-flow-success', {'success': 4}, tuple(STEP + STEP + literal(2) + STEP + operation('Math.div', literal(8) + reference(), 33) + ['flow-return:success', 'charge:33'])),
    Fixture('fold', 3, tuple(fold_events)),
    Fixture('terminal-handler', 17, tuple(terminal)),
)

def expected(fixture: Fixture, budget: int, cancel: int = -1, weight_limit: int = 67108864) -> dict[str, Any]:
    events: list[str] = []
    steps = weight = status = code = 0
    for event in fixture.events:
        events.append(event)
        if event == 'step':
            if cancel >= 0 and steps >= cancel:
                status, code = 2, 2
            elif steps == budget:
                status, code = 2, 1
            else:
                steps += 1
        elif event.startswith(('charge:', 'progress:')):
            amount = int(event.split(':')[1])
            if event.startswith('progress:') and cancel >= 0 and steps >= cancel:
                status, code = 2, 2
            elif amount > 16777216 or weight + amount > weight_limit:
                status, code = 2, 1
            else:
                weight += amount
        if status:
            events.append('cancel' if code == 2 else 'limit')
            break
    return dict(events=events, status=status, code=code, value=None if status else fixture.value,
                steps=steps, weight=weight, live=0, scopes=0, depth=0)
