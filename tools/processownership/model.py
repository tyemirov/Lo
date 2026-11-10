"""Check bounded ownership traces under the documented Endpoint Security contract."""

from __future__ import annotations

from dataclasses import dataclass, field
from itertools import product


@dataclass(frozen=True, order=True)
class Token:
    pid: int
    version: int


SUPERVISOR = Token(1, 1)
ROOT = Token(2, 1)
MARKER_FILE = (1, 7)


@dataclass(frozen=True)
class Event:
    sequence: int
    kind: str
    actor: Token
    target: Token | None = None
    object_identity: tuple[int, int] | None = None


@dataclass(frozen=True)
class Sync:
    generation: int
    marker: int


@dataclass
class Owner:
    # Sequence zero represents an accepted initial supervisor marker.
    sequence: int = 0
    initial_pending: bool = True
    live: dict[int, set[Token]] = field(default_factory=dict)
    pending_auth: dict[int, Token] = field(default_factory=dict)
    lost: bool = False
    frozen: bool = False
    client_open: bool = True
    generation: int = 1
    marker: int = 0
    marker_observed: int = 0
    marker_drained: int = 0
    marker_opened: bool = False
    token_limit: int = 4
    termination: set[Token] = field(default_factory=set)

    def receive(self, event: Event) -> None:
        if self.lost:
            return
        if not self.client_open or event.sequence != self.sequence + 1:
            self.lost = True
            return
        self.sequence = event.sequence
        known = event.actor in self.live.get(event.actor.pid, set())
        if event.kind == "fork":
            target = event.target
            if target is None or target.pid in self.live:
                self.lost = True
                return
            initial = self.initial_pending and event.actor == SUPERVISOR and target == ROOT
            if not initial and not known:
                self.lost = True
                return
            self.initial_pending &= not initial
            self.live[target.pid] = {target}
            if self.frozen:
                self.termination.add(target)
        elif event.kind == "auth_exec":
            target = event.target
            if not known or target is None or target.pid != event.actor.pid or target.pid in self.pending_auth:
                self.lost = True
                return
            tokens = self.live[target.pid]
            if target not in tokens and len(tokens) >= self.token_limit:
                self.lost = True
                return
            tokens.add(target)
            self.pending_auth[target.pid] = target
        elif event.kind == "exec":
            target = event.target
            if not known or target is None or target not in self.live[event.actor.pid]:
                self.lost = True
                return
            self.live[target.pid] = {target}
            self.pending_auth.pop(target.pid, None)
            if self.frozen:
                self.termination.add(target)
        elif event.kind == "exit":
            if not known:
                self.lost = True
                return
            del self.live[event.actor.pid]
            self.pending_auth.pop(event.actor.pid, None)
        elif event.kind == "open":
            if event.actor == SUPERVISOR and event.object_identity == MARKER_FILE and self.marker > self.marker_observed:
                self.marker_observed = self.marker
        else:
            self.lost = True

    def freeze(self) -> None:
        self.frozen = True
        for tokens in self.live.values():
            self.termination.update(tokens)

    def reply_exec(self, token: Token) -> bool:
        """Serialize the final reply with freeze; previously sent replies remain sent."""
        if self.pending_auth.get(token.pid) != token:
            self.lost = True
            return False
        del self.pending_auth[token.pid]
        return not self.frozen and not self.lost and self.client_open

    def begin_sync(self) -> Sync:
        if self.marker != self.marker_drained:
            self.lost = True
        self.marker += 1
        self.marker_opened = False
        return Sync(self.generation, self.marker)

    def opened_marker(self, identity: tuple[int, int] | None) -> None:
        if identity != MARKER_FILE:
            self.lost = True
        else:
            self.marker_opened = True

    def synchronized(self, sync: Sync) -> None:
        if self.client_open and sync.generation == self.generation and sync.marker == self.marker_observed:
            self.marker_drained = sync.marker

    def barrier(self, *, root_reaped: bool, pipes_closed: bool, sink_joined: bool) -> bool:
        return (
            self.client_open
            and not self.lost
            and not self.initial_pending
            and not self.live
            and self.marker > 0
            and self.marker_opened
            and self.marker == self.marker_observed == self.marker_drained
            and root_reaped
            and pipes_closed
            and sink_joined
        )


def complete(owner: Owner) -> bool:
    return owner.barrier(root_reaped=True, pipes_closed=True, sink_joined=True)


def drain(owner: Owner, sequence: int | None = None) -> None:
    sync = owner.begin_sync()
    owner.opened_marker(MARKER_FILE)
    owner.receive(Event(sequence or owner.sequence + 1, "open", SUPERVISOR, object_identity=MARKER_FILE))
    owner.synchronized(sync)


def traces(
    alive: frozenset[Token] = frozenset({ROOT}),
    born: int = 1,
    events: tuple[Event, ...] = (Event(1, "fork", SUPERVISOR, ROOT),),
) -> list[tuple[Event, ...]]:
    if not alive:
        return [events]
    results: list[tuple[Event, ...]] = []
    sequence = len(events) + 1
    for actor in sorted(alive):
        results.extend(traces(alive - {actor}, born, events + (Event(sequence, "exit", actor),)))
        if born < 3:
            child = Token(born + 2, 1)
            results.extend(traces(alive | {child}, born + 1, events + (Event(sequence, "fork", actor, child),)))
    return results


def verify_model() -> dict[str, int]:
    checks = 0
    schedules = traces()
    for events in schedules:
        for generated in range(len(events) + 1):
            actual: set[Token] = set()
            for event in events[:generated]:
                if event.kind == "fork":
                    assert event.target is not None
                    actual.add(event.target)
                else:
                    actual.remove(event.actor)
            for keep in product((False, True), repeat=generated):
                queued = [event for event, retained in zip(events, keep) if retained]
                for delivered in range(len(queued) + 1):
                    for cancelled in (False, True):
                        owner = Owner()
                        if cancelled:
                            owner.freeze()
                        for event in queued[:delivered]:
                            owner.receive(event)
                        sync = owner.begin_sync()
                        owner.opened_marker(MARKER_FILE)
                        # FIFO drainage cannot pass an earlier retained but undelivered event.
                        if delivered == len(queued):
                            owner.receive(Event(generated + 1, "open", SUPERVISOR, object_identity=MARKER_FILE))
                            owner.synchronized(sync)
                        success = owner.barrier(root_reaped=ROOT not in actual, pipes_closed=True, sink_joined=True)
                        assert not success or not actual, (events, keep, delivered, actual)
                        if all(keep) and generated == len(events) and delivered == len(queued):
                            assert success, "lossless terminated tree did not complete"
                        checks += 1

    edges = 0
    before, after = ROOT, Token(ROOT.pid, 2)
    for exit_token in (before, after):
        owner = Owner()
        owner.receive(Event(1, "fork", SUPERVISOR, before))
        owner.receive(Event(2, "auth_exec", before, after))
        owner.freeze()
        assert not owner.reply_exec(after)
        owner.receive(Event(3, "exit", exit_token))
        drain(owner)
        assert complete(owner)
        edges += 1
    owner = Owner()
    owner.receive(Event(1, "fork", SUPERVISOR, before))
    owner.receive(Event(2, "auth_exec", before, after))
    assert owner.reply_exec(after)
    owner.freeze()
    owner.receive(Event(3, "exec", before, after))
    assert after in owner.termination
    child = Token(3, 1)
    owner.receive(Event(4, "fork", after, child))
    assert child in owner.termination
    owner.receive(Event(5, "exit", after))
    assert not complete(owner)
    owner.receive(Event(6, "exit", child))
    drain(owner)
    assert complete(owner)
    edges += 1

    owner = Owner()
    owner.receive(Event(1, "fork", SUPERVISOR, before))
    owner.receive(Event(2, "auth_exec", before, after))
    owner.reply_exec(after)
    owner.receive(Event(3, "exec", before, after))
    owner.receive(Event(4, "exit", before))
    assert owner.lost and not complete(owner)
    edges += 1
    owner = Owner(token_limit=2)
    owner.receive(Event(1, "fork", SUPERVISOR, before))
    owner.receive(Event(2, "auth_exec", before, after))
    owner.freeze()
    assert not owner.reply_exec(after)
    owner.receive(Event(3, "auth_exec", before, Token(ROOT.pid, 3)))
    assert owner.lost and len(owner.live[ROOT.pid]) == 2
    edges += 1

    for missing in ("root_reaped", "pipes_closed", "sink_joined"):
        owner = Owner(initial_pending=False)
        drain(owner)
        requirements = dict(root_reaped=True, pipes_closed=True, sink_joined=True)
        requirements[missing] = False
        assert not owner.barrier(**requirements)
        edges += 1
    for invalid in ("missing_marker", "destroyed_client", "different_generation", "wrong_actor", "wrong_object", "failed_open"):
        owner = Owner(initial_pending=False)
        sync = owner.begin_sync()
        owner.opened_marker(None if invalid == "failed_open" else MARKER_FILE)
        if invalid != "missing_marker":
            actor = ROOT if invalid == "wrong_actor" else SUPERVISOR
            identity = (1, 8) if invalid == "wrong_object" else MARKER_FILE
            owner.receive(Event(1, "open", actor, object_identity=identity))
        if invalid == "destroyed_client":
            owner.client_open = False
        if invalid == "different_generation":
            owner.generation += 1
        owner.synchronized(sync)
        assert not complete(owner)
        edges += 1
    owner = Owner(initial_pending=False, lost=True)
    owner.receive(Event(1, "fork", SUPERVISOR, ROOT))
    drain(owner)
    assert owner.lost and not complete(owner)
    edges += 1
    owner = Owner()
    owner.receive(Event(1, "fork", SUPERVISOR, ROOT))
    owner.freeze()
    owner.receive(Event(2, "open", ROOT, object_identity=MARKER_FILE))
    owner.receive(Event(3, "exit", ROOT))
    drain(owner)
    assert complete(owner)
    edges += 1
    return {"lifecycle_traces": len(schedules), "prefix_loss_checks": checks, "edge_checks": edges}
