use crate::context::{host, Fault};
use std::{
    cell::Cell,
    ffi::CString,
    os::raw::{c_char, c_int},
    sync::{
        atomic::{AtomicBool, Ordering},
        Arc, Mutex,
    },
    time::Instant,
};
#[cfg(feature = "metal")]
use std::{ffi::CStr, os::raw::c_void};

pub const ADD: u32 = 1;
pub const SUB: u32 = 2;
pub const MUL: u32 = 3;
pub const DIV: u32 = 4;
pub const SQRT: u32 = 5;
pub const RELU: u32 = 6;
pub const BROADCAST: u32 = 7;
pub const TRANSPOSE: u32 = 8;
pub const SUM: u32 = 9;
pub const MEAN: u32 = 10;
pub const MATMUL: u32 = 11;
pub const FILL: u32 = 12;
pub const RELU_GRAD: u32 = 13;
pub const SQRT_GRAD: u32 = 14;
pub const METAL_PROFILE: &str = "metal-fp32";

#[repr(C)]
#[derive(Clone, Copy, Default)]
pub struct Operation {
    pub kind: u32,
    pub count: u32,
    pub input_count: u32,
    pub region: u32,
    pub a_rank: u32,
    pub b_rank: u32,
    pub rank: u32,
    pub scalar: u32,
    pub a: [u32; 8],
    pub b: [u32; 8],
    pub output: [u32; 8],
    pub axes: [u32; 8],
}

pub struct Control<'a> {
    cancelled: &'a bool,
    cancellation: &'a AtomicBool,
    deadline: Option<Instant>,
    observe_cli: bool,
    polls: &'a Cell<u64>,
    after: Option<u64>,
}
impl<'a> Control<'a> {
    pub fn new(
        cancelled: &'a bool,
        cancellation: &'a AtomicBool,
        deadline: Option<Instant>,
        observe_cli: bool,
        polls: &'a Cell<u64>,
        after: Option<u64>,
    ) -> Self {
        Self {
            cancelled,
            cancellation,
            deadline,
            observe_cli,
            polls,
            after,
        }
    }
    pub fn check(&self) -> Result<(), Fault> {
        let polls = self.polls.get().saturating_add(1);
        self.polls.set(polls);
        if *self.cancelled
            || self.cancellation.load(Ordering::Relaxed)
            || self
                .deadline
                .is_some_and(|deadline| Instant::now() >= deadline)
            || self.observe_cli && crate::cli::cancelled()
            || self.after.is_some_and(|limit| polls >= limit)
        {
            Err(Fault::Host(
                "runtime.cancelled",
                "execution cancelled: context canceled".into(),
            ))
        } else {
            Ok(())
        }
    }
}

#[repr(C)]
#[derive(Clone, Copy)]
pub struct DriverError {
    pub kind: u32,
    pub message: [c_char; 512],
}
impl Default for DriverError {
    fn default() -> Self {
        Self {
            kind: 0,
            message: [0; 512],
        }
    }
}
#[repr(C)]
#[derive(Clone, Copy)]
pub struct Identity {
    pub name: [c_char; 256],
    pub profile: [c_char; 64],
    pub registry_id: [c_char; 64],
    pub system: [c_char; 128],
    pub kernel_sha256: [c_char; 65],
}
impl Default for Identity {
    fn default() -> Self {
        Self {
            name: [0; 256],
            profile: [0; 64],
            registry_id: [0; 64],
            system: [0; 128],
            kernel_sha256: [0; 65],
        }
    }
}
fn fixed_text(chars: &[c_char]) -> Result<String, Fault> {
    let length = chars
        .iter()
        .position(|c| *c == 0)
        .ok_or_else(|| host("device driver string is not terminated"))?;
    String::from_utf8(chars[..length].iter().map(|c| *c as u8).collect())
        .map_err(|_| host("device driver string is not UTF-8"))
}
impl Identity {
    pub fn profile(&self) -> Result<String, Fault> {
        fixed_text(&self.profile)
    }
    fn validate(&self) -> Result<(), Fault> {
        for value in [
            &self.name[..],
            &self.profile[..],
            &self.registry_id[..],
            &self.system[..],
            &self.kernel_sha256[..],
        ] {
            fixed_text(value)?;
        }
        Ok(())
    }
}
#[repr(C)]
pub struct Receipt {
    pub event: u32,
    pub allocation: u64,
    pub bytes: u64,
    pub live_bytes: u64,
    pub device: *const c_char,
    pub backend: *const Identity,
}
pub type Observer = unsafe extern "C" fn(usize, *const Receipt);
#[repr(C)]
#[derive(Clone, Copy)]
pub struct Driver {
    pub user: usize,
    pub open: Option<
        unsafe extern "C" fn(
            usize,
            *const c_char,
            *mut usize,
            *mut Identity,
            *mut DriverError,
        ) -> c_int,
    >,
    pub close: Option<unsafe extern "C" fn(usize, usize, *mut DriverError) -> c_int>,
    pub allocate: Option<
        unsafe extern "C" fn(usize, usize, u64, *mut usize, *mut u64, *mut DriverError) -> c_int,
    >,
    pub upload:
        Option<unsafe extern "C" fn(usize, usize, *const f32, u64, *mut DriverError) -> c_int>,
    pub download:
        Option<unsafe extern "C" fn(usize, usize, *mut f32, u64, *mut DriverError) -> c_int>,
    pub release: Option<unsafe extern "C" fn(usize, usize, *mut DriverError) -> c_int>,
    pub supports:
        Option<unsafe extern "C" fn(usize, usize, *const Operation, *mut DriverError) -> c_int>,
    pub compute: Option<
        unsafe extern "C" fn(
            usize,
            usize,
            *const Operation,
            *const usize,
            usize,
            usize,
            usize,
            *mut u32,
            *mut DriverError,
        ) -> c_int,
    >,
}
#[derive(Clone, Copy, Default)]
struct Hooks {
    driver: Option<Driver>,
    observer: Option<Observer>,
    observer_user: usize,
}
static HOOKS: Mutex<Hooks> = Mutex::new(Hooks {
    driver: None,
    observer: None,
    observer_user: 0,
});
#[no_mangle]
pub unsafe extern "C" fn lo_device_install_driver(driver: *const Driver) -> c_int {
    let mut hooks = HOOKS.lock().unwrap();
    if driver.is_null() {
        hooks.driver = None;
        return 1;
    }
    let driver = *driver;
    if driver.open.is_none()
        || driver.close.is_none()
        || driver.allocate.is_none()
        || driver.upload.is_none()
        || driver.download.is_none()
        || driver.release.is_none()
        || driver.supports.is_none()
        || driver.compute.is_none()
    {
        return 0;
    }
    hooks.driver = Some(driver);
    1
}
#[no_mangle]
pub extern "C" fn lo_device_set_observer(user: usize, observer: Option<Observer>) {
    let mut hooks = HOOKS.lock().unwrap();
    hooks.observer_user = user;
    hooks.observer = observer;
}
#[derive(Clone)]
pub struct Config {
    pub grant: String,
    hooks: Hooks,
}
impl Config {
    pub fn new(grant: &str) -> Result<Self, String> {
        if !matches!(grant, "" | "cuda:0" | "cuda-sim:0" | "metal:0") {
            return Err(format!("invalid device grant {grant:?}"));
        }
        Ok(Self {
            grant: grant.into(),
            hooks: *HOOKS.lock().unwrap(),
        })
    }
    pub fn emit(
        &self,
        event: u32,
        id: u64,
        location: &str,
        bytes: u64,
        live: u64,
        identity: Option<&Identity>,
    ) {
        if let Some(observer) = self.hooks.observer {
            let location = CString::new(location).unwrap();
            let receipt = Receipt {
                event,
                allocation: id,
                bytes,
                live_bytes: live,
                device: location.as_ptr(),
                backend: identity.map_or(std::ptr::null(), |v| v),
            };
            unsafe {
                observer(self.hooks.observer_user, &receipt);
            }
        }
    }
}
impl Default for Config {
    fn default() -> Self {
        Self::new("").unwrap()
    }
}

pub fn fault(kind: &'static str, message: impl Into<String>) -> Fault {
    Fault::Numeric(kind, format!("{kind}: {}", message.into()))
}
fn driver_fault(error: &DriverError) -> Fault {
    let kind = match error.kind {
        1 => "Device.Unavailable",
        2 => "Device.AllocationFailed",
        3 => "Device.TransferFailed",
        4 => "Device.UnsupportedOperation",
        5 => "Device.UnsupportedProfile",
        6 => "Device.LibraryFailed",
        7 => "Device.Lost",
        8 => "Tensor.DeviceMismatch",
        9 => "Device.ExecutionFailed",
        _ => {
            return host(format!(
                "device driver returned unknown failure kind {}",
                error.kind
            ))
        }
    };
    match fixed_text(&error.message) {
        Ok(message) => fault(kind, message),
        Err(error) => error,
    }
}
fn checked(status: c_int, error: &DriverError) -> Result<(), Fault> {
    match status {
        1 => Ok(()),
        0 => Err(driver_fault(error)),
        _ => Err(host("device driver returned invalid completion status")),
    }
}

pub enum Backing {
    Host(Vec<f32>),
    Device(Arc<Allocation>),
}
pub struct Allocation {
    logical: u64,
    reserved: u64,
    kind: Mutex<Option<Buffer>>,
}
enum Buffer {
    Simulation(Vec<f32>),
    Injected(Driver, usize),
    #[cfg(feature = "metal")]
    Metal(usize),
}
impl Allocation {
    pub fn logical_bytes(&self) -> u64 {
        self.logical
    }
    pub fn reserved_bytes(&self) -> u64 {
        self.reserved
    }
    pub fn close(&self) -> Result<(), Fault> {
        let buffer = self.kind.lock().unwrap().take();
        match buffer {
            Some(Buffer::Injected(driver, handle)) => {
                let mut error = DriverError::default();
                checked(
                    unsafe { driver.release.unwrap()(driver.user, handle, &mut error) },
                    &error,
                )
            }
            #[cfg(feature = "metal")]
            Some(Buffer::Metal(handle)) => {
                unsafe { metal::lo_metal_release(handle as *mut c_void) };
                Ok(())
            }
            _ => Ok(()),
        }
    }
    pub fn upload(&self, values: &[f32]) -> Result<(), Fault> {
        let mut guard = self.kind.lock().unwrap();
        match guard
            .as_mut()
            .ok_or_else(|| host("upload to released device allocation"))?
        {
            Buffer::Simulation(buffer) => {
                buffer.copy_from_slice(values);
                Ok(())
            }
            Buffer::Injected(driver, handle) => {
                let mut error = DriverError::default();
                checked(
                    unsafe {
                        driver.upload.unwrap()(
                            driver.user,
                            *handle,
                            values.as_ptr(),
                            4 * values.len() as u64,
                            &mut error,
                        )
                    },
                    &error,
                )
            }
            #[cfg(feature = "metal")]
            Buffer::Metal(handle) => {
                let mut error = metal::Error::default();
                metal::checked(
                    unsafe {
                        metal::lo_metal_upload(
                            *handle as *mut c_void,
                            values.as_ptr() as *const c_void,
                            4 * values.len() as u64,
                            &mut error,
                        )
                    },
                    &error,
                )
            }
        }
    }
    pub fn download(&self, values: &mut [f32]) -> Result<(), Fault> {
        let mut guard = self.kind.lock().unwrap();
        match guard
            .as_mut()
            .ok_or_else(|| host("download from released device allocation"))?
        {
            Buffer::Simulation(buffer) => {
                values.copy_from_slice(buffer);
                Ok(())
            }
            Buffer::Injected(driver, handle) => {
                let mut error = DriverError::default();
                checked(
                    unsafe {
                        driver.download.unwrap()(
                            driver.user,
                            *handle,
                            values.as_mut_ptr(),
                            4 * values.len() as u64,
                            &mut error,
                        )
                    },
                    &error,
                )
            }
            #[cfg(feature = "metal")]
            Buffer::Metal(handle) => {
                let mut error = metal::Error::default();
                metal::checked(
                    unsafe {
                        metal::lo_metal_download(
                            *handle as *mut c_void,
                            values.as_mut_ptr() as *mut c_void,
                            4 * values.len() as u64,
                            &mut error,
                        )
                    },
                    &error,
                )
            }
        }
    }
    fn handle(&self) -> Result<usize, Fault> {
        match self
            .kind
            .lock()
            .unwrap()
            .as_ref()
            .ok_or_else(|| host("use of released device allocation"))?
        {
            Buffer::Simulation(_) => Err(host("simulation buffer has no compute handle")),
            Buffer::Injected(_, handle) => Ok(*handle),
            #[cfg(feature = "metal")]
            Buffer::Metal(handle) => Ok(*handle),
        }
    }
}

enum Session {
    Simulation,
    Injected(Driver, usize),
    #[cfg(feature = "metal")]
    Metal(usize),
}
pub struct Backend {
    session: Option<Session>,
    pub identity: Identity,
}
impl Backend {
    pub fn open(config: &Config, location: &str) -> Result<Self, Fault> {
        if let Some(driver) = config.hooks.driver {
            let selector = CString::new(location).unwrap();
            let mut handle = 0;
            let mut identity = Identity::default();
            let mut error = DriverError::default();
            checked(
                unsafe {
                    driver.open.unwrap()(
                        driver.user,
                        selector.as_ptr(),
                        &mut handle,
                        &mut identity,
                        &mut error,
                    )
                },
                &error,
            )?;
            if handle == 0 {
                return Err(fault(
                    "Device.Unavailable",
                    "backend factory returned no device",
                ));
            }
            if let Err(failure) = identity.validate() {
                let mut close_error = DriverError::default();
                let close = checked(
                    unsafe { driver.close.unwrap()(driver.user, handle, &mut close_error) },
                    &close_error,
                );
                return Err(crate::context::combine_cleanup(Some(failure), close.err()).unwrap());
            }
            return Ok(Self {
                session: Some(Session::Injected(driver, handle)),
                identity,
            });
        }
        if location == "cuda-sim:0" {
            let mut identity = Identity::default();
            set_text(&mut identity.name, "software simulation");
            set_text(&mut identity.profile, "cuda-sim-fp32");
            return Ok(Self {
                session: Some(Session::Simulation),
                identity,
            });
        }
        #[cfg(feature = "metal")]
        if location == "metal:0" {
            return metal::open();
        }
        Err(fault(
            "Device.Unavailable",
            format!("backend {location} is not present in this build"),
        ))
    }
    pub fn allocate(&self, bytes: u64) -> Result<Arc<Allocation>, Fault> {
        let (reserved, kind) = match self
            .session
            .as_ref()
            .ok_or_else(|| host("allocate after device session close"))?
        {
            Session::Simulation => (bytes, Buffer::Simulation(vec![0.0; (bytes / 4) as usize])),
            Session::Injected(driver, session) => {
                let mut buffer = 0;
                let mut reserved = 0;
                let mut error = DriverError::default();
                checked(
                    unsafe {
                        driver.allocate.unwrap()(
                            driver.user,
                            *session,
                            bytes,
                            &mut buffer,
                            &mut reserved,
                            &mut error,
                        )
                    },
                    &error,
                )?;
                if buffer == 0 {
                    return Err(fault(
                        "Device.AllocationFailed",
                        "device allocation returned no buffer",
                    ));
                }
                (reserved, Buffer::Injected(*driver, buffer))
            }
            #[cfg(feature = "metal")]
            Session::Metal(session) => {
                let mut error = metal::Error::default();
                let buffer =
                    unsafe { metal::lo_metal_allocate(*session as *mut c_void, bytes, &mut error) };
                if buffer.is_null() {
                    return Err(metal::failure(&error));
                }
                (
                    unsafe { metal::lo_metal_reserved(buffer) },
                    Buffer::Metal(buffer as usize),
                )
            }
        };
        Ok(Arc::new(Allocation {
            logical: bytes,
            reserved,
            kind: Mutex::new(Some(kind)),
        }))
    }
    pub fn supports(&self, operation: &Operation) -> Result<(), Fault> {
        if !(ADD..=SQRT_GRAD).contains(&operation.kind)
            || operation.a_rank > 8
            || operation.b_rank > 8
            || operation.rank > 8
        {
            return Err(fault(
                "Device.UnsupportedOperation",
                "invalid compute descriptor",
            ));
        }
        match self
            .session
            .as_ref()
            .ok_or_else(|| host("compute after device session close"))?
        {
            Session::Simulation => Err(fault(
                "Device.UnsupportedOperation",
                "simulation backend implements transfers only",
            )),
            Session::Injected(driver, session) => {
                let mut error = DriverError::default();
                checked(
                    unsafe {
                        driver.supports.unwrap()(driver.user, *session, operation, &mut error)
                    },
                    &error,
                )
            }
            #[cfg(feature = "metal")]
            Session::Metal(_) => Ok(()),
        }
    }
    pub fn compute(
        &self,
        operation: &Operation,
        inputs: &[Arc<Allocation>],
        output: &Allocation,
        status: &Allocation,
    ) -> Result<(), Fault> {
        let handles = inputs
            .iter()
            .map(|a| a.handle())
            .collect::<Result<Vec<_>, _>>()?;
        let output = output.handle()?;
        let status = status.handle()?;
        let mut numeric = u32::MAX;
        match self
            .session
            .as_ref()
            .ok_or_else(|| host("compute after device session close"))?
        {
            Session::Simulation => {
                return Err(fault(
                    "Device.UnsupportedOperation",
                    "simulation backend implements transfers only",
                ))
            }
            Session::Injected(driver, session) => {
                let mut error = DriverError::default();
                checked(
                    unsafe {
                        driver.compute.unwrap()(
                            driver.user,
                            *session,
                            operation,
                            handles.as_ptr(),
                            handles.len(),
                            output,
                            status,
                            &mut numeric,
                            &mut error,
                        )
                    },
                    &error,
                )?;
            }
            #[cfg(feature = "metal")]
            Session::Metal(session) => {
                let mut error = metal::Error::default();
                metal::checked(
                    unsafe {
                        metal::lo_metal_compute(
                            *session as *mut c_void,
                            operation,
                            handles.first().copied().unwrap_or(0) as *mut c_void,
                            handles.get(1).copied().unwrap_or(0) as *mut c_void,
                            output as *mut c_void,
                            status as *mut c_void,
                            &mut numeric,
                            &mut error,
                        )
                    },
                    &error,
                )?;
            }
        }
        if numeric == u32::MAX {
            Ok(())
        } else {
            let kind = match numeric & 7 {
                1 => "Numeric.NonFinite",
                2 => "Numeric.DivideByZero",
                3 => "Numeric.Domain",
                4 => "Autodiff.UndefinedDerivative",
                _ => {
                    return Err(fault(
                        "Device.ExecutionFailed",
                        format!("invalid Metal numeric status {numeric}"),
                    ))
                }
            };
            Err(Fault::Numeric(kind,format!("expected valid finite metal-fp32 arithmetic, got invalid value at row-major element {}",numeric>>3)))
        }
    }
    pub fn close(&mut self) -> Result<(), Fault> {
        match self.session.take() {
            Some(Session::Injected(driver, session)) => {
                let mut error = DriverError::default();
                checked(
                    unsafe { driver.close.unwrap()(driver.user, session, &mut error) },
                    &error,
                )
            }
            #[cfg(feature = "metal")]
            Some(Session::Metal(session)) => {
                unsafe { metal::lo_metal_close(session as *mut c_void) };
                Ok(())
            }
            _ => Ok(()),
        }
    }
}
fn set_text(target: &mut [c_char], text: &str) {
    assert!(text.len() < target.len());
    for (a, b) in target.iter_mut().zip(text.bytes()) {
        *a = b as c_char;
    }
}

#[cfg(feature = "metal")]
mod metal {
    use super::*;
    #[repr(C)]
    pub struct Error {
        code: c_int,
        message: [c_char; 512],
    }
    impl Default for Error {
        fn default() -> Self {
            Self {
                code: 0,
                message: [0; 512],
            }
        }
    }
    extern "C" {
        pub fn lo_native_metal_library(length: *mut usize) -> *const c_void;
        pub fn lo_native_metal_kernel_sha256() -> *const c_char;
        pub fn lo_metal_open(
            library: *const c_void,
            length: usize,
            error: *mut Error,
        ) -> *mut c_void;
        pub fn lo_metal_close(session: *mut c_void);
        pub fn lo_metal_name(session: *mut c_void) -> *const c_char;
        pub fn lo_metal_system(session: *mut c_void) -> *const c_char;
        pub fn lo_metal_registry(session: *mut c_void) -> u64;
        pub fn lo_metal_allocate(
            session: *mut c_void,
            bytes: u64,
            error: *mut Error,
        ) -> *mut c_void;
        pub fn lo_metal_reserved(buffer: *mut c_void) -> u64;
        pub fn lo_metal_upload(
            buffer: *mut c_void,
            values: *const c_void,
            bytes: u64,
            error: *mut Error,
        ) -> c_int;
        pub fn lo_metal_download(
            buffer: *mut c_void,
            values: *mut c_void,
            bytes: u64,
            error: *mut Error,
        ) -> c_int;
        pub fn lo_metal_release(buffer: *mut c_void);
        pub fn lo_metal_compute(
            session: *mut c_void,
            operation: *const Operation,
            a: *mut c_void,
            b: *mut c_void,
            output: *mut c_void,
            status: *mut c_void,
            numeric: *mut u32,
            error: *mut Error,
        ) -> c_int;
    }
    pub fn failure(error: &Error) -> Fault {
        let kind = match error.code {
            1 => "Device.Unavailable",
            2 => "Device.UnsupportedProfile",
            3 => "Device.LibraryFailed",
            4 => "Device.AllocationFailed",
            5 => "Device.TransferFailed",
            6 => "Device.Lost",
            7 => "Device.ExecutionFailed",
            _ => {
                return host(format!(
                    "Metal adapter returned unknown error code {}",
                    error.code
                ))
            }
        };
        match fixed_text(&error.message) {
            Ok(message) => fault(kind, format!("metal:0: {message}")),
            Err(error) => error,
        }
    }
    pub fn checked(status: c_int, error: &Error) -> Result<(), Fault> {
        if status == 0 {
            Err(failure(error))
        } else {
            Ok(())
        }
    }
    pub fn open() -> Result<Backend, Fault> {
        let mut length = 0;
        let library = unsafe { lo_native_metal_library(&mut length) };
        let mut error = Error::default();
        let session = unsafe { lo_metal_open(library, length, &mut error) };
        if session.is_null() {
            return Err(failure(&error));
        }
        let mut identity = Identity::default();
        unsafe {
            set_text(
                &mut identity.name,
                &CStr::from_ptr(lo_metal_name(session)).to_string_lossy(),
            );
            set_text(
                &mut identity.system,
                &CStr::from_ptr(lo_metal_system(session)).to_string_lossy(),
            );
            set_text(
                &mut identity.registry_id,
                &format!("{:x}", lo_metal_registry(session)),
            );
            set_text(
                &mut identity.kernel_sha256,
                &CStr::from_ptr(lo_native_metal_kernel_sha256()).to_string_lossy(),
            );
        }
        set_text(&mut identity.profile, METAL_PROFILE);
        Ok(Backend {
            session: Some(Session::Metal(session as usize)),
            identity,
        })
    }
}
