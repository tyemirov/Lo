use crate::value_id_hash::IdentityBuildHasher;
use std::collections::HashMap;

const RECENT_SLOTS: usize = 16;

pub(crate) struct EntryMap<T> {
    recent: [Option<(usize, T)>; RECENT_SLOTS],
    cold: HashMap<usize, T, IdentityBuildHasher>,
}

impl<T> EntryMap<T> {
    pub fn new() -> Self {
        Self {
            recent: std::array::from_fn(|_| None),
            cold: HashMap::default(),
        }
    }
    fn slot(key: usize) -> usize {
        ((key >> 4) ^ (key >> 12)) & (RECENT_SLOTS - 1)
    }
    pub fn get(&self, key: &usize) -> Option<&T> {
        if let Some((present, value)) = &self.recent[Self::slot(*key)] {
            if present == key {
                return Some(value);
            }
        }
        self.cold.get(key)
    }
    pub fn get_mut(&mut self, key: &usize) -> Option<&mut T> {
        let index = Self::slot(*key);
        let present = self.recent[index]
            .as_ref()
            .is_some_and(|(present, _)| present == key);
        if present {
            self.recent[index].as_mut().map(|(_, value)| value)
        } else {
            self.cold.get_mut(key)
        }
    }
    pub fn contains_key(&self, key: &usize) -> bool {
        self.get(key).is_some()
    }
    pub fn insert_absent(&mut self, key: usize, value: T) {
        if let Some((key, value)) = self.recent[Self::slot(key)].replace((key, value)) {
            self.cold.insert(key, value);
        }
    }
    pub fn remove(&mut self, key: &usize) -> Option<T> {
        let index = Self::slot(*key);
        let present = self.recent[index]
            .as_ref()
            .is_some_and(|(present, _)| present == key);
        if present {
            self.recent[index].take().map(|(_, value)| value)
        } else {
            self.cold.remove(key)
        }
    }
    pub fn iter(&self) -> impl Iterator<Item = (&usize, &T)> {
        self.recent
            .iter()
            .filter_map(|entry| entry.as_ref().map(|(key, value)| (key, value)))
            .chain(self.cold.iter())
    }
    #[cfg(test)]
    fn insert(&mut self, key: usize, value: T) -> Option<T> {
        if let Some(present) = self.get_mut(&key) {
            Some(std::mem::replace(present, value))
        } else {
            self.insert_absent(key, value);
            None
        }
    }
}

#[cfg(test)]
mod tests {
    use super::EntryMap;
    use std::{cell::Cell, collections::HashMap, rc::Rc, sync::Arc};

    #[test]
    fn canonical_updates_evictions_and_removals_match_hash_map() {
        let mut actual = EntryMap::new();
        let mut expected = HashMap::new();
        let mut random = 0x7183_u64;
        for step in 0..10000 {
            random = random.wrapping_mul(6364136223846793005).wrapping_add(1);
            let key = ((random >> 32) % 257) as usize * 16;
            match step % 5 {
                0 | 1 => assert_eq!(actual.insert(key, step), expected.insert(key, step)),
                2 => assert_eq!(actual.remove(&key), expected.remove(&key)),
                3 => {
                    if let Some(value) = actual.get_mut(&key) {
                        *value += 1;
                    }
                    if let Some(value) = expected.get_mut(&key) {
                        *value += 1;
                    }
                }
                _ => assert_eq!(actual.get(&key), expected.get(&key)),
            }
            assert_eq!(actual.iter().count(), expected.len());
            assert_eq!(
                actual
                    .iter()
                    .map(|(key, value)| (*key, *value))
                    .collect::<HashMap<_, _>>(),
                expected
            );
            for key in (0..257).map(|value| value * 16) {
                assert_eq!(actual.contains_key(&key), expected.contains_key(&key));
            }
        }
    }

    #[test]
    fn occupied_evictions_move_nonclone_values_without_dropping() {
        struct Tracked {
            value: usize,
            drops: Rc<Cell<usize>>,
        }
        impl Drop for Tracked {
            fn drop(&mut self) {
                self.drops.set(self.drops.get() + 1);
            }
        }
        let drops = Rc::new(Cell::new(0));
        let mut entries = EntryMap::new();
        for value in 0..512 {
            entries.insert_absent(
                value * 16,
                Tracked {
                    value,
                    drops: drops.clone(),
                },
            );
        }
        assert_eq!(drops.get(), 0);
        assert_eq!(entries.iter().count(), 512);
        for value in 0..512 {
            assert_eq!(entries.get(&(value * 16)).unwrap().value, value);
        }
        for value in (0..512).step_by(2) {
            assert_eq!(entries.remove(&(value * 16)).unwrap().value, value);
        }
        assert_eq!(drops.get(), 256);
        drop(entries);
        assert_eq!(drops.get(), 512);
    }

    #[test]
    fn weak_orphans_are_visible_in_both_stores() {
        let mut entries = EntryMap::new();
        let mut live = Vec::new();
        for value in 0..256 {
            let value = Arc::new(value);
            let key = Arc::as_ptr(&value) as usize;
            entries.insert_absent(key, Arc::downgrade(&value));
            live.push(Some(value));
        }
        for value in live.iter_mut().step_by(2) {
            *value = None;
        }
        let expired = entries
            .iter()
            .filter_map(|(key, value)| (value.strong_count() == 0).then_some(*key))
            .collect::<Vec<_>>();
        assert_eq!(expired.len(), 128);
        for key in expired {
            entries.remove(&key);
        }
        assert_eq!(entries.iter().count(), 128);
        assert!(entries.iter().all(|(_, value)| value.upgrade().is_some()));
    }
}
