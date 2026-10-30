use crate::{
    value::{Data, Handle, Value},
    value_id_hash::IdentityBuildHasher,
    value_id_set::IdentitySet,
    value_stack::PendingStack,
};
use std::{
    collections::HashMap,
    sync::{Arc, Weak},
};

const MIN_CACHE_IDENTITIES: usize = 16;

struct Summary {
    root: Weak<Value>,
    identities: IdentitySet,
    max_list_items: usize,
}
#[derive(Default)]
pub(crate) struct GraphCache {
    // Ledger receipt release removes the entry; weak roots do not retain data.
    entries: HashMap<usize, Arc<Summary>, IdentityBuildHasher>,
}

#[derive(Default)]
pub(crate) struct Reachability {
    pub direct: IdentitySet,
    borrowed: Vec<Arc<Summary>>,
    pub max_list_items: usize,
}
impl Reachability {
    pub fn contains_cached(&self, value: &usize) -> bool {
        self.borrowed
            .iter()
            .any(|graph| graph.identities.contains(value))
    }
    pub fn has_cached(&self) -> bool {
        !self.borrowed.is_empty()
    }
    pub fn cached_len(&self) -> usize {
        self.borrowed
            .iter()
            .map(|graph| graph.identities.len())
            .sum()
    }
    pub fn cached_values(&self) -> impl Iterator<Item = &usize> {
        self.borrowed
            .iter()
            .flat_map(|graph| graph.identities.iter())
    }
    pub fn cached_roots(&self) -> impl Iterator<Item = &Weak<Value>> {
        self.borrowed.iter().map(|graph| &graph.root)
    }
    pub fn cached_parts(&self) -> usize {
        self.borrowed.len()
    }
    fn add_summary(&mut self, summary: Arc<Summary>) {
        self.max_list_items = self.max_list_items.max(summary.max_list_items);
        self.borrowed.push(summary);
    }
    fn absorb(&mut self, graph: Self) {
        self.max_list_items = self.max_list_items.max(graph.max_list_items);
        self.direct.extend(graph.direct);
        self.borrowed.extend(graph.borrowed);
    }
    fn into_summary(self, root: &Handle) -> Arc<Summary> {
        assert!(self.borrowed.is_empty());
        Arc::new(Summary {
            root: Arc::downgrade(root),
            identities: self.direct,
            max_list_items: self.max_list_items,
        })
    }
}

impl GraphCache {
    fn get(&self, root: &Handle) -> Option<Arc<Summary>> {
        self.entries.get(&(Arc::as_ptr(root) as usize)).cloned()
    }
    fn insert(&mut self, root: &Handle, summary: Arc<Summary>) {
        self.entries.insert(Arc::as_ptr(root) as usize, summary);
    }
    pub fn remove(&mut self, root: usize) {
        self.entries.remove(&root);
    }
    pub fn reachable(
        &mut self,
        roots: &[Handle],
        ancestor: impl Fn(usize) -> bool,
    ) -> Reachability {
        self.scan(roots, &ancestor, true, false).0
    }
    pub fn reachable_keep(
        &mut self,
        roots: &[Handle],
        eligible: impl Fn(usize) -> bool,
    ) -> Reachability {
        self.discover_keep_fragments(roots, &eligible);
        self.scan(roots, &eligible, false, false).0
    }
    fn discover_keep_fragments(&mut self, roots: &[Handle], eligible: &impl Fn(usize) -> bool) {
        #[derive(Clone, Copy)]
        struct Completed {
            count: usize,
            cached: bool,
        }
        let mut completed: HashMap<usize, Completed, IdentityBuildHasher> = HashMap::default();
        let mut pending = PendingStack::default();
        for root in roots {
            pending.push((root, false));
        }
        while let Some((value, children_done)) = pending.pop() {
            let id = Arc::as_ptr(value) as usize;
            if completed.contains_key(&id) {
                continue;
            }
            if self.entries.contains_key(&id) {
                completed.insert(
                    id,
                    Completed {
                        count: 0,
                        cached: true,
                    },
                );
                continue;
            }
            if is_leaf(value) {
                completed.insert(
                    id,
                    Completed {
                        count: 1,
                        cached: false,
                    },
                );
                continue;
            }
            if !children_done {
                pending.push((value, true));
                for_each_child(value, |child| pending.push((child, false)));
                continue;
            }
            let mut result = Completed {
                count: 1,
                cached: false,
            };
            for_each_child(value, |child| {
                let child = completed.get(&(Arc::as_ptr(child) as usize)).unwrap();
                result.count = result
                    .count
                    .saturating_add(child.count)
                    .min(MIN_CACHE_IDENTITIES);
                result.cached |= child.cached;
            });
            if !result.cached && result.count >= MIN_CACHE_IDENTITIES && eligible(id) {
                let graph = self
                    .scan(std::slice::from_ref(value), eligible, false, false)
                    .0;
                if !graph.has_cached() && graph.direct.len() >= MIN_CACHE_IDENTITIES {
                    self.insert(value, graph.into_summary(value));
                    result.cached = true;
                }
            }
            completed.insert(id, result);
        }
    }
    fn scan(
        &mut self,
        roots: &[Handle],
        ancestor: &impl Fn(usize) -> bool,
        admit: bool,
        find_dominant: bool,
    ) -> (Reachability, Option<Handle>) {
        let mut graph = Reachability::default();
        let mut dominant: Option<Handle> = None;
        let mut stack = PendingStack::default();
        stack.extend(roots);
        while let Some(value) = stack.pop() {
            let id = Arc::as_ptr(value) as usize;
            // Include cached fragment roots in the exact set of popped nodes.
            if !graph.direct.insert(id) {
                continue;
            }
            if find_dominant
                && roots.len() == 1
                && !Arc::ptr_eq(value, &roots[0])
                && ancestor(id)
                && dominant
                    .as_ref()
                    .is_none_or(|prior| value.weight > prior.weight)
            {
                dominant = Some(value.clone());
            }
            if !is_leaf(value) {
                if let Some(summary) = self.get(value) {
                    graph.add_summary(summary);
                    continue;
                }
                if admit && ancestor(id) {
                    let (subgraph, child) =
                        self.scan(std::slice::from_ref(value), ancestor, false, true);
                    if !subgraph.has_cached() && subgraph.direct.len() >= MIN_CACHE_IDENTITIES {
                        let summary = subgraph.into_summary(value);
                        self.insert(value, summary.clone());
                        if let Some(child) = child.filter(|child| child.weight >= value.weight / 2)
                        {
                            let child_graph = self
                                .scan(std::slice::from_ref(&child), ancestor, false, false)
                                .0;
                            if !child_graph.has_cached()
                                && child_graph.direct.len() >= MIN_CACHE_IDENTITIES
                            {
                                self.insert(&child, child_graph.into_summary(&child));
                            }
                        }
                        graph.add_summary(summary);
                    } else {
                        graph.absorb(subgraph);
                    }
                    continue;
                }
            }
            if let Data::List(values) = &value.data {
                graph.max_list_items = graph.max_list_items.max(values.len());
            }
            push_children(value, &mut stack);
        }
        (graph, dominant)
    }
}

pub(crate) fn is_leaf(value: &Handle) -> bool {
    matches!(
        value.data,
        Data::Int(_)
            | Data::Float(_)
            | Data::Text(_)
            | Data::Bool(_)
            | Data::Null
            | Data::Tensor(_)
    )
}

fn push_children<'a>(value: &'a Handle, stack: &mut PendingStack<&'a Handle>) {
    for_each_child(value, |child| stack.push(child));
}

fn for_each_child<'a>(value: &'a Handle, mut push: impl FnMut(&'a Handle)) {
    match &value.data {
        Data::Wrapped(value) | Data::Success(value) | Data::Failure(value) => push(value),
        Data::List(values) => values.iter().for_each(&mut push),
        Data::Record(fields) => fields.values().for_each(&mut push),
        Data::Map(values) => {
            for (key, value) in values {
                push(key);
                push(value);
            }
        }
        Data::Range(first, last, _, _) => {
            push(first);
            push(last);
        }
        Data::Error(error) => {
            push(&error.input);
            error.trace.iter().for_each(&mut push);
            error.flow_progress.iter().for_each(&mut push);
        }
        Data::Adapter(crate::adapters::AdapterValue::File(crate::files::Value::Remove {
            entries,
            guard,
            ..
        })) => {
            push(entries);
            if let Some(guard) = guard {
                guard.contexts.iter().for_each(&mut push);
            }
        }
        Data::Adapter(value) => {
            if let Some(fields) = value.fields() {
                fields.values().for_each(&mut push);
            }
        }
        _ => (),
    }
}
