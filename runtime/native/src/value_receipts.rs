const PAGE_SIZE: usize = 64;
const INLINE_SLOTS: usize = 4;

pub(crate) struct ReceiptArena<T> {
    pages: Vec<Box<[Option<T>; PAGE_SIZE]>>,
    free: Vec<usize>,
    next: usize,
}

impl<T> Default for ReceiptArena<T> {
    fn default() -> Self {
        Self {
            pages: Vec::new(),
            free: Vec::new(),
            next: 0,
        }
    }
}

impl<T> ReceiptArena<T> {
    pub fn insert(&mut self, value: T) -> usize {
        if let Some(slot) = self.free.pop() {
            let entry = &mut self.pages[slot / PAGE_SIZE][slot % PAGE_SIZE];
            debug_assert!(entry.is_none());
            *entry = Some(value);
            return slot;
        }
        let slot = self.next;
        if slot / PAGE_SIZE == self.pages.len() {
            self.allocate_page();
        }
        self.pages[slot / PAGE_SIZE][slot % PAGE_SIZE] = Some(value);
        self.next += 1;
        slot
    }

    #[cold]
    #[inline(never)]
    fn allocate_page(&mut self) {
        self.pages.push(Box::new(std::array::from_fn(|_| None)));
    }

    pub fn get(&self, slot: usize) -> &T {
        self.pages[slot / PAGE_SIZE][slot % PAGE_SIZE]
            .as_ref()
            .expect("occupied receipt slot")
    }

    pub fn get_mut(&mut self, slot: usize) -> &mut T {
        self.pages[slot / PAGE_SIZE][slot % PAGE_SIZE]
            .as_mut()
            .expect("occupied receipt slot")
    }

    pub fn remove(&mut self, slot: usize) -> T {
        let value = self.pages[slot / PAGE_SIZE][slot % PAGE_SIZE]
            .take()
            .expect("occupied receipt slot");
        self.free.push(slot);
        value
    }
}

#[derive(Default)]
pub(crate) struct SmallSlots {
    inline: [usize; INLINE_SLOTS],
    inline_len: usize,
    spill: Vec<usize>,
}

impl SmallSlots {
    pub fn len(&self) -> usize {
        self.inline_len + self.spill.len()
    }

    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    pub fn get(&self, index: usize) -> usize {
        if index < self.inline_len {
            self.inline[index]
        } else {
            self.spill[index - self.inline_len]
        }
    }

    pub fn swap(&mut self, first: usize, second: usize) {
        assert!(first < self.len() && second < self.len());
        if first == second {
            return;
        }
        let (a, b) = (self.get(first), self.get(second));
        self.set(first, b);
        self.set(second, a);
    }

    fn set(&mut self, index: usize, slot: usize) {
        if index < self.inline_len {
            self.inline[index] = slot;
        } else {
            self.spill[index - self.inline_len] = slot;
        }
    }

    pub fn push(&mut self, slot: usize) -> usize {
        let position = self.len();
        if self.inline_len < INLINE_SLOTS {
            self.inline[self.inline_len] = slot;
            self.inline_len += 1;
        } else {
            self.spill.push(slot);
        }
        position
    }

    pub fn swap_remove(&mut self, index: usize) -> Option<usize> {
        let length = self.len();
        assert!(index < length);
        let replacement = if let Some(slot) = self.spill.pop() {
            slot
        } else {
            self.inline_len -= 1;
            std::mem::replace(&mut self.inline[self.inline_len], 0)
        };
        if index == length - 1 {
            return None;
        }
        if index < self.inline_len {
            self.inline[index] = replacement;
        } else {
            self.spill[index - self.inline_len] = replacement;
        }
        Some(replacement)
    }

    pub fn drain(&mut self) -> <Self as IntoIterator>::IntoIter {
        std::mem::take(self).into_iter()
    }
}

impl IntoIterator for SmallSlots {
    type Item = usize;
    type IntoIter = std::iter::Chain<
        std::iter::Take<std::array::IntoIter<usize, INLINE_SLOTS>>,
        std::vec::IntoIter<usize>,
    >;

    fn into_iter(self) -> Self::IntoIter {
        self.inline
            .into_iter()
            .take(self.inline_len)
            .chain(self.spill)
    }
}

#[cfg(test)]
mod tests {
    use super::{ReceiptArena, SmallSlots, PAGE_SIZE};
    use std::{cell::Cell, collections::HashMap, rc::Rc};

    #[test]
    fn arena_admission_mutation_and_removal_match_hash_map() {
        let mut actual = ReceiptArena::default();
        let mut expected = HashMap::new();
        let mut occupied = Vec::new();
        let mut peak = 0;
        let mut random = 0x57a3_u64;
        for step in 0..10000 {
            random = random.wrapping_mul(6364136223846793005).wrapping_add(1);
            if step % 3 == 0 && !occupied.is_empty() {
                let index = (random as usize) % occupied.len();
                let slot = occupied.swap_remove(index);
                assert_eq!(actual.remove(slot), expected.remove(&slot).unwrap());
            } else {
                let slot = actual.insert(step);
                assert!(expected.insert(slot, step).is_none());
                occupied.push(slot);
                peak = peak.max(occupied.len());
            }
            if !occupied.is_empty() && step % 7 == 0 {
                let slot = occupied[(random as usize) % occupied.len()];
                *actual.get_mut(slot) += 1;
                *expected.get_mut(&slot).unwrap() += 1;
            }
            if step % 113 == 0 {
                for (&slot, value) in &expected {
                    assert_eq!(actual.get(slot), value);
                }
            }
            assert_eq!(actual.next, peak);
            assert_eq!(actual.pages.len(), peak.div_ceil(PAGE_SIZE));
        }
        for slot in occupied {
            assert_eq!(actual.remove(slot), expected.remove(&slot).unwrap());
        }
        assert!(expected.is_empty());
        assert_eq!(actual.free.len(), actual.next);
    }

    #[test]
    fn arena_zero_slot_reuses_storage_without_replacing_live_values() {
        let mut arena = ReceiptArena::default();
        let first = arena.insert(String::from("first"));
        let second = arena.insert(String::from("second"));
        assert_eq!(first, 0);
        assert_eq!(arena.remove(first), "first");
        let reused = arena.insert(String::from("reused"));
        assert_eq!(reused, first);
        arena.get_mut(second).push_str(" value");
        assert_eq!(arena.get(second).as_str(), "second value");
        assert_eq!(arena.get(reused).as_str(), "reused");
    }

    #[test]
    fn arena_removed_and_retained_values_drop_once() {
        struct Tracked {
            identity: usize,
            drops: Rc<Vec<Cell<usize>>>,
        }
        impl Drop for Tracked {
            fn drop(&mut self) {
                let count = &self.drops[self.identity];
                count.set(count.get() + 1);
            }
        }
        let drops = Rc::new((0..256).map(|_| Cell::new(0)).collect::<Vec<_>>());
        let mut arena = ReceiptArena::default();
        let mut original = Vec::new();
        for identity in 0..128 {
            original.push(arena.insert(Tracked {
                identity,
                drops: drops.clone(),
            }));
        }
        for identity in (0..128).step_by(2) {
            drop(arena.remove(original[identity]));
        }
        for identity in 128..256 {
            arena.insert(Tracked {
                identity,
                drops: drops.clone(),
            });
        }
        for identity in 0..128 {
            assert_eq!(drops[identity].get(), if identity % 2 == 0 { 1 } else { 0 });
        }
        drop(arena);
        assert!(drops.iter().all(|count| count.get() == 1));
    }

    fn check_slots(actual: &SmallSlots, expected: &[usize]) {
        assert_eq!(actual.len(), expected.len());
        assert_eq!(actual.is_empty(), expected.is_empty());
        for (index, &slot) in expected.iter().enumerate() {
            assert_eq!(actual.get(index), slot);
        }
    }

    #[test]
    fn small_slots_push_swap_remove_and_drain_match_vec() {
        let mut actual = SmallSlots::default();
        let mut expected = Vec::new();
        let mut random = 0x9c21_u64;
        for step in 0..10000 {
            random = random.wrapping_mul(6364136223846793005).wrapping_add(1);
            if step % 211 == 0 {
                assert_eq!(actual.drain().collect::<Vec<_>>(), expected);
                expected.clear();
            } else if random & 3 == 0 && !expected.is_empty() {
                let index = (random as usize) % expected.len();
                expected.swap_remove(index);
                let moved = expected.get(index).copied();
                assert_eq!(actual.swap_remove(index), moved);
            } else {
                let slot = ((random >> 32) % 17) as usize;
                assert_eq!(actual.push(slot), expected.len());
                expected.push(slot);
            }
            check_slots(&actual, &expected);
        }
        assert_eq!(actual.into_iter().collect::<Vec<_>>(), expected);
    }

    #[test]
    fn small_slots_swap_matches_vec_at_every_inline_spill_boundary() {
        for length in 1..12 {
            for first in 0..length {
                for second in 0..length {
                    let mut slots = SmallSlots::default();
                    let mut expected = Vec::new();
                    for value in 0..length {
                        slots.push(value);
                        expected.push(value);
                    }
                    slots.swap(first, second);
                    expected.swap(first, second);
                    assert_eq!(slots.into_iter().collect::<Vec<_>>(), expected);
                }
            }
        }
    }

    #[test]
    fn small_slots_move_report_crosses_inline_spill_boundary() {
        for count in [1, 4, 5, 9, 65] {
            for index in 0..count {
                let mut actual = SmallSlots::default();
                let mut expected = (0..count).collect::<Vec<_>>();
                for slot in 0..count {
                    actual.push(slot);
                }
                expected.swap_remove(index);
                assert_eq!(actual.swap_remove(index), expected.get(index).copied());
                check_slots(&actual, &expected);
            }
        }
    }

    #[test]
    fn small_slots_grow_and_partial_drain_leaves_source_empty() {
        let mut slots = SmallSlots::default();
        for slot in 0..8192 {
            assert_eq!(slots.push(slot), slot);
        }
        let mut drained = slots.drain();
        for slot in 0..5 {
            assert_eq!(drained.next(), Some(slot));
        }
        drop(drained);
        assert!(slots.is_empty());
        assert_eq!(slots.push(0), 0);
        assert_eq!(slots.into_iter().collect::<Vec<_>>(), vec![0]);
    }
}
