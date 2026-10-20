use super::*;
use std::{
    net::ToSocketAddrs,
    sync::{
        atomic::{AtomicBool, AtomicUsize, Ordering},
        mpsc::{self, RecvTimeoutError, SyncSender, TrySendError},
        Mutex,
    },
    thread,
    time::{Duration, Instant},
};
struct Job {
    host: String,
    port: u16,
    reply: SyncSender<Result<Vec<IpAddr>, String>>,
}
struct State {
    sender: Option<SyncSender<Job>>,
    workers: Vec<thread::JoinHandle<()>>,
}
pub struct Resolver {
    state: Mutex<State>,
    stopping: AtomicBool,
    pending: Arc<AtomicUsize>,
}
impl Resolver {
    pub fn new() -> Self {
        Self {
            state: Mutex::new(State {
                sender: None,
                workers: vec![],
            }),
            stopping: AtomicBool::new(false),
            pending: Arc::new(AtomicUsize::new(0)),
        }
    }
    pub fn resolve(
        &self,
        context: &Context,
        host: &str,
        port: u16,
        deadline: Instant,
    ) -> Result<Vec<IpAddr>, Fault> {
        let sender = {
            let mut state = self.state.lock().unwrap();
            if self.stopping.load(Ordering::Acquire) {
                return Err(Fault::RawCancellation);
            }
            if state.sender.is_none() {
                let (sender, receiver) = mpsc::sync_channel::<Job>(2);
                let receiver = Arc::new(Mutex::new(receiver));
                for _ in 0..2 {
                    let receiver = receiver.clone();
                    let pending = self.pending.clone();
                    let worker = thread::Builder::new()
                        .name("lo-http-resolver".into())
                        .spawn(move || loop {
                            let job = match receiver.lock().unwrap().recv() {
                                Ok(job) => job,
                                Err(_) => break,
                            };
                            let result = (job.host.as_str(), job.port)
                                .to_socket_addrs()
                                .map(|items| items.map(|a| a.ip()).collect())
                                .map_err(|error| error.to_string());
                            let _ = job.reply.send(result);
                            pending.fetch_sub(1, Ordering::AcqRel);
                        })
                        .map_err(|error| host_fault(error.to_string()))?;
                    state.workers.push(worker);
                }
                state.sender = Some(sender);
            }
            state.sender.as_ref().unwrap().clone()
        };
        let (reply, receiver) = mpsc::sync_channel(1);
        let mut job = Job {
            host: host.into(),
            port,
            reply,
        };
        self.pending.fetch_add(1, Ordering::AcqRel);
        loop {
            if let Err(fault) = check(context, deadline, &self.stopping) {
                self.pending.fetch_sub(1, Ordering::AcqRel);
                return Err(fault);
            }
            match sender.try_send(job) {
                Ok(()) => break,
                Err(TrySendError::Full(returned)) => {
                    job = returned;
                    thread::sleep(Duration::from_millis(10))
                }
                Err(TrySendError::Disconnected(_)) => {
                    self.pending.fetch_sub(1, Ordering::AcqRel);
                    return Err(host_fault("HTTP resolver stopped"));
                }
            }
        }
        loop {
            check(context, deadline, &self.stopping)?;
            match receiver.recv_timeout(Duration::from_millis(50)) {
                Ok(result) => return result.map_err(host_fault),
                Err(RecvTimeoutError::Timeout) => continue,
                Err(_) => return Err(host_fault("HTTP resolver worker failed")),
            }
        }
    }
    pub fn close(&self) -> Result<(), Fault> {
        self.stopping.store(true, Ordering::Release);
        self.state.lock().unwrap().sender.take();
        let deadline = Instant::now() + Duration::from_secs(5);
        loop {
            if self
                .state
                .lock()
                .unwrap()
                .workers
                .iter()
                .all(thread::JoinHandle::is_finished)
            {
                return self.wait_retained();
            }
            if Instant::now() >= deadline {
                return Err(
                    host_fault("HTTP resolver cleanup did not finish before its deadline")
                        .with_details(BoundaryDetails {
                            cleanup_errors: vec!["HTTP resolver worker remains retained".into()],
                            ..Default::default()
                        }),
                );
            }
            thread::sleep(Duration::from_millis(10));
        }
    }
    pub fn wait_retained(&self) -> Result<(), Fault> {
        let workers = std::mem::take(&mut self.state.lock().unwrap().workers);
        for worker in workers {
            if worker.join().is_err() {
                return Err(host_fault("HTTP resolver worker failed"));
            }
        }
        Ok(())
    }
}
fn host_fault(message: impl Into<String>) -> Fault {
    host(message)
}
fn check(context: &Context, deadline: Instant, stopping: &AtomicBool) -> Result<(), Fault> {
    context.check_cancel()?;
    if stopping.load(Ordering::Acquire) {
        return Err(Fault::RawCancellation);
    }
    if Instant::now() >= deadline {
        return Err(Fault::Host(
            "runtime.cancelled",
            "context deadline exceeded".into(),
        ));
    }
    Ok(())
}
