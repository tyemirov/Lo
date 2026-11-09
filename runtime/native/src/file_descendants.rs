use super::*;

pub(super) fn changed(site: &Site, input: &Handle, path: &Path) -> Fault {
    list_failure(
        site,
        input,
        "File.DirectoryChanged",
        path,
        "observed directory or ancestor changed",
    )
}

pub(super) fn directory_observation(site: &Site, value: &Handle) -> Result<Observation, Fault> {
    match &value.data {
        Data::Adapter(crate::adapters::AdapterValue::File(Value::Directory {
            observation: Some(observation),
            ..
        })) => Ok(observation.clone()),
        _ => Err(denied(&site.operation, Path::new("[unobserved directory]"))),
    }
}

pub(super) fn entry_observation(site: &Site, value: &Handle) -> Result<Observation, Fault> {
    match &value.data {
        Data::Adapter(crate::adapters::AdapterValue::File(Value::Entry {
            observation: Some(observation),
            ..
        })) => Ok(observation.clone()),
        _ => Err(denied(&site.operation, Path::new("[unobserved entry]"))),
    }
}

pub(super) fn check_lease(context: &Context, site: &Site, observed: &Observation) -> Result<(), Fault> {
    let denied = || denied(&site.operation, &observed.directory.path);
    if observed.session != context.files.session {
        return Err(denied());
    }
    let anchor = context
        .files
        .observation_directories
        .get(&observed.lease)
        .ok_or_else(denied)?;
    if !context.files.observed.contains_key(anchor)
        || anchor.path != observed.directory.anchor
        || !observed.directory.witnesses.starts_with(&anchor.witnesses)
        || anchor.recursive != observed.directory.recursive
        || (!anchor.recursive && anchor.path != observed.directory.path)
    {
        return Err(denied());
    }
    Ok(())
}

pub(super) fn retained(
    context: &mut Context,
    site: &Site,
    observed: &Observation,
    input: &Handle,
) -> Result<File, Fault> {
    check_lease(context, site, observed)?;
    context.reserve_file(
        observed.directory.witnesses.len() as u64,
        observed.directory.weight(),
    )?;
    witnesses::reopen(&observed.directory.witnesses).map_err(|failure| match failure {
        witnesses::Failure::Changed | witnesses::Failure::Missing(_) => {
            changed(site, input, &observed.directory.path)
        }
        witnesses::Failure::IO(error) => list_failure(
            site,
            input,
            if matches!(
                site.operation.as_str(),
                "File.Directory.refresh" | "File.Directory.child"
            ) {
                "File.DirectoryFailed"
            } else {
                "File.ListFailed"
            },
            &observed.directory.path,
            &system_error(&error),
        ),
        witnesses::Failure::Close(error) => host(system_error(&error)),
    })
}

pub(super) fn acquire(
    context: &mut Context,
    site: &Site,
    root: &Handle,
) -> Result<(File, std::sync::Arc<Directory>, bool, u64), Fault> {
    context.check_cancel()?;
    if root.typ == atom("File.Directory") {
        let observed = directory_observation(site, root)?;
        let file = retained(context, site, &observed, root)?;
        return Ok((file, observed.directory, false, observed.lease));
    }
    let (file, observed) = observation_grant(context, site, root)?;
    let fresh = !context.files.observed.contains_key(&observed);
    if fresh
        && context.values.limits.file_directories != 0
        && context.files.observed.len() as u64 >= context.values.limits.file_directories
    {
        let error = limit("retained file directories");
        return Err(match close_file(file) {
            Ok(()) => error,
            Err(cleanup) => cleanup_failure(error, cleanup),
        });
    }
    let lease = if fresh {
        let lease = context.files.next_observation_lease;
        context.files.next_observation_lease += 1;
        lease
    } else {
        context.files.observed_leases[&observed]
    };
    Ok((file, std::sync::Arc::new(observed), fresh, lease))
}

pub(super) fn recheck(
    context: &mut Context,
    site: &Site,
    root: &Handle,
    observed: &std::sync::Arc<Directory>,
    lease: u64,
) -> Result<(), Fault> {
    let (check, current) = if root.typ != atom("File.Path") {
        let prior = Observation {
            session: context.files.session,
            lease,
            directory: observed.clone(),
            identity: observed.identity,
        };
        (retained(context, site, &prior, root)?, None)
    } else {
        context.reserve_file(observed.witnesses.len() as u64, observed.weight())?;
        let (check, current) = observation_grant(context, site, root)?;
        (check, Some(current))
    };
    close_file(check).map_err(|error| host(system_error(&error)))?;
    if current.as_ref().is_some_and(|current| current != observed.as_ref()) {
        return Err(denied(&site.operation, &observed.path));
    }
    Ok(())
}

fn finish(file: File, result: Outcome) -> Outcome {
    match (result, close_file(file)) {
        (Err(primary), Err(cleanup)) => Err(cleanup_failure(primary, cleanup)),
        (Err(primary), Ok(())) => Err(primary),
        (Ok(_), Err(cleanup)) => Err(host(system_error(&cleanup))),
        (Ok(value), Ok(())) => Ok(value),
    }
}

pub(crate) fn child(
    context: &mut Context,
    site: &Site,
    directory: &Handle,
    entry: &Handle,
) -> Outcome {
    let parent = directory_observation(site, directory)?;
    let leaf = entry_observation(site, entry)?;
    check_lease(context, site, &parent)?;
    if !parent.directory.recursive
        || parent.session != leaf.session
        || parent.lease != leaf.lease
        || parent.directory != leaf.directory
    {
        return Err(denied(&site.operation, &parent.directory.path));
    }
    if entry.fields()["kind"].string() != "directory" {
        return Err(list_failure(
            site,
            entry,
            "File.DirectoryFailed",
            &parent.directory.path,
            "entry is not a directory",
        ));
    }
    let parent_file = retained(context, site, &parent, directory)?;
    let result = (|| {
        let name = entry.fields()["name"].string();
        let path = parent.directory.path.join(name);
        let info = lstat_at(&parent_file, name).map_err(|error| {
            list_failure(
                site,
                entry,
                if error.kind() == io::ErrorKind::NotFound {
                    "File.DirectoryAbsent"
                } else {
                    "File.DirectoryFailed"
                },
                &path,
                &system_error(&error),
            )
        })?;
        if !info.is_dir() || info.identity != leaf.identity {
            return Err(changed(site, entry, &path));
        }
        let file =
            witnesses::open_at(&parent_file, std::ffi::OsStr::new(name)).map_err(|error| {
                match error.raw_os_error() {
                    Some(libc::ENOENT) => list_failure(
                        site,
                        entry,
                        "File.DirectoryAbsent",
                        &path,
                        &system_error(&error),
                    ),
                    Some(libc::ENOTDIR | libc::ELOOP) => changed(site, entry, &path),
                    _ => list_failure(
                        site,
                        entry,
                        "File.DirectoryFailed",
                        &path,
                        &system_error(&error),
                    ),
                }
            })?;
        let mut chain = (*parent.directory.witnesses).clone();
        chain.push(witnesses::Witness {
            name: name.into(),
            identity: info.identity,
        });
        let observed = std::sync::Arc::new(Directory {
            path,
            identity: info.identity,
            anchor: parent.directory.anchor.clone(),
            witnesses: std::sync::Arc::new(chain),
            recursive: true,
        });
        let result = directory_value(context, site, entry, &file, &observed, parent.lease, 0);
        finish(file, result)
    })();
    finish(parent_file, result)
}

fn observe_name(
    context: &mut Context,
    site: &Site,
    input: &Handle,
    parent: &Observation,
    name: &str,
    optional: bool,
) -> Outcome {
    let file = retained(context, site, parent, input)?;
    let result = (|| {
        context.reserve_file(1, 0)?;
        let observed = match observed_at(context, &file, &parent.directory.path, name, 2) {
            Ok(info) => Ok(Some(info)),
            Err(observer::Failure::IO(error))
                if error.kind() == io::ErrorKind::NotFound && optional =>
            {
                Ok(None)
            }
            Err(observer::Failure::IO(error)) if error.kind() == io::ErrorKind::NotFound => {
                Err(list_failure(
                    site,
                    input,
                    "File.EntryAbsent",
                    &parent.directory.path.join(name),
                    &system_error(&error),
                ))
            }
            Err(error) => Err(observation_failure(
                error,
                site,
                input,
                "File.ListFailed",
                &parent.directory.path.join(name),
            )),
        };
        let checked = retained(context, site, parent, input)
            .and_then(|checked| close_file(checked).map_err(|error| host(system_error(&error))));
        let info = postcheck(observed, checked)?;
        let value = info
            .as_ref()
            .map(|info| {
                entry_value(
                    context,
                    site,
                    &parent.directory,
                    parent.lease,
                    name,
                    info,
                    0,
                    "File.ListFailed",
                )
            })
            .transpose()?;
        if optional {
            Ok(new(
                Type::List(Box::new(atom("File.Entry"))),
                Data::List(value.into_iter().collect()),
            ))
        } else {
            Ok(value.expect("strict entry observation"))
        }
    })();
    finish(file, result)
}

pub(crate) fn named_entry(
    context: &mut Context,
    site: &Site,
    directory: &Handle,
    name: &Handle,
) -> Outcome {
    let parent = directory_observation(site, directory)?;
    check_lease(context, site, &parent)?;
    let name = name.string();
    if name.is_empty() || name == "." || name == ".." || name.contains('/') || name.contains('\0') {
        return Err(list_failure(
            site,
            directory,
            "File.InvalidName",
            &parent.directory.path,
            "entry name must be one basename",
        ));
    }
    observe_name(context, site, directory, &parent, name, false)
}

pub(crate) fn observe(context: &mut Context, site: &Site, entry: &Handle) -> Outcome {
    let parent = entry_observation(site, entry)?;
    observe_name(
        context,
        site,
        entry,
        &parent,
        entry.fields()["name"].string(),
        true,
    )
}

pub(super) fn removal_authority(
    context: &Context,
    site: &Site,
    observed: &Observation,
    input: &Handle,
) -> Result<(), Fault> {
    let path = &observed.directory.path;
    let exact = context
        .files
        .removes
        .iter()
        .any(|grant| &grant.path == path);
    let (roots, granted_path) = if exact {
        (&context.files.removes, path)
    } else {
        (&context.files.remove_trees, &observed.directory.anchor)
    };
    let granted_path = granted_path.to_string_lossy();
    let (file, grant) = exact_grant(
        roots,
        &context.files.base,
        "File.remove",
        &granted_path,
        site,
        input,
    )?;
    close_file(file).map_err(|error| host(system_error(&error)))?;
    if !observed.directory.witnesses.starts_with(&grant.witnesses) {
        return Err(denied("File.remove", path));
    }
    Ok(())
}
