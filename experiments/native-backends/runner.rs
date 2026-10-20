use std::{env, fs, path::{Path, PathBuf}, process::{Command, Stdio}};

fn copy(source: &Path, target: &Path) -> Result<(), Box<dyn std::error::Error>> {
    fs::create_dir_all(target.parent().ok_or("missing target parent")?)?;
    fs::copy(source, target)?;
    Ok(())
}

fn copy_tree(source: &Path, target: &Path) -> Result<(), Box<dyn std::error::Error>> {
    fs::create_dir_all(target)?;
    for entry in fs::read_dir(source)? {
        let entry = entry?;
        if entry.file_type()?.is_dir() { copy_tree(&entry.path(), &target.join(entry.file_name()))?; }
        else { copy(&entry.path(), &target.join(entry.file_name()))?; }
    }
    Ok(())
}

fn main() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = env::args().collect();
    if args.len() != 3 { return Err("expected executable and sysroot".into()); }
    let executable = Path::new(&args[1]);
    let sysroot = Path::new(&args[2]);
    let bytes = fs::read(executable)?;
    if bytes.starts_with(&[0xcf, 0xfa, 0xed, 0xfe]) {
        let status = Command::new(executable).stdin(Stdio::inherit()).status()?;
        std::process::exit(status.code().ok_or("native process terminated by signal")?);
    }
    if !bytes.starts_with(b"\x7fELF") { return Err("unsupported executable format".into()); }
    let bundle = env::current_exe()?.parent().ok_or("missing runner parent")?.parent().ok_or("missing bundle parent")?.join("emulation");
    let work = env::current_dir()?;
    let name = executable.file_name().ok_or("missing executable filename")?.to_str().ok_or("invalid filename")?;
    let guest = work.join(format!("guest-{name}"));
    if guest.exists() { fs::remove_dir_all(&guest)?; }
    fs::create_dir_all(guest.join("dev"))?;
    copy(executable, &guest.join("probe"))?;
    copy(&bundle.join("guest-init"), &guest.join("init"))?;
    copy_tree(&sysroot.join("lib"), &guest.join("lib"))?;
    let initrd: PathBuf = work.join(format!("initrd-{name}.cpio"));
    let archive = Command::new(bundle.join("tar"))
        .args(["--format=newc", "-cf"]).arg(&initrd).arg("-C").arg(&guest).arg(".")
        .stdin(Stdio::inherit()).output()?;
    if !archive.status.success() { return Err(format!("create guest archive: {}", String::from_utf8_lossy(&archive.stderr)).into()); }
    let output = Command::new(bundle.join("qemu-system-aarch64"))
        .env("DYLD_LIBRARY_PATH", bundle.join("lib"))
        .args(["-machine", "virt", "-accel", "tcg", "-cpu", "cortex-a72", "-m", "256", "-display", "none", "-monitor", "none", "-serial", "stdio", "-no-reboot", "-nodefaults", "-nic", "none", "-kernel"])
        .arg(bundle.join("kernel")).arg("-initrd").arg(&initrd)
        .args(["-append", "console=ttyAMA0 rdinit=/init panic=-1 loglevel=3"])
        .stdin(Stdio::inherit()).output()?;
    fs::write(work.join(format!("console-{name}.log")), &output.stdout)?;
    fs::write(work.join(format!("emulator-{name}.log")), &output.stderr)?;
    if !output.status.success() { return Err(format!("guest runner exited {}: {}", output.status, String::from_utf8_lossy(&output.stderr)).into()); }
    let console = String::from_utf8(output.stdout)?.replace("\r\n", "\n");
    let program = console.split_once("BEGIN_LO_NATIVE_PROBE\n").ok_or("guest start marker missing")?.1;
    let (stdout, status) = program.split_once("END_LO_NATIVE_PROBE:").ok_or("guest completion marker missing")?;
    let code: i32 = status.lines().next().ok_or("guest exit code missing")?.parse()?;
    print!("{stdout}");
    std::process::exit(code);
}
