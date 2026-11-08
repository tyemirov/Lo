# File System Observations

F032 adds file identity, allocation, fresh time, and volume measurements.
These operations use the compiled native runtime.
Application flows select paths, ages, and report text.

## File Metadata

`File.Entry` and `File.Directory` include three additional `Int` fields.

| Field | Meaning |
| --- | --- |
| `device` | Native filesystem device identifier. |
| `inode` | Native inode identifier on that device. |
| `allocated_bytes` | Native allocation block count multiplied by 512. |

One native metadata observation supplies identity, allocation, owner, timestamps, and the existing file fields.
The allocation block count must be nonnegative.
Allocation arithmetic uses the full Lo integer range.
A sparse file can have less allocation than its logical size.
An empty file can have zero allocation.

Field projections are pure.
Copied observations retain their original values.
Automatic snapshots contain metadata without executable authority.
Public identity fields cannot grant access or restore an expired observation.
Removal still uses its private native identity.
`File.RemoveReceipt.observed_bytes` still reports logical bytes.

## Fresh Time

```lo
flow Main() -> Int! {
    return Clock.now_unix_ns()
}
return Main()
```

`Clock.now_unix_ns()` is a host read.
Each call measures Unix time in nanoseconds.
Negative timestamps are valid.
The result must fit a signed 64-bit integer.
The operation does not change the time in `Host.Invocation.current()`.
That invocation value retains its initial capture.

A clock failure or an out-of-range timestamp produces `Clock.Failed`.
Cancellation and quota failures remain terminal.
A pure callback cannot call this operation.

## Volume Usage

```lo
flow Main() -> Int! {
    usage = File.System.usage(File.Path.new("fixture"))
    return usage.free_bytes
}
return Main()
```

`File.System.usage(root)` measures the volume that contains an existing directory.
The operation requires an exact `--list-root` grant for that directory.
A parent grant does not authorize a child path.
Read authority does not supply this grant.
The runtime rejects symlink traversal and changed directory identity.
It measures through the authorized descriptor and closes temporary descriptors before completion.

The immutable `File.System.Usage` result has three `Int` fields.

| Field | Native calculation |
| --- | --- |
| `total_bytes` | `f_blocks * f_frsize` |
| `used_bytes` | `(f_blocks - f_bfree) * f_frsize` |
| `free_bytes` | `f_bavail * f_frsize` |

`free_bytes` reports space available to an ordinary caller.
Reserved space can make the sum of used and available space less than total space.
The runtime rejects zero fragment size and inconsistent block counts.
Multiplication uses the full Lo integer range.
Each call produces a fresh measurement.

A missing directory produces `File.DirectoryAbsent`.
A volume measurement failure produces `File.System.UsageFailed`.
Missing authority, cancellation, quota failures, and descriptor close failures remain terminal.
The error contains the selected root.
Observed free-space change does not prove how much space a cleanup operation reclaimed.
Other processes and filesystem behavior can change that measurement.

## Trusted Observation Providers

`lo_host_observer.h` declares synchronous clock and volume providers for native embeddings.
Install each provider before session evaluation on the context owner thread.
Keep the provider and its userdata valid until the session and its requests finish.
The runtime supplies a native observation before the provider runs.
Volume providers cannot bypass directory authority.
Providers can cancel the context but cannot reenter or destroy it.

A positive provider result supplies an observation error number.
A negative result produces a terminal host failure.
Invalid provider metadata also produces a terminal host failure.
The existing file metadata provider includes the allocation block count.
That provider cannot replace native device or inode identity.

## Acceptance

Public compiled fixtures compare file identity and allocation with independent native observations.
They cover empty files, sparse files, exact directory authority, and logical removal receipts.
Native ABI fixtures supply advancing clocks, large block counts, observation failures, and cancellation.
The same ABI scenarios run on Darwin arm64 and Linux arm64 under QEMU.
All fixtures and outputs remain inside Lo.
