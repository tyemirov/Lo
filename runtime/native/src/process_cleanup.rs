use super::*;
use std::os::unix::fs::FileTypeExt;

const MAX_MUTATION_ENTRIES: usize = 100000;
const MAX_MUTATION_DEPTH: usize = 128;
const MAX_UNIX_ENDPOINT: usize = 103;

fn denied(subject: &str) -> Fault {
    Fault::Host(
        "runtime.capability-denied",
        format!("Process.run {subject:?} has no host capability"),
    )
}

pub(super) fn validate(context: &mut Context, scope: &policy::Scope) -> Result<(), Fault> {
    let roots: Vec<_> = scope.writes.iter().chain(&scope.prunes).cloned().collect();
    for root in &roots {
        context.check_cancel()?;
        if !canonical(root) {
            return Err(denied("cleanup root"));
        }
        let directory =
            open_read(root).map_err(|error| host(format!("open Process cleanup root: {error}")))?;
        let observed = directory
            .metadata()
            .map_err(|error| host(format!("observe Process cleanup root: {error}")));
        let closed = close_file(directory)
            .map_err(|error| host(format!("close Process cleanup root: {error}")));
        match observed {
            Ok(info) => {
                closed?;
                if !info.is_dir() {
                    return Err(host("Process cleanup root requires a directory"));
                }
            }
            Err(fault) => return Err(combine_cleanup(Some(fault), closed.err()).unwrap()),
        }
    }
    if !scope.writes.is_empty() {
        let selected: Vec<_> = scope
            .writes
            .iter()
            .map(|root| (root.clone(), crate::files::MutationAccess::Data))
            .chain(
                scope
                    .prunes
                    .iter()
                    .map(|root| (root.clone(), crate::files::MutationAccess::Namespace)),
            )
            .collect();
        crate::files::validate_namespace(
            context,
            &selected,
            MAX_MUTATION_ENTRIES,
            MAX_MUTATION_DEPTH,
        )?;
    }
    for endpoint in &scope.endpoints {
        context.check_cancel()?;
        if endpoint.len() > MAX_UNIX_ENDPOINT || !canonical(endpoint) {
            return Err(denied("Unix endpoint"));
        }
        let information = fs::symlink_metadata(endpoint)
            .map_err(|error| host(format!("observe Process Unix endpoint: {error}")))?;
        if !information.file_type().is_socket() {
            return Err(denied("Unix endpoint requires a socket"));
        }
    }
    Ok(())
}
