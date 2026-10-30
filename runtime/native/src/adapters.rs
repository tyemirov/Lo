use crate::{context::*, value::*};
use serde_json::Value as Json;
use std::{
    collections::BTreeMap,
    sync::{Arc, Mutex},
};

#[derive(Clone)]
pub enum AdapterValue {
    Host(crate::host_invocation::Value),
    File(crate::files::Value),
    Process(crate::process::Value),
    Http(crate::http::Value),
}
macro_rules! delegate {
    ($self:ident, $method:ident) => {
        match $self {
            Self::Host(v) => v.$method(),
            Self::File(v) => v.$method(),
            Self::Process(v) => v.$method(),
            Self::Http(v) => v.$method(),
        }
    };
}
impl AdapterValue {
    pub fn weight(&self) -> u64 {
        delegate!(self, weight)
    }
    pub fn json(&self) -> Json {
        delegate!(self, json)
    }
    pub fn canonical_json(&self) -> String {
        delegate!(self, canonical_json)
    }
    pub fn fields(&self) -> Option<&BTreeMap<String, Handle>> {
        delegate!(self, fields)
    }
    pub fn snapshot(&self) -> Self {
        match self {
            Self::Host(v) => Self::Host(v.snapshot()),
            Self::File(v) => Self::File(v.snapshot()),
            Self::Process(v) => Self::Process(v.snapshot()),
            Self::Http(v) => Self::Http(v.snapshot()),
        }
    }
}

pub struct Session {
    pub initial_process_context: Result<Handle, String>,
    pub initial_host_invocation: Result<Handle, String>,
    pub program: Arc<Program>,
    pub steps: u64,
    pub limits: crate::value_resources::Limits,
    pub processes: Arc<crate::process::Processes>,
    pub http: Arc<crate::http::Http>,
    pub files: Mutex<crate::files::Files>,
    pub devices: crate::device::Config,
    pub serving: bool,
}
impl Session {
    pub fn empty(program: Arc<Program>, steps: u64) -> Arc<Self> {
        let captured = crate::host_invocation::capture_inputs(&[]);
        Arc::new(Self {
            initial_process_context: captured.process,
            initial_host_invocation: captured.host,
            program,
            steps,
            limits: crate::value_resources::Limits::default(),
            processes: Arc::new(crate::process::empty()),
            http: Arc::new(crate::http::Http::empty()),
            files: Mutex::new(crate::files::empty()),
            devices: crate::device::Config::default(),
            serving: false,
        })
    }
    pub fn close(&self) -> Result<(), Fault> {
        let mut failures = vec![];
        if let Err(f) = self.http.shutdown() {
            failures.push(f);
        }
        if let Err(message) = self.processes.close() {
            failures.push(host(message));
        }
        if failures.is_empty() {
            if let Err(message) = self.files.lock().unwrap().close() {
                failures.push(host(message));
            }
        }
        if failures.is_empty() {
            return Ok(());
        }
        let fault = failures
            .into_iter()
            .fold(None, |primary, failure| {
                combine_cleanup(primary, Some(failure))
            })
            .unwrap();
        let message = format!("close runtime resources \"program\": {}", fault.message());
        let details = match fault {
            Fault::Boundary { mut details, .. } => {
                details.cleanup_messages.clear();
                *details
            }
            _ => BoundaryDetails::default(),
        };
        Err(host(message).with_details(details))
    }
    pub fn wait_retained(&self) -> Result<(), Fault> {
        let http = self.http.wait_retained();
        let process = self.processes.wait_retained().map_err(host);
        let files = self.files.lock().unwrap().close().map_err(host);
        combine_cleanup(combine_cleanup(http.err(), process.err()), files.err()).map_or(Ok(()), Err)
    }
}

pub fn commit(context: &mut Context, site: &Site, target: &Handle) -> Outcome {
    match &target.data {
        Data::Adapter(AdapterValue::File(_)) => crate::files::commit(context, site, target),
        Data::Adapter(AdapterValue::Process(_)) => crate::process::commit(context, site, target),
        Data::Adapter(AdapterValue::Http(_)) => {
            let http = context.session.http.clone();
            http.commit(context, site, target)
        }
        _ => Err(host("unsupported native commit value")),
    }
}
