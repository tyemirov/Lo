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
    identities: IdentitySet,
    max_list_items: usize,
}
struct Cached {
    _root: Weak<Value>,
    summary: Arc<Summary>,
}
#[derive(Default)]
pub(crate) struct GraphCache {
    // Ledger receipt release removes the entry; weak roots do not retain data.
    entries: HashMap<usize, Cached, IdentityBuildHasher>,
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
    fn into_summary(self) -> Arc<Summary> {
        assert!(self.borrowed.is_empty());
        Arc::new(Summary {
            identities: self.direct,
            max_list_items: self.max_list_items,
        })
    }
}

impl GraphCache {
    fn get(&self, root: &Handle) -> Option<Arc<Summary>> {
        self.entries
            .get(&(Arc::as_ptr(root) as usize))
            .map(|entry| entry.summary.clone())
    }
    fn insert(&mut self, root: &Handle, summary: Arc<Summary>) {
        self.entries.insert(
            Arc::as_ptr(root) as usize,
            Cached {
                _root: Arc::downgrade(root),
                summary,
            },
        );
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
                        let summary = subgraph.into_summary();
                        self.insert(value, summary.clone());
                        if let Some(child) = child.filter(|child| child.weight >= value.weight / 2)
                        {
                            let child_graph = self
                                .scan(std::slice::from_ref(&child), ancestor, false, false)
                                .0;
                            if !child_graph.has_cached()
                                && child_graph.direct.len() >= MIN_CACHE_IDENTITIES
                            {
                                self.insert(&child, child_graph.into_summary());
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
    match &value.data {
        Data::Wrapped(value) | Data::Success(value) | Data::Failure(value) => stack.push(value),
        Data::List(values) => stack.extend(values),
        Data::Record(fields) => stack.extend(fields.values()),
        Data::Map(values) => {
            for (key, value) in values {
                stack.push(key);
                stack.push(value);
            }
        }
        Data::Range(first, last, _, _) => {
            stack.push(first);
            stack.push(last);
        }
        Data::Error(error) => {
            stack.push(&error.input);
            stack.extend(&error.trace);
            stack.extend(&error.flow_progress);
        }
        Data::Adapter(crate::adapters::AdapterValue::File(crate::files::Value::Remove {
            entries,
            guard,
            ..
        })) => {
            stack.push(entries);
            if let Some(guard) = guard {
                stack.extend(&guard.contexts);
            }
        }
        Data::Adapter(value) => {
            if let Some(fields) = value.fields() {
                stack.extend(fields.values());
            }
        }
        _ => (),
    }
}
