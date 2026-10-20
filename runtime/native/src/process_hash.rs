use crate::context::{host, Fault};
use ring::digest::{Context, SHA256};
use std::{fs::File, os::unix::fs::FileExt};

pub fn digest(file: &File) -> Result<[u8; 32], String> {
    digest_with_cancel(file, || Ok(())).map_err(|fault| fault.message())
}

pub fn digest_with_cancel(
    file: &File,
    mut check: impl FnMut() -> Result<(), Fault>,
) -> Result<[u8; 32], Fault> {
    let mut digest = Context::new(&SHA256);
    let mut buffer = [0u8; 32768];
    let mut offset = 0usize;
    loop {
        check()?;
        let size = buffer.len().min((64 << 20) - offset + 1);
        let observed = file.read_at(&mut buffer[..size], offset as u64);
        check()?;
        let count = observed.map_err(|error| host(error.to_string()))?;
        if count > (64 << 20) - offset {
            return Err(host("executable exceeds registry byte bound"));
        }
        if count == 0 {
            return Ok(digest.finish().as_ref().try_into().unwrap());
        }
        offset += count;
        digest.update(&buffer[..count]);
    }
}

pub fn hex(bytes: &[u8]) -> String {
    bytes.iter().map(|byte| format!("{byte:02x}")).collect()
}
