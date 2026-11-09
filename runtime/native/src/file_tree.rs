use super::*;

#[derive(Clone)]
struct Row {
    relative: String,
    name: String,
    parent: Option<usize>,
    info: FileInfo,
}

#[derive(Clone)]
pub struct Tree {
    pub parent: Observation,
    rows: Vec<Row>,
    weight: u64,
}
impl Tree {
    pub fn weight(&self) -> u64 {
        self.weight
    }

    fn directory(&self, index: Option<usize>) -> Observation {
        let mut observation = self.parent.clone();
        if index.is_none() {
            return observation;
        }
        let mut directory = observation.directory.as_ref().clone();
        let mut ancestors = vec![];
        let mut current = index;
        while let Some(index) = current {
            ancestors.push(index);
            current = self.rows[index].parent;
        }
        let mut chain = (*directory.witnesses).clone();
        for index in ancestors.into_iter().rev() {
            let row = &self.rows[index];
            directory.path.push(&row.name);
            directory.identity = row.info.identity;
            chain.push(witnesses::Witness { name: row.name.clone().into(), identity: row.info.identity });
        }
        directory.witnesses = std::sync::Arc::new(chain);
        observation.identity = directory.identity;
        observation.directory = std::sync::Arc::new(directory);
        observation
    }
}

fn finish<T>(file: File, result: Result<T, Fault>) -> Result<T, Fault> {
    match (result, close_file(file)) {
        (Err(primary), Err(cleanup)) => Err(cleanup_failure(primary, cleanup)),
        (Err(primary), Ok(())) => Err(primary),
        (Ok(_), Err(cleanup)) => Err(host(system_error(&cleanup))),
        (Ok(value), Ok(())) => Ok(value),
    }
}

fn check(context: &mut Context, site: &Site, observation: &Observation, input: &Handle) -> Result<(), Fault> {
    let file = descendants::retained(context, site, observation, input)?;
    finish(file, Ok(()))
}

fn read(context: &mut Context, site: &Site, parent: &Observation, name: &str, input: &Handle, phase: u32, kind: &str) -> Result<FileInfo, Fault> {
    let file = descendants::retained(context, site, parent, input)?;
    let result = (|| {
        context.reserve_file(1, 0)?;
        let result = observed_at(context, &file, &parent.directory.path, name, phase)
            .map_err(|error| {
                let absent = matches!(&error, observer::Failure::IO(error) if error.kind() == io::ErrorKind::NotFound);
                let kind = if absent && site.operation == "File.remove_tree" { "File.EntryChanged" } else if absent && kind == "File.DirectoryFailed" { "File.DirectoryAbsent" } else { kind };
                observation_failure(error, site, input, kind, &parent.directory.path.join(name))
            });
        postcheck(result, check(context, site, parent, input))
    })();
    finish(file, result)
}

fn names(context: &mut Context, site: &Site, directory: &Observation, input: &Handle, weight: u64, maximum: usize) -> Result<Vec<String>, Fault> {
    let file = descendants::retained(context, site, directory, input)?;
    let result = (|| {
        let mut stream = DirectoryStream::open(&file).map_err(|error| {
            let primary = list_failure(site, input, "File.ListFailed", &directory.directory.path, &error.primary.to_string());
            error.fault(primary)
        })?;
        let result = (|| {
            let mut names = BTreeSet::new();
            let mut bytes = weight;
            loop {
                context.reserve_file(1, bytes)?;
                let Some(name) = stream.next().map_err(|error| list_failure(site, input, "File.ListFailed", &directory.directory.path, &error.to_string()))? else { break };
                if names.len() >= maximum { return Err(limit("tree entries")); }
                let name = name.to_str().ok_or_else(|| list_failure(site, input, "File.InvalidName", &directory.directory.path, "invalid UTF-8 filename"))?;
                bytes += 64 + name.len() as u64;
                context.reserve_file(0, bytes)?;
                if !names.insert(name.to_owned()) { return Err(list_failure(site, input, "File.ListFailed", &directory.directory.path, "duplicate directory entry")); }
            }
            Ok(names.into_iter().collect())
        })();
        let result = match (result, stream.close()) {
            (Err(primary), Err(cleanup)) => Err(cleanup_failure(primary, cleanup)),
            (Err(primary), Ok(())) => Err(primary),
            (Ok(_), Err(cleanup)) => Err(host(system_error(&cleanup))),
            (Ok(value), Ok(())) => Ok(value),
        };
        postcheck(result, check(context, site, directory, input))
    })();
    finish(file, result)
}

fn guard(context: &Context, args: &[Handle], fixed: usize) -> Result<Guard, Fault> {
    let Some(last) = args.last() else { return Err(host("missing tree predicate")); };
    let Data::Flow(flow) = last.data else { return Err(host("invalid tree predicate")); };
    let predicate = context.program.flows.get(flow).ok_or_else(|| host("unknown tree predicate"))?.identity.clone();
    Ok(Guard { flow, predicate, contexts: args[fixed..args.len()-1].to_vec() })
}

fn invoke(context: &mut Context, site: &Site, guard: &Guard, value: Handle) -> Result<bool, Fault> {
    context.charge(&value)?;
    let mut args = guard.contexts.clone();
    args.push(value);
    let result = unsafe { crate::exports::invoke(context, guard.flow, &args, site.id) }?;
    match result.data { Data::Bool(value) => Ok(value), _ => Err(host("invalid tree predicate result")) }
}

fn row_value(context: &mut Context, site: &Site, tree: &Tree, row: &Row, weight: u64) -> Outcome {
    let parent = tree.directory(row.parent);
    let entry = entry_value(context, site, &parent.directory, parent.lease, &row.name, &row.info, weight, "File.ListFailed")?;
    Ok(record([("relative".into(), text(&row.relative)), ("entry".into(), crate::value::snapshot(&entry))]))
}

fn walk(context: &mut Context, site: &Site, parent: Observation, root: &Row, input: &Handle, descend: Option<&Guard>, mut progress: Option<(&Tree, &Handle, &mut Progress)>) -> Result<(Tree, Handle, bool), Fault> {
    let indexes: BTreeMap<&str, usize> = progress.as_ref().map(|(tree, _, _)| tree.rows.iter().enumerate().map(|(index, row)| (row.relative.as_str(), index)).collect()).unwrap_or_default();
    let mut tree = Tree { weight: 128 + parent.directory.weight(), parent, rows: vec![] };
    let mut pending = vec![(None, root.name.clone(), ".".to_owned())];
    let mut pending_bytes = 256 + root.name.len() as u64 + 2;
    let mut values = vec![];
    let mut complete = true;
    let mut weight = 64;
    let maximum = if context.values.limits.file_entries == 0 { usize::MAX } else { context.values.limits.file_entries as usize };
    while let Some((parent, name, relative)) = pending.pop() {
        pending_bytes -= 256 + name.capacity() as u64 + 2 * relative.len() as u64;
        if let Some((expected, rows, progress)) = progress.as_mut() {
            progress.index = indexes.get(relative.as_str()).map_or(-1, |index| *index as i64);
            progress.record(context, expected, rows);
        }
        if tree.rows.len() >= maximum { return Err(limit("tree entries")); }
        let observed_parent = tree.directory(parent);
        let info = read(context, site, &observed_parent, &name, input, 2, "File.ListFailed")?;
        if tree.rows.is_empty() && info.identity != root.info.identity { return Err(descendants::changed(site, input, &observed_parent.directory.path.join(&name))); }
        let row = Row { relative, name, parent, info };
        let private_weight = 4 * std::mem::size_of::<Row>() as u64 + row.relative.capacity() as u64 + row.name.capacity() as u64;
        context.reserve_file(0, weight + tree.weight() + private_weight + pending_bytes)?;
        let value = row_value(context, site, &tree, &row, weight + tree.weight())?;
        weight += value.weight;
        let index = tree.rows.len();
        tree.weight += private_weight;
        tree.rows.push(row);
        if tree.rows[index].info.is_dir() {
            let allowed = match descend { Some(guard) => invoke(context, site, guard, value.fields()["entry"].clone())?, None => true };
            if allowed {
                let directory = tree.directory(Some(index));
                let children = names(context, site, &directory, input, weight + tree.weight() + pending_bytes, maximum.saturating_sub(tree.rows.len() + pending.len()))?;
                let mut children_bytes = children.capacity() as u64 * std::mem::size_of::<String>() as u64 + children.iter().map(|name| name.capacity() as u64).sum::<u64>();
                for name in children.into_iter().rev() {
                    children_bytes -= name.capacity() as u64;
                    let relative_bytes = if index == 0 { name.len() } else { tree.rows[index].relative.len() + 1 + name.len() };
                    let addition = 256 + name.capacity() as u64 + 2 * relative_bytes as u64;
                    context.reserve_file(1, weight + tree.weight() + pending_bytes + addition + children_bytes)?;
                    let mut relative = String::with_capacity(relative_bytes);
                    if index != 0 { relative.push_str(&tree.rows[index].relative); relative.push('/'); }
                    relative.push_str(&name);
                    pending_bytes += addition;
                    pending.push((Some(index), name, relative));
                }
            } else { complete = false; }
        }
        values.push(value);
        context.reserve_file(1, weight + tree.weight() + pending_bytes)?;
    }
    let rows = new(Type::List(Box::new(row_type())), Data::List(values));
    Ok((tree, rows, complete))
}

pub(crate) fn row_type() -> Type {
    Type::Record(vec![("entry".into(), atom("File.Entry")), ("relative".into(), atom("String"))])
}

pub(crate) fn snapshot(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let parent = descendants::directory_observation(site, &args[0])?;
    let entry = descendants::entry_observation(site, &args[1])?;
    descendants::check_lease(context, site, &parent)?;
    if !parent.directory.recursive || parent.session != entry.session || parent.lease != entry.lease || parent.directory != entry.directory { return Err(denied(&site.operation, &parent.directory.path)); }
    if args[1].fields()["kind"].string() != "directory" { return Err(list_failure(site, &args[1], "File.DirectoryFailed", &parent.directory.path, "tree root is not a directory")); }
    let name = args[1].fields()["name"].string();
    let info = read(context, site, &parent, name, &args[1], 2, "File.DirectoryFailed")?;
    if info.identity != entry.identity || !info.is_dir() { return Err(descendants::changed(site, &args[1], &parent.directory.path.join(name))); }
    let root = Row { relative: ".".into(), name: name.into(), parent: None, info };
    let predicate = guard(context, args, 2)?;
    let (tree, rows, complete) = walk(context, site, parent, &root, &args[1], Some(&predicate), None)?;
    let value = adapter("File.Tree", Value::Tree { fields: record([("entries".into(), rows), ("complete".into(), boolean(complete))]), observation: Some(std::sync::Arc::new(tree)) });
    context.reserve_file(0, value.weight)?;
    Ok(value)
}

pub(crate) fn checked(context: &mut Context, site: &Site, args: &[Handle]) -> Outcome {
    let Data::Adapter(crate::adapters::AdapterValue::File(Value::Tree { fields, observation: Some(_), .. })) = &args[0].data else {
        return Err(file_fault(site, "File.InvalidSelection", "tree lacks private observation data", record([("expected".into(), crate::value::snapshot(&args[0]))])));
    };
    if !matches!(fields.fields()["complete"].data, Data::Bool(true)) { return Err(file_fault(site, "File.InvalidSelection", "tree observation is incomplete", record([("expected".into(), crate::value::snapshot(&args[0]))]))); }
    let value = adapter("File.Tree.Remove", Value::TreeRemove { expected: args[0].clone(), guard: guard(context, args, 1)? });
    context.reserve_file(0, value.weight)?;
    Ok(value)
}

fn same_metadata(left: &FileInfo, right: &FileInfo) -> bool {
    left.identity == right.identity && left.kind == right.kind && left.owner == right.owner && left.size == right.size && left.allocated_blocks == right.allocated_blocks && left.links == right.links
        && left.modified == right.modified && left.nanoseconds == right.nanoseconds && left.changed == right.changed && left.changed_nsec == right.changed_nsec
        && (left.is_dir() || (left.accessed == right.accessed && left.accessed_nsec == right.accessed_nsec))
}

fn authority(context: &mut Context, site: &Site, tree: &Tree, input: &Handle) -> Result<(), Fault> {
    descendants::check_lease(context, site, &tree.parent)?;
    let root = tree.directory(Some(0));
    let path = context.files.remove_trees.iter().find(|grant| !grant.inert && root.directory.witnesses.starts_with(&grant.witnesses)).map(|grant| grant.path.clone()).ok_or_else(|| denied("File.remove_tree", &root.directory.path))?;
    context.reserve_file(root.directory.witnesses.len() as u64, tree.weight())?;
    let (file, grant) = exact_grant(&context.files.remove_trees, &context.files.base, "File.remove_tree", &path.to_string_lossy(), site, input).map_err(|fault| {
        if matches!(fault.primary(), Fault::Recoverable(_)) { denied("File.remove_tree", &path) } else { fault }
    })?;
    let result = if root.directory.witnesses.starts_with(&grant.witnesses) { Ok(()) } else { Err(denied("File.remove_tree", &path)) };
    finish(file, result)
}

struct Progress {
    completed: usize,
    bytes: BigInt,
    index: i64,
    stage: &'static str,
}
impl Progress {
    fn value(&self, tree: &Tree) -> Handle {
        let relative = if self.index < 0 { "" } else { &tree.rows[self.index as usize].relative };
        record([("total_count".into(), int(tree.rows.len())), ("completed_count".into(), int(self.completed)), ("completed_bytes".into(), new(atom("Int"), Data::Int(self.bytes.clone()))), ("failed_index".into(), new(atom("Int"), Data::Int(BigInt::from(self.index)))), ("failed_relative".into(), text(relative)), ("stage".into(), text(self.stage))])
    }
    fn record(&self, context: &mut Context, tree: &Tree, rows: &Handle) {
        context.files.last_removal = Some((rows.clone(), self.value(tree)));
    }
}

fn mismatch(site: &Site, input: &Handle, message: &str) -> Fault {
    file_fault(site, "File.EntryChanged", message, record([("expected".into(), crate::value::snapshot(input))]))
}

fn protect(context: &Context, site: &Site, input: &Handle, file: &File, name: &str) -> Result<(), Fault> {
    let info = lstat_at(file, name).map_err(|error| file_fault(site, if error.kind() == io::ErrorKind::NotFound { "File.EntryChanged" } else { "File.RemoveFailed" }, &format!("observe protected tree row {name:?}: {}", system_error(&error)), record([("expected".into(), crate::value::snapshot(input))])))?;
    let identity = crate::locks::Identity(info.identity.0, info.identity.1);
    if context.session.entry.path.is_ok() && context.session.entry.expected_identity().map_err(host)? == identity {
        return Err(denied("File.remove_tree", Path::new(name)));
    }
    context.session.locks.protect_tree_identity(identity)?;
    context.session.console.protect_removal(file, name)?;
    context.session.processes.protect_removal(file, name)?;
    if let Some(probe) = &context.files.probe { probe.protect_removal(file, name)?; }
    Ok(())
}

fn protect_row(context: &mut Context, site: &Site, tree: &Tree, index: usize, input: &Handle) -> Result<(), Fault> {
    let row = &tree.rows[index];
    let parent = tree.directory(row.parent);
    let file = descendants::retained(context, site, &parent, input)?;
    let result = protect(context, site, input, &file, &row.name);
    finish(file, result)
}

fn observe_row(context: &mut Context, site: &Site, tree: &Tree, index: usize, input: &Handle) -> Result<FileInfo, Fault> {
    let row = &tree.rows[index];
    read(context, site, &tree.directory(row.parent), &row.name, input, 4, "File.RemoveFailed")
}

fn remaining_names(tree: &Tree, parent: usize, removed: &[bool]) -> Vec<String> {
    tree.rows.iter().enumerate().filter(|(index, row)| row.parent == Some(parent) && !removed[*index]).map(|(_, row)| row.name.clone()).collect()
}

fn check_names(context: &mut Context, site: &Site, tree: &Tree, index: usize, removed: &[bool], input: &Handle) -> Result<(), Fault> {
    context.reserve_file(tree.rows.len() as u64, tree.weight())?;
    let current = names(context, site, &tree.directory(Some(index)), input, tree.weight(), tree.rows.len().saturating_add(1))?;
    if current != remaining_names(tree, index, removed) { return Err(mismatch(site, input, "tree remaining names changed")); }
    Ok(())
}

fn remove_inner(context: &mut Context, site: &Site, expected: &Handle, guard: &Guard, tree: &Tree, rows: &Handle, progress: &mut Progress) -> Outcome {
    progress.record(context, tree, rows);
    authority(context, site, tree, expected)?;
    context.reserve_file(4 * tree.rows.len() as u64, expected.weight.saturating_mul(3) + tree.weight().saturating_mul(2) + 8192)?;
    let (fresh, fresh_rows, _) = walk(context, site, tree.parent.clone(), &tree.rows[0], expected, None, Some((tree, rows, progress)))?;
    progress.index = -1;
    progress.record(context, tree, rows);
    if fresh.rows.len() != tree.rows.len() { return Err(mismatch(site, expected, "tree topology changed")); }
    for (index, (old, fresh)) in tree.rows.iter().zip(&fresh.rows).enumerate() {
        progress.index = index as i64;
        progress.record(context, tree, rows);
        if old.relative != fresh.relative || old.parent != fresh.parent || !same_metadata(&old.info, &fresh.info) { return Err(mismatch(site, expected, "tree topology or metadata changed")); }
        protect_row(context, site, tree, index, expected)?;
        if !invoke(context, site, guard, fresh_rows.items()[index].clone())? {
            return Err(file_fault(site, "File.EntryChanged", "tree eligibility predicate returned false", record([
                ("expected".into(), crate::value::snapshot(expected)),
                ("guard".into(), record([("predicate".into(), text(&guard.predicate)), ("phase".into(), text("preflight")), ("index".into(), int(index)), ("condition".into(), text("predicate-false"))])),
            ])));
        }
    }
    let mut state = tree.clone();
    let mut removed = vec![false; tree.rows.len()];
    let mut children = vec![vec![]; tree.rows.len()];
    let mut aliases: BTreeMap<(u64, u64), Vec<usize>> = BTreeMap::new();
    for (index, row) in tree.rows.iter().enumerate() {
        if let Some(parent) = row.parent { children[parent].push(index); }
        if !row.info.is_dir() { aliases.entry(row.info.identity).or_default().push(index); }
    }
    let mut order = vec![];
    let mut pending = vec![(0, false)];
    while let Some((index, visited)) = pending.pop() {
        if visited { order.push(index); } else {
            pending.push((index, true));
            pending.extend(children[index].iter().rev().map(|child| (*child, false)));
        }
    }
    for index in order {
        progress.index = index as i64;
        progress.stage = "remove";
        progress.record(context, tree, rows);
        context.check_cancel()?;
        authority(context, site, tree, expected)?;
        let mut ancestor = Some(index);
        while let Some(current) = ancestor {
            progress.index = current as i64;
            progress.record(context, tree, rows);
            let observed = observe_row(context, site, &state, current, expected)?;
            if !same_metadata(&state.rows[current].info, &observed) { return Err(mismatch(site, expected, "tree metadata changed before removal")); }
            ancestor = state.rows[current].parent;
        }
        progress.index = index as i64;
        progress.record(context, tree, rows);
        let row = state.rows[index].clone();
        if let Some(parent) = row.parent {
            progress.index = parent as i64;
            progress.record(context, tree, rows);
            check_names(context, site, &state, parent, &removed, expected)?;
        }
        progress.index = index as i64;
        progress.record(context, tree, rows);
        if row.info.is_dir() { check_names(context, site, &state, index, &removed, expected)?; }
        let parent_observation = state.directory(row.parent);
        let file = descendants::retained(context, site, &parent_observation, expected)?;
        let result = (|| {
            authority(context, site, tree, expected)?;
            protect(context, site, expected, &file, &row.name)?;
            let name = CString::new(row.name.as_str()).unwrap();
            if unsafe { unlinkat(file.as_raw_fd(), name.as_ptr(), if row.info.is_dir() { libc::AT_REMOVEDIR } else { 0 }) } != 0 {
                let error = io::Error::last_os_error();
                return Err(file_fault(site, if error.kind() == io::ErrorKind::NotFound { "File.EntryChanged" } else { "File.RemoveFailed" }, &format!("remove {}: {}", row.relative, system_error(&error)), record([("expected".into(), crate::value::snapshot(expected))])));
            }
            progress.completed += 1;
            progress.bytes += BigInt::from(tree.rows[index].info.size);
            removed[index] = true;
            progress.index = -1;
            progress.record(context, tree, rows);
            context.check_cancel()?;
            progress.stage = "postcheck";
            progress.record(context, tree, rows);
            if let Some(parent) = state.rows[index].parent {
                progress.index = parent as i64;
                progress.record(context, tree, rows);
                check_names(context, site, &state, parent, &removed, expected)?;
                let current = observe_row(context, site, &state, parent, expected)?;
                let old = &state.rows[parent].info;
                if current.identity != old.identity || current.kind != old.kind || current.owner != old.owner { return Err(mismatch(site, expected, "parent changed after removal")); }
                let updated = &mut state.rows[parent].info;
                updated.size = current.size;
                updated.allocated_blocks = current.allocated_blocks;
                updated.links = current.links;
                updated.modified = current.modified;
                updated.nanoseconds = current.nanoseconds;
                updated.changed = current.changed;
                updated.changed_nsec = current.changed_nsec;
            }
            if let Some(group) = aliases.get(&tree.rows[index].info.identity) {
                for alias in group.iter().copied().filter(|index| !removed[*index]) {
                    progress.index = alias as i64;
                    progress.record(context, tree, rows);
                    let current = observe_row(context, site, &state, alias, expected)?;
                    let old = &state.rows[alias].info;
                    let mut adjusted = old.clone();
                    adjusted.links = old.links.checked_sub(1).ok_or_else(|| mismatch(site, expected, "invalid alias link count"))?;
                    adjusted.changed = current.changed;
                    adjusted.changed_nsec = current.changed_nsec;
                    if !same_metadata(&adjusted, &current) { return Err(mismatch(site, expected, "hardlink alias changed after removal")); }
                    state.rows[alias].info = current;
                }
            }
            progress.stage = "finalize";
            progress.index = -1;
            progress.record(context, tree, rows);
            Ok(())
        })();
        finish(file, result)?;
    }
    Ok(new(atom("File.RemoveReceipt"), Data::Record(BTreeMap::from([("total_count".into(), int(tree.rows.len())), ("completed_count".into(), int(progress.completed)), ("observed_bytes".into(), new(atom("Int"), Data::Int(progress.bytes.clone())))]))))
}

pub(crate) fn remove(context: &mut Context, site: &Site, target: &Value) -> Outcome {
    let Value::TreeRemove { expected, guard } = target else { return Err(host("invalid tree removal target")); };
    let Data::Adapter(crate::adapters::AdapterValue::File(Value::Tree { fields, observation: Some(tree) })) = &expected.data else { return Err(denied("File.remove_tree", Path::new("[unobserved tree]"))); };
    let rows = fields.fields()["entries"].clone();
    let mut progress = Progress { completed: 0, bytes: BigInt::from(0), index: -1, stage: "preflight" };
    let mut activation = site.clone();
    activation.operation = "File.remove_tree".into();
    let result = remove_inner(context, &activation, expected, guard, tree, &rows, &mut progress);
    let checked = if progress.completed < tree.rows.len() && result.as_ref().err().is_some_and(|fault| matches!(fault.primary(), Fault::Recoverable(_))) {
        authority(context, &activation, tree, expected)
    } else { Ok(()) };
    postcheck(result, checked).map_err(|fault| {
        match fault {
            Fault::Recoverable(value) => {
                let Data::Error(cause) = &value.data else { return host("invalid tree removal failure"); };
                let mut cause = cause.clone();
                cause.kind = if matches!(cause.kind.as_str(), "File.DirectoryChanged" | "File.DirectoryAbsent" | "File.EntryAbsent" | "File.EntryChanged") { "File.EntryChanged" } else { "File.RemoveFailed" }.into();
                cause.message = format!("{}; tree removal: completed {} of {} entries, {} logical bytes; stage {}", cause.message, progress.completed, tree.rows.len(), progress.bytes, progress.stage);
                if progress.index >= 0 {
                    cause.message.push_str(&format!("; entry {}", crate::checkpoint_print::quote(&tree.rows[progress.index as usize].relative)));
                }
                let mut input = cause.input.fields().clone();
                input.insert("progress".into(), progress.value(tree));
                cause.input = view(&record(input));
                Fault::Recoverable(new(atom("Error"), Data::Error(cause)))
            }
            other => other.with_details(BoundaryDetails { removal_entries: Some(rows.json()), removal_progress: Some(progress.value(tree).json()), ..Default::default() }),
        }
    })
}
