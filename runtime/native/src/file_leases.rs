use super::*;

#[derive(Default)]
pub(super) struct ObservationScope {
    owned: BTreeSet<u64>,
    pinned: BTreeMap<u64, usize>,
}

fn observation_leases(roots: &[Handle]) -> BTreeSet<u64> {
    let mut leases = BTreeSet::new();
    let mut seen = BTreeSet::new();
    let mut pending = roots.to_vec();
    while let Some(value) = pending.pop() {
        if !seen.insert(std::sync::Arc::as_ptr(&value) as usize) {
            continue;
        }
        match &value.data {
            Data::Adapter(crate::adapters::AdapterValue::File(file)) => match file {
                Value::Entry { observation, .. } | Value::Directory { observation, .. } => {
                    if let Some(observation) = observation {
                        leases.insert(observation.lease);
                    }
                }
                Value::Scan { fields } => pending.push(fields.clone()),
                Value::Remove { entries, guard, .. } => {
                    pending.push(entries.clone());
                    if let Some(guard) = guard {
                        pending.extend(guard.contexts.iter().cloned());
                    }
                }
                _ => (),
            },
            Data::Wrapped(inner) | Data::Success(inner) | Data::Failure(inner) => {
                pending.push(inner.clone())
            }
            Data::List(items) => pending.extend(items.iter().cloned()),
            Data::Record(fields) => pending.extend(fields.values().cloned()),
            Data::Map(items) => pending.extend(items.iter().map(|(_, value)| value.clone())),
            Data::Error(error) => {
                pending.push(error.input.clone());
                pending.extend(error.trace.iter().cloned());
                pending.extend(error.flow_progress.iter().cloned());
            }
            _ => (),
        }
    }
    leases
}

impl Files {
    pub fn scope_enter(&mut self, id: usize) {
        self.observation_stack.push(id);
    }
    pub(super) fn retain_observation(
        &mut self,
        directory: entries::Directory,
        lease: u64,
        file: File,
    ) {
        self.observed.insert(directory.clone(), file);
        self.observation_directories
            .insert(lease, directory.clone());
        self.observed_leases.insert(directory, lease);
        let id = *self.observation_stack.last().unwrap_or(&0);
        self.observation_scopes
            .entry(id)
            .or_default()
            .owned
            .insert(lease);
        self.observation_owners.insert(lease, id);
    }
    fn release_observations(&mut self, leases: Vec<u64>) -> Result<(), String> {
        let mut errors = vec![];
        for lease in leases {
            self.observation_owners.remove(&lease);
            let directory = self.observation_directories.remove(&lease);
            if let Some(directory) = directory {
                self.observed_leases.remove(&directory);
                if let Some(file) = self.observed.remove(&directory) {
                    if let Err(error) = close_file(file) {
                        errors.push(format!(
                            "close observed directory {}: {}",
                            quote(&directory.path),
                            system_error(&error)
                        ));
                    }
                }
            }
        }
        if errors.is_empty() {
            Ok(())
        } else {
            Err(errors.join("\n"))
        }
    }
    pub fn scope_keep(&mut self, id: usize, roots: &[Handle]) -> Result<(), String> {
        if self
            .observation_scopes
            .get(&id)
            .is_none_or(|scope| scope.owned.is_empty())
        {
            return Ok(());
        }
        let retained = observation_leases(roots);
        let mut released = vec![];
        if let Some(scope) = self.observation_scopes.get_mut(&id) {
            scope.owned.retain(|lease| {
                if retained.contains(lease) {
                    true
                } else {
                    released.push(*lease);
                    false
                }
            });
        }
        self.release_observations(released)
    }
    pub fn scope_retain(&mut self, id: usize, roots: &[Handle]) -> Result<(), String> {
        if self
            .observation_scopes
            .get(&id)
            .is_none_or(|scope| scope.owned.is_empty() && scope.pinned.is_empty())
        {
            return Ok(());
        }
        let Some(index) = self
            .observation_stack
            .iter()
            .position(|current| *current == id)
        else {
            return Ok(());
        };
        let parent = if index == 0 {
            0
        } else {
            self.observation_stack[index - 1]
        };
        if parent == id {
            return Ok(());
        }
        for lease in observation_leases(roots) {
            if self.observation_owners.get(&lease) == Some(&id) {
                if let Some(scope) = self.observation_scopes.get_mut(&id) {
                    scope.owned.remove(&lease);
                    scope.pinned.remove(&lease);
                }
                self.observation_scopes
                    .entry(parent)
                    .or_default()
                    .owned
                    .insert(lease);
                self.observation_owners.insert(lease, parent);
            }
        }
        Ok(())
    }
    pub fn scope_close(&mut self, id: usize, roots: &[Handle]) -> Result<(), String> {
        self.scope_retain(id, roots)?;
        assert_eq!(self.observation_stack.pop(), Some(id));
        match self.observation_scopes.remove(&id) {
            Some(scope) => self.release_observations(
                scope
                    .owned
                    .into_iter()
                    .chain(scope.pinned.into_keys())
                    .collect(),
            ),
            None => Ok(()),
        }
    }
    pub fn scope_pin(&mut self, id: usize, roots: &[Handle]) -> Result<Vec<u64>, String> {
        if self
            .observation_scopes
            .get(&id)
            .is_none_or(|scope| scope.owned.is_empty() && scope.pinned.is_empty())
        {
            return Ok(vec![]);
        }
        let mut pinned = vec![];
        for lease in observation_leases(roots) {
            if self.observation_owners.get(&lease) != Some(&id) {
                continue;
            }
            let scope = self.observation_scopes.entry(id).or_default();
            scope.owned.remove(&lease);
            *scope.pinned.entry(lease).or_default() += 1;
            pinned.push(lease);
        }
        Ok(pinned)
    }
    pub fn scope_unpin(&mut self, id: usize, leases: &[u64]) -> Result<(), String> {
        let Some(scope) = self.observation_scopes.get_mut(&id) else {
            return Ok(());
        };
        for lease in leases {
            if let Some(count) = scope.pinned.get_mut(lease) {
                *count -= 1;
                if *count == 0 {
                    scope.pinned.remove(lease);
                    scope.owned.insert(*lease);
                }
            }
        }
        Ok(())
    }
}
