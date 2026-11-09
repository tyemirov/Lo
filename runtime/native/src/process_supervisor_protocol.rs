use super::{super::{io, resources::Inventory}, Deadlines, DescendantLimits, Decoder, Encoder, Kind, Packet};

pub(in crate::process) struct Setup {
    pub descendants: DescendantLimits,
    pub limits: io::Limits,
    pub deadlines: Deadlines,
    pub launcher: String,
    pub directory: String,
    pub embedded: bool,
    pub forward: bool,
    pub resources: usize,
    pub resource_storage: u64,
    pub storage: u64,
    pub configuration: usize,
    pub command: usize,
    pub input: usize,
}
impl Setup {
    pub fn encode(&self) -> Result<Vec<u8>, &'static str> {
        let mut out = Encoder::new();
        out.u32(self.descendants.max_processes)?; out.u32(self.descendants.max_tokens)?;
        out.u64(self.descendants.max_events)?; out.u32(self.descendants.kernel_processes)?;
        out.u64(self.limits.timeout_ms)?; out.u64(self.limits.cleanup_ms)?;
        out.u64(self.limits.output_bytes as u64)?; out.u32(self.limits.forward_streams)?;
        out.u64(self.deadlines.command)?;
        out.text(&self.launcher)?; out.text(&self.directory)?;
        out.u32(u32::from(self.embedded))?; out.u32(u32::from(self.forward))?;
        for value in [self.resources as u64, self.resource_storage, self.storage,
            self.configuration as u64, self.command as u64, self.input as u64] { out.u64(value)?; }
        Ok(out.finish())
    }
    pub fn decode(packet: Packet) -> Result<Self, &'static str> {
        if packet.kind != Kind::Initialize || packet.descriptor.is_some() { return Err("Process supervisor initialization is missing"); }
        let mut input = Decoder::new(&packet.body);
        let descendants = DescendantLimits { max_processes: input.u32()?, max_tokens: input.u32()?,
            max_events: input.u64()?, kernel_processes: input.u32()? };
        descendants.storage()?;
        let limits = io::Limits { timeout_ms: input.u64()?, cleanup_ms: input.u64()?,
            output_bytes: usize::try_from(input.u64()?).map_err(|_| "Process output limit overflow")?, forward_streams: input.u32()? }
            .validate().map_err(|_| "Process supervisor I/O limits are invalid")?;
        let deadlines = Deadlines { command: input.u64()?, cleanup_ms: limits.cleanup_ms };
        let launcher = input.text()?.to_owned(); let directory = input.text()?.to_owned();
        if !super::super::policy_path(&launcher) || !super::super::policy_path(&directory) { return Err("Process supervisor paths are invalid"); }
        let embedded = flag(input.u32()?)?; let forward = flag(input.u32()?)?;
        if forward && limits.forward_streams != 3 { return Err("Process forwarding authority is absent"); }
        let resources = count(input.u64()?)?; let resource_storage = input.u64()?; let storage = input.u64()?;
        let configuration = count(input.u64()?)?; let command = count(input.u64()?)?; let stdin = count(input.u64()?)?;
        input.finish()?;
        if configuration > 65552 || command > super::super::MAX_COMMAND_BODY + 16
            || configuration == 0 || command == 0 || deadlines.command == 0
            || storage < reservation(descendants, configuration, command, stdin, resource_storage)? {
            return Err("Process supervisor storage admission differs");
        }
        Ok(Self { descendants, limits, deadlines, launcher, directory, embedded, forward, resources,
            resource_storage, storage, configuration, command, input: stdin })
    }
}
fn flag(value: u32) -> Result<bool, &'static str> { match value { 0=>Ok(false),1=>Ok(true),_=>Err("Process supervisor flag is invalid") } }
fn count(value: u64) -> Result<usize, &'static str> { usize::try_from(value).ok().filter(|n| *n <= isize::MAX as usize).ok_or("Process supervisor size overflow") }
pub(in crate::process) fn reservation(limits: DescendantLimits, configuration: usize, command: usize, input: usize,
    inventory: u64) -> Result<u64, &'static str> {
    // Configuration and command frames exist in the caller, wire owner, and
    // supervisor. Stdin already has a charged immutable value in the caller;
    // its parent and supervisor copies require two further reservations.
    let payload = (configuration as u64).checked_add(command as u64).and_then(|n| n.checked_mul(3))
        .and_then(|n| (input as u64).checked_mul(2).and_then(|stdin| n.checked_add(stdin)))
        .ok_or("Process supervisor input storage overflow")?;
    limits.storage()?.checked_add(super::Owner::provider_storage(limits.max_tokens)?)
        .and_then(|n| n.checked_add(super::Channel::storage() * 2))
        .and_then(|n| n.checked_add(payload)).and_then(|n| n.checked_add(inventory))
        .and_then(|n| n.checked_add(65536)).ok_or("Process supervisor storage overflow")
}
pub(super) fn empty(packet: Packet, kind: Kind) -> Result<(), &'static str> {
    if packet.kind == kind && packet.body.is_empty() && packet.descriptor.is_none() { Ok(()) }
    else { Err("Process supervisor boundary message differs") }
}
pub(super) fn result_body(result: &io::NativeResult, unavailable: bool) -> Result<Vec<u8>, &'static str> {
    let mut output = Encoder::new(); output.u32(u32::from(unavailable))?;
    for value in [result.stage, result.class_id, result.started, result.cleanup_complete,
        result.termination_kind, result.termination_value] { output.u32(value as u32)?; }
    for value in [result.stdin_written, result.stdout_size, result.stderr_size] { output.u64(value as u64)?; }
    output.u32(result.cleanup_failures)?;
    // An unknown owner remains in the supervisor. No process-local pointer is serialized.
    Ok(output.finish())
}
pub(super) fn result(packet: Packet) -> Result<(io::NativeResult, bool), &'static str> {
    if packet.kind != Kind::Receipt || packet.descriptor.is_some() { return Err("Process supervisor receipt is absent"); }
    let mut input = Decoder::new(&packet.body);
    let unavailable = flag(input.u32()?)?;
    let result = io::NativeResult { stage: input.u32()? as i32, class_id: input.u32()? as i32,
        started: input.u32()? as i32, cleanup_complete: input.u32()? as i32,
        termination_kind: input.u32()? as i32, termination_value: input.u32()? as i32,
        stdin_written: count(input.u64()?)?, stdout_size: count(input.u64()?)?, stderr_size: count(input.u64()?)?,
        cleanup_failures: input.u32()?, ownership: 0 };
    input.finish()?;
    if !(0..=7).contains(&result.stage) || !(0..=5).contains(&result.class_id)
        || !(0..=1).contains(&result.started) || !(0..=1).contains(&result.cleanup_complete)
        || !(0..=2).contains(&result.termination_kind) || result.cleanup_failures & !7 != 0
        || (unavailable && (result.stage != 1 || result.class_id != 2 || result.started != 0
            || result.cleanup_complete != 1 || result.termination_kind != 0 || result.stdin_written != 0
            || result.stdout_size != 0 || result.stderr_size != 0 || result.cleanup_failures != 0)) {
        return Err("Process supervisor receipt fields are invalid");
    }
    Ok((result, unavailable))
}
pub(super) fn startup_result(packet: Packet) -> Result<(io::NativeResult, bool), &'static str> {
    let received = result(packet)?;
    let result = &received.0;
    // Before Launch, a clean receipt can only report admission failure.
    // It cannot establish command success or any command execution evidence.
    if result.stage != 1 || result.class_id != 2 || result.started != 0
        || result.cleanup_complete != 1 || result.termination_kind != 0
        || result.termination_value != 0 || result.stdin_written != 0
        || result.stdout_size != 0 || result.stderr_size != 0 || result.cleanup_failures != 0 {
        return Err("Process supervisor startup receipt fields are invalid");
    }
    Ok(received)
}
pub(super) fn chunk(body: &[u8], target: &mut Vec<u8>, expected: usize) -> Result<(), &'static str> {
    let mut input = Decoder::new(body); let offset = count(input.u64()?)?;
    let bytes = input.bytes(body.len().checked_sub(8).ok_or("Process supervisor chunk is short")?)?;
    if offset != target.len() || bytes.is_empty() || bytes.len() > io::CHUNK_BYTES || bytes.len() > expected - target.len() {
        return Err("Process supervisor chunk range differs");
    }
    target.extend_from_slice(bytes); input.finish()
}
pub(super) fn buffer(length: usize) -> Result<Vec<u8>, &'static str> {
    let mut value = Vec::new(); value.try_reserve_exact(length).map_err(|_| "Process supervisor input storage is unavailable")?; Ok(value)
}
pub(super) fn inventory(setup: &Setup) -> Result<Inventory, &'static str> { Inventory::receiving(setup.resources, setup.resource_storage) }
