use super::provider::{self, Configuration, Event, FileIdentity, Provider, Token};
use std::{mem::size_of, sync::{Condvar, Mutex}, time::Duration};

#[repr(C)]
#[derive(Clone, Copy, Debug)]
pub(crate) struct Limits {
    pub max_processes: u32,
    pub max_tokens: u32,
    pub max_events: u64,
    pub kernel_processes: u32,
}
impl Default for Limits {
    fn default() -> Self {
        Self { max_processes: 256, max_tokens: 1024, max_events: 1_000_000, kernel_processes: 1024 }
    }
}
impl Limits {
    pub fn storage(self) -> Result<u64, &'static str> {
        if self.max_processes == 0 || self.max_tokens == 0 || self.max_events == 0
            || self.kernel_processes == 0 {
            return Err("Process descendant limits must be finite positive integers");
        }
        let tokens = u64::from(self.max_tokens).checked_mul(size_of::<OwnedToken>() as u64)
            .ok_or("Process descendant storage size overflow")?;
        tokens.checked_add(size_of::<Controller>() as u64)
            .ok_or("Process descendant storage size overflow")
    }
}
pub(super) struct Image {
    pub path: String,
    pub file: FileIdentity,
    // The trusted launcher is executable only during initial host startup.
    // Registered command images retain the initial command's sandbox scope.
    pub launch_only: bool,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
enum Decision { None, Pending, Allow, Deny }
struct OwnedToken {
    token: Token,
    image: Option<usize>,
    request: u64,
    decision: Decision,
    confirmed: bool,
    alive: bool,
    signal_epoch: u64,
}
#[derive(Clone, Copy, Debug, Eq, PartialEq)]
pub(super) enum Purpose { Admission, FailedLaunch, Completion }
struct Marker {
    request: u64,
    purpose: Purpose,
    opened: bool,
    observed: bool,
    submitted: bool,
    synchronized: bool,
}
struct State {
    active: bool,
    closing: bool,
    damaged: Option<&'static str>,
    frozen: bool,
    denied_execution: bool,
    events: u64,
    sequence: Option<u64>,
    last_auth_request: u64,
    marker_request: u64,
    marker: Option<Marker>,
    admitted: bool,
    launch_started: bool,
    launch_resolved: bool,
    initial: Option<Token>,
    initial_latest: Option<Token>,
    initial_exec_confirmed: bool,
    processes: u32,
    tokens: Vec<OwnedToken>,
    signal_epoch: u64,
    completion_barrier: bool,
}
impl State {
    fn damage(&mut self, reason: &'static str) {
        self.damaged.get_or_insert(reason);
        self.frozen = true;
        self.signal_epoch = 1;
        self.completion_barrier = false;
    }
    fn known(&self, token: Token) -> Option<usize> {
        self.tokens.iter().position(|entry| entry.alive && entry.token == token)
    }
    fn empty(&self) -> bool { self.processes == 0 && self.tokens.is_empty() }
    fn healthy(&self) -> bool { self.active && !self.closing && self.damaged.is_none() }
    fn pending(&self) -> Option<usize> {
        self.tokens.iter().position(|entry| entry.request != 0 && entry.decision == Decision::Pending)
    }
}

// Event callbacks mutate this fixed-capacity ledger directly. There is no
// event-history allocation or callback queue. At most max_tokens records hold
// unresolved aliases and AUTH requests; process count is a separate limit.
pub(super) struct Controller {
    configuration: Configuration,
    images: Vec<Image>,
    state: Mutex<State>,
    authorization: Mutex<()>,
    changed: Condvar,
}
impl Controller {
    // The caller first reserves Limits::storage plus the image storage through
    // Context's generic temporary/live accounting, and retains that reservation
    // with the remote owner until proven release, including quarantine.
    pub fn new(configuration: Configuration, images: Vec<Image>) -> Result<Self, &'static str> {
        let limits = Limits { max_processes: configuration.max_processes,
            max_tokens: configuration.max_tokens, max_events: configuration.max_events,
            kernel_processes: configuration.kernel_processes };
        limits.storage()?;
        if configuration.generation == 0 || !configuration.supervisor.valid()
            || configuration.supervisor.word[1] == 0
            || configuration.supervisor.word[1] != configuration.supervisor.word[3]
            || configuration.marker == FileIdentity::default() {
            return Err("invalid Process descendant configuration");
        }
        if images.is_empty() || images.iter().enumerate().any(|(index, image)| {
            !image.path.starts_with('/') || image.path.len() > provider::PATH_MAX
                || image.path.as_bytes().contains(&0) || image.file == FileIdentity::default()
                || images[..index].iter().any(|other| other.path == image.path)
        }) { return Err("invalid Process descendant executable registry"); }
        let mut tokens = Vec::new();
        tokens.try_reserve_exact(configuration.max_tokens as usize)
            .map_err(|_| "Process descendant token storage is unavailable")?;
        Ok(Self { configuration, images, authorization: Mutex::new(()), changed: Condvar::new(),
            state: Mutex::new(State { active: true, closing: false, damaged: None,
                frozen: false, denied_execution: false, events: 0, sequence: None,
                last_auth_request: 0, marker_request: 0, marker: None, admitted: false,
                launch_started: false, launch_resolved: false, initial: None, initial_latest: None, initial_exec_confirmed: false, processes: 0,
                tokens, signal_epoch: 0, completion_barrier: false }) })
    }
    pub fn damage(&self, reason: &'static str) {
        self.state.lock().unwrap().damage(reason);
        self.changed.notify_all();
    }
    pub fn failure(&self) -> Option<&'static str> { self.state.lock().unwrap().damaged }
    pub fn denied_execution(&self) -> bool { self.state.lock().unwrap().denied_execution }
    pub fn frozen(&self) -> bool { self.state.lock().unwrap().frozen }
    pub fn empty(&self) -> bool { self.state.lock().unwrap().empty() }
    pub fn launch_resolved(&self) -> bool { self.state.lock().unwrap().launch_resolved }
    pub fn failed_launch_evidence(&self, pid: i32) -> Result<bool, &'static str> {
        let state = self.state.lock().unwrap();
        if let Some(reason) = state.damaged { return Err(reason); }
        if !state.launch_started || state.launch_resolved || !state.empty() || pid < 0 {
            return Err("Process failed-launch receipt is outside its ownership boundary");
        }
        if pid == 0 { return Ok(state.initial.is_none()); }
        if state.initial.is_some_and(|token| token.pid() != pid as u32) {
            return Err("Process failed-launch receipt differs from its initial token");
        }
        Ok(state.initial_latest.is_some_and(|token| token.pid() == pid as u32))
    }
    pub fn launch_was_started(&self) -> bool { self.state.lock().unwrap().launch_started }
    pub fn resolve_without_launch(&self) -> Result<(), &'static str> {
        let mut state = self.state.lock().unwrap();
        if !state.healthy() || !state.admitted || state.launch_started || !state.empty() {
            return Err("Process no-launch boundary is not proved");
        }
        state.launch_resolved = true;
        Ok(())
    }
    pub fn wait(&self, duration: Duration) {
        let state = self.state.lock().unwrap();
        drop(self.changed.wait_timeout(state, duration).unwrap());
    }
    pub fn freeze(&self) {
        // This gate also covers the final allow/deny decision and the provider
        // reply. A prior allow can still deliver EXEC after this gate closes.
        let _gate = self.authorization.lock().unwrap();
        let mut state = self.state.lock().unwrap();
        state.frozen = true;
        state.signal_epoch = 1;
        state.completion_barrier = false;
        self.changed.notify_all();
    }
    pub fn start_launch(&self) -> Result<(), &'static str> {
        let mut state = self.state.lock().unwrap();
        if !state.healthy() || !state.admitted || state.launch_started || state.frozen {
            return Err("Process descendant launch admission is not complete");
        }
        state.launch_started = true;
        Ok(())
    }
    pub fn resolve_launch(&self, token: Token) -> Result<(), &'static str> {
        let mut state = self.state.lock().unwrap();
        if !state.launch_started || state.launch_resolved || state.initial.is_none_or(|first| first.pid() != token.pid())
            || !state.initial_exec_confirmed || state.initial_latest != Some(token) {
            state.damage("initial Process identity did not match its lifecycle event");
            return Err("initial Process identity did not match its lifecycle event");
        }
        state.launch_resolved = true;
        Ok(())
    }
    pub fn launch_evidence(&self, token: Token) -> Result<bool, &'static str> {
        let mut state = self.state.lock().unwrap();
        if let Some(reason) = state.damaged { return Err(reason); }
        if !state.launch_started || state.launch_resolved {
            return Err("Process launch evidence was requested outside its launch interval");
        }
        if state.initial.is_some_and(|initial| initial.pid() != token.pid()) {
            return Err("initial Process receipt identifies a different child");
        }
        if state.initial_latest == Some(token) && !state.initial_exec_confirmed {
            state.damage("Process Ready reused an unexecuted fork token");
            return Err("Process Ready reused an unexecuted fork token");
        }
        // The trusted launch receipt and ES events can arrive in either order.
        // The supervisor waits within the original absolute command deadline.
        Ok(state.initial_exec_confirmed && state.initial_latest == Some(token))
    }
    pub fn receive(&self, event: Event<'_>) -> bool {
        let mut state = self.state.lock().unwrap();
        if !state.active || state.closing || event.generation != self.configuration.generation {
            state.damage("provider event has an inactive client generation");
            self.changed.notify_all();
            return false;
        }
        let exhausted = match state.events.checked_add(1) {
            Some(count) => { state.events = count; count > self.configuration.max_events }
            None => true,
        };
        if exhausted {
            state.damage("Process descendant event-work limit exceeded");
            if !matches!(event.kind, provider::FORK | provider::AUTH_EXEC | provider::EXEC | provider::EXIT) {
                self.changed.notify_all();
                return false;
            }
            // Quarantine keeps fixed-storage termination work after the
            // activation budget. OPEN/SYNC cannot repair history or proof.
        }
        let pending_before = event.kind == provider::AUTH_EXEC
            && state.tokens.iter().any(|entry| entry.request == event.request);
        if event.kind == provider::FAILURE {
            state.damage(match event.status { 1 => "provider lost an event", 2 => "provider closed unexpectedly",
                _ => "provider reported invalid observation state" });
        } else if event.kind == provider::SYNC {
            match state.marker.as_mut() {
                Some(marker) if marker.request == event.request && marker.observed
                    && marker.opened && marker.submitted && !marker.synchronized => marker.synchronized = true,
                _ => state.damage("provider synchronization does not match the active marker"),
            }
        } else {
            let reserved_marker = event.kind == provider::OPEN && event.actor == self.configuration.supervisor
                && event.file == self.configuration.marker && event.flags == 1;
            if state.sequence.is_none() {
                if reserved_marker && state.marker.as_ref().is_some_and(|marker| marker.purpose == Purpose::Admission) {
                    state.sequence = Some(event.sequence);
                } else if event.kind == provider::OPEN && event.actor == self.configuration.supervisor {
                    // There are no command children before the admission marker.
                    // Provider setup can open its own non-marker resources.
                    self.changed.notify_all();
                    return state.damaged.is_none();
                } else { state.damage("lifecycle event preceded the initial Process marker"); }
            } else if state.sequence.and_then(|value| value.checked_add(1)) != Some(event.sequence) {
                state.damage("Process descendant event sequence is incomplete");
                // Retain identifiable late children for termination. A damaged
                // history stays damaged even when later events are coherent.
                state.sequence = Some(event.sequence);
            } else { state.sequence = Some(event.sequence); }
            self.lifecycle(&mut state, &event, reserved_marker);
        }
        // A retained AUTH must receive its one reply through reply_one, even
        // in quarantine. Unretained requests return zero for provider denial.
        let accepted = if event.kind == provider::AUTH_EXEC {
            !pending_before && state.tokens.iter().any(|entry| entry.request == event.request)
        } else { state.damaged.is_none() };
        self.changed.notify_all();
        accepted
    }
    fn lifecycle(&self, state: &mut State, event: &Event<'_>, reserved_marker: bool) {
        if !event.actor.credentials_match(self.configuration.supervisor) {
            state.damage("Process descendant credentials changed");
            return;
        }
        match event.kind {
            provider::FORK => {
                let initial = event.actor == self.configuration.supervisor && state.launch_started && state.initial.is_none();
                let parent_known = state.known(event.actor).is_some_and(|index| state.tokens[index].confirmed);
                if (!initial && (!parent_known || !state.launch_resolved)) || !event.target.credentials_match(self.configuration.supervisor)
                    || event.target.pid() == self.configuration.supervisor.pid()
                    || state.tokens.iter().any(|entry| entry.token.pid() == event.target.pid()) {
                    state.damage("Process fork has an unknown parent or duplicate child");
                    return;
                }
                if state.processes == self.configuration.max_processes
                    || state.tokens.len() == self.configuration.max_tokens as usize {
                    state.damage("Process descendant ledger limit exceeded");
                    return;
                }
                state.tokens.push(OwnedToken { token: event.target, image: None, request: 0,
                    decision: Decision::None, confirmed: true, alive: true, signal_epoch: 0 });
                state.processes += 1;
                if initial { state.initial = Some(event.target); state.initial_latest = Some(event.target); }
                state.completion_barrier = false;
            }
            provider::AUTH_EXEC => {
                let Some(actor) = state.known(event.actor) else {
                    state.damage("Process exec authorization has an unknown owner"); return;
                };
                if !state.tokens[actor].confirmed || event.actor == event.target || event.actor.pid() != event.target.pid()
                    || !event.target.credentials_match(self.configuration.supervisor)
                    || event.request <= state.last_auth_request
                    || state.tokens.iter().any(|entry| entry.token.pid() == event.actor.pid() && entry.request != 0) {
                    state.damage("Process exec authorization has an invalid transition"); return;
                }
                state.last_auth_request = event.request;
                let image = self.images.iter().position(|image| image.file == event.file && image.path == event.path
                    && if !state.launch_resolved {
                        image.launch_only && !state.initial_exec_confirmed
                            && state.initial.is_some_and(|initial| initial.pid() == event.actor.pid())
                    } else { !image.launch_only });
                if image.is_none() { state.denied_execution = true; }
                let index = if let Some(index) = state.known(event.target) { index } else {
                    if state.tokens.len() == self.configuration.max_tokens as usize {
                        state.damage("Process execution-token limit exceeded"); return;
                    }
                    state.tokens.push(OwnedToken { token: event.target, image, request: 0,
                        decision: Decision::None, confirmed: false, alive: true, signal_epoch: 0 });
                    state.tokens.len() - 1
                };
                let target = &mut state.tokens[index];
                target.image = image;
                target.request = event.request;
                target.decision = Decision::Pending;
                state.completion_barrier = false;
            }
            provider::EXEC => {
                let Some(index) = state.known(event.actor) else {
                    state.damage("Process exec notification has an unknown execution token"); return;
                };
                let entry = &state.tokens[index];
                if entry.decision != Decision::Allow || entry.image.is_none_or(|index| {
                    self.images[index].file != event.file || self.images[index].path != event.path
                }) {
                    state.damage("Process exec notification has no matching allowed transition"); return;
                }
                let bootstrap_image = entry.image.is_some_and(|image| self.images[image].launch_only);
                state.tokens[index].confirmed = true;
                state.tokens[index].decision = Decision::None;
                state.tokens[index].signal_epoch = 0;
                if state.initial.is_some_and(|initial| initial.pid() == event.actor.pid()) {
                    state.initial_latest = Some(event.actor);
                    if bootstrap_image { state.initial_exec_confirmed = true; }
                }
                state.tokens.retain(|entry| entry.token.pid() != event.actor.pid() || entry.token == event.actor);
                state.completion_barrier = false;
            }
            provider::EXIT => {
                if !state.known(event.actor).is_some_and(|index| state.tokens[index].confirmed) {
                    state.damage("Process exit has an unknown or unconfirmed execution token"); return;
                }
                for entry in &mut state.tokens {
                    if entry.token.pid() == event.actor.pid() { entry.alive = false; }
                }
                state.tokens.retain(|entry| entry.alive || entry.request != 0);
                state.processes -= 1;
                state.completion_barrier = false;
            }
            provider::OPEN => {
                if event.actor != self.configuration.supervisor
                    && !state.known(event.actor).is_some_and(|index| state.tokens[index].confirmed) {
                    state.damage("Process open has an unknown execution token"); return;
                }
                if reserved_marker {
                    match state.marker.as_mut() {
                        Some(marker) if !marker.observed => marker.observed = true,
                        _ => state.damage("Process reserved marker has no pending request"),
                    }
                }
            }
            _ => state.damage("invalid Process lifecycle event"),
        }
    }
    pub fn reply_one(&self, provider: &Provider, validate_registry: impl FnOnce(&Image) -> bool) -> bool {
        let _gate = self.authorization.lock().unwrap();
        let (selected_request, selected_image, eligible) = {
            let state = self.state.lock().unwrap();
            let Some(index) = state.pending() else { return false; };
            let entry = &state.tokens[index];
            (entry.request, entry.image, state.healthy() && !state.frozen && !state.denied_execution && entry.alive)
        };
        let registry_valid = eligible && selected_image.is_some_and(|index| validate_registry(&self.images[index]));
        let (request, allow) = {
            let mut state = self.state.lock().unwrap();
            let Some(index) = state.tokens.iter().position(|entry| entry.request == selected_request
                && entry.decision == Decision::Pending) else {
                state.damage("Process pending authorization changed before its reply"); return false;
            };
            let allow = registry_valid && state.healthy() && !state.frozen && !state.denied_execution
                && state.tokens[index].alive && state.tokens[index].image.is_some();
            if eligible && !registry_valid { state.denied_execution = true; }
            state.tokens[index].decision = if allow { Decision::Allow } else { Decision::Deny };
            (state.tokens[index].request, allow)
        };
        // Callbacks may run here. The authorization gate is held, but the event
        // state mutex is not. Freeze cannot pass an allow reply in progress.
        let result = provider.respond(request, allow);
        let mut state = self.state.lock().unwrap();
        if let Err(reason) = result { state.damage(reason); }
        if let Some(entry) = state.tokens.iter_mut().find(|entry| entry.request == request) {
            entry.request = 0;
        } else { state.damage("Process authorization receipt lost its pending request"); }
        state.tokens.retain(|entry| entry.alive || entry.request != 0);
        self.changed.notify_all();
        true
    }
    pub fn next_termination(&self) -> Option<Token> {
        let mut state = self.state.lock().unwrap();
        if !state.frozen { return None; }
        let epoch = state.signal_epoch;
        let entry = state.tokens.iter_mut().find(|entry| entry.alive && entry.signal_epoch < epoch)?;
        entry.signal_epoch = epoch;
        Some(entry.token)
    }
    pub fn begin_marker(&self, purpose: Purpose) -> Result<u64, &'static str> {
        let mut state = self.state.lock().unwrap();
        let allowed = match purpose {
            Purpose::Admission => !state.admitted && !state.launch_started,
            Purpose::FailedLaunch => state.launch_started && !state.launch_resolved,
            Purpose::Completion => state.launch_resolved && state.frozen,
        };
        if !state.healthy() || !state.empty() || !allowed || state.marker.is_some() {
            return Err("Process marker cannot establish the requested boundary");
        }
        let request = state.marker_request.checked_add(1).ok_or("Process marker request overflow")?;
        state.marker_request = request;
        state.marker = Some(Marker { request, purpose, opened: false, observed: false,
            submitted: false, synchronized: false });
        Ok(request)
    }
    pub fn marker_opened(&self, request: u64, file: FileIdentity) -> Result<(), &'static str> {
        let mut state = self.state.lock().unwrap();
        if file != self.configuration.marker || !state.healthy() {
            state.damage("Process marker descriptor identity changed");
            return Err("Process marker descriptor identity changed");
        }
        match state.marker.as_mut() {
            Some(marker) if marker.request == request && !marker.opened => { marker.opened = true; Ok(()) }
            _ => { state.damage("Process marker open has no matching request"); Err("Process marker open has no matching request") }
        }
    }
    pub fn marker_submitted(&self, request: u64) -> Result<(), &'static str> {
        let mut state = self.state.lock().unwrap();
        match state.marker.as_mut() {
            Some(marker) if marker.request == request && marker.opened && !marker.submitted => {
                marker.submitted = true; Ok(())
            }
            _ => { state.damage("Process marker synchronization was not requested once");
                Err("Process marker synchronization was not requested once") }
        }
    }
    pub fn marker_ready(&self, request: u64) -> bool {
        let state = self.state.lock().unwrap();
        state.healthy() && state.empty() && state.marker.as_ref().is_some_and(|marker| {
            marker.request == request && marker.opened && marker.observed && marker.submitted && marker.synchronized
        })
    }
    // Call only after successful closure of the temporary marker descriptor.
    pub fn finish_marker(&self, request: u64) -> Result<Purpose, &'static str> {
        let mut state = self.state.lock().unwrap();
        if !state.healthy() || !state.empty() || !state.marker.as_ref().is_some_and(|marker| {
            marker.request == request && marker.opened && marker.observed && marker.submitted && marker.synchronized
        }) { return Err("Process marker boundary is incomplete"); }
        let purpose = state.marker.take().unwrap().purpose;
        match purpose {
            Purpose::Admission => state.admitted = true,
            Purpose::FailedLaunch => state.launch_resolved = true,
            Purpose::Completion => state.completion_barrier = true,
        }
        Ok(purpose)
    }
    pub fn begin_close(&self, child_reaped: bool, channels_closed: bool, sinks_joined: bool) -> Result<(), &'static str> {
        let mut state = self.state.lock().unwrap();
        if !state.healthy() || !state.frozen || !state.launch_resolved || !state.empty()
            || !state.completion_barrier || state.marker.is_some() || !child_reaped || !channels_closed || !sinks_joined {
            return Err("Process descendant cleanup proof is incomplete");
        }
        state.closing = true;
        Ok(())
    }
    pub fn finish_close(&self) -> Result<(), &'static str> {
        let mut state = self.state.lock().unwrap();
        if !state.active || !state.closing || state.damaged.is_some() || !state.empty() {
            return Err("Process descendant client closure did not preserve cleanup proof");
        }
        state.active = false;
        state.closing = false;
        Ok(())
    }
}
