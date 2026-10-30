use crate::value_id_hash::IdentityBuildHasher;
use std::collections::HashSet;

const INLINE_CAPACITY: usize = 4;

#[derive(Default)]
pub(crate) struct IdentitySet {
    inline: [usize; INLINE_CAPACITY],
    inline_len: usize,
    spill: Option<HashSet<usize, IdentityBuildHasher>>,
}

impl IdentitySet {
    pub fn len(&self) -> usize {
        self.inline_len + self.spill.as_ref().map_or(0, HashSet::len)
    }
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }
    pub fn contains(&self, value: &usize) -> bool {
        self.inline[..self.inline_len].contains(value)
            || self
                .spill
                .as_ref()
                .is_some_and(|spill| spill.contains(value))
    }
    pub fn insert(&mut self, value: usize) -> bool {
        if self.inline[..self.inline_len].contains(&value) {
            return false;
        }
        if self.inline_len < INLINE_CAPACITY {
            if self
                .spill
                .as_ref()
                .is_some_and(|spill| spill.contains(&value))
            {
                return false;
            }
            self.inline[self.inline_len] = value;
            self.inline_len += 1;
            true
        } else {
            self.spill
                .get_or_insert_with(HashSet::default)
                .insert(value)
        }
    }
    pub fn remove(&mut self, value: &usize) -> bool {
        if let Some(index) = self.inline[..self.inline_len]
            .iter()
            .position(|item| item == value)
        {
            self.inline_len -= 1;
            self.inline[index] = self.inline[self.inline_len];
            self.inline[self.inline_len] = 0;
            true
        } else {
            self.spill.as_mut().is_some_and(|spill| spill.remove(value))
        }
    }
    pub fn iter(&self) -> impl Iterator<Item = &usize> {
        self.inline[..self.inline_len]
            .iter()
            .chain(self.spill.iter().flatten())
    }
    pub fn retain(&mut self, mut keep: impl FnMut(&usize) -> bool) {
        let mut index = 0;
        while index < self.inline_len {
            if keep(&self.inline[index]) {
                index += 1;
            } else {
                self.inline_len -= 1;
                self.inline[index] = self.inline[self.inline_len];
                self.inline[self.inline_len] = 0;
            }
        }
        if let Some(spill) = self.spill.as_mut() {
            spill.retain(keep);
        }
        let available = INLINE_CAPACITY - self.inline_len;
        if self
            .spill
            .as_ref()
            .is_some_and(|spill| spill.len() <= available)
        {
            for value in self.spill.take().unwrap() {
                self.inline[self.inline_len] = value;
                self.inline_len += 1;
            }
        } else if let Some(spill) = self.spill.as_mut() {
            if spill.capacity() > spill.len().saturating_mul(4) {
                spill.shrink_to_fit();
            }
        }
    }
    pub fn drain(&mut self) -> <Self as IntoIterator>::IntoIter {
        std::mem::take(self).into_iter()
    }
}

impl Extend<usize> for IdentitySet {
    fn extend<T: IntoIterator<Item = usize>>(&mut self, values: T) {
        for value in values {
            self.insert(value);
        }
    }
}

impl IntoIterator for IdentitySet {
    type Item = usize;
    type IntoIter = std::iter::Chain<
        std::iter::Take<std::array::IntoIter<usize, INLINE_CAPACITY>>,
        std::iter::Flatten<std::option::IntoIter<HashSet<usize, IdentityBuildHasher>>>,
    >;
    fn into_iter(self) -> Self::IntoIter {
        self.inline
            .into_iter()
            .take(self.inline_len)
            .chain(self.spill.into_iter().flatten())
    }
}

#[cfg(test)]
mod tests {
    use super::IdentitySet;
    use std::collections::HashSet;

    fn check(actual: &IdentitySet, expected: &HashSet<usize>) {
        assert_eq!(actual.len(), expected.len());
        assert_eq!(actual.is_empty(), expected.is_empty());
        assert_eq!(actual.iter().copied().collect::<HashSet<_>>(), *expected);
        assert_eq!(actual.iter().count(), expected.len());
        for value in 0..128 {
            assert_eq!(actual.contains(&value), expected.contains(&value));
        }
    }

    #[test]
    fn insertion_removal_union_and_retain_match_hash_set() {
        let mut actual = IdentitySet::default();
        let mut expected = HashSet::new();
        let mut random = 0x5a17_u64;
        for step in 0..10000 {
            random = random.wrapping_mul(6364136223846793005).wrapping_add(1);
            let value = ((random >> 32) % 97) as usize;
            if step % 3 == 0 {
                assert_eq!(actual.remove(&value), expected.remove(&value));
            } else {
                assert_eq!(actual.insert(value), expected.insert(value));
            }
            if step % 37 == 0 {
                let remainder = step % 7;
                actual.retain(|value| value % 7 != remainder);
                expected.retain(|value| value % 7 != remainder);
            }
            if step % 113 == 0 {
                let mut other = IdentitySet::default();
                other.extend(64..96);
                expected.extend(other.iter().copied());
                actual.extend(other.drain());
                assert!(other.is_empty());
            }
            check(&actual, &expected);
        }
        assert_eq!(actual.into_iter().collect::<HashSet<_>>(), expected);
    }

    #[test]
    fn spill_duplicates_after_inline_removal_stay_unique() {
        let mut actual = IdentitySet::default();
        let mut expected = HashSet::new();
        for value in 0..32 {
            actual.insert(value);
            expected.insert(value);
        }
        for value in 0..4 {
            actual.remove(&value);
            expected.remove(&value);
        }
        for value in [12, 16, 31, 0, usize::MAX] {
            assert_eq!(actual.insert(value), expected.insert(value));
        }
        check(&actual, &expected);
        actual.retain(|value| value % 2 != 0);
        expected.retain(|value| value % 2 != 0);
        check(&actual, &expected);
    }

    #[test]
    fn partial_drain_empties_the_source() {
        let mut actual = IdentitySet::default();
        actual.extend(0..32);
        let mut drain = actual.drain();
        assert!(drain.next().is_some());
        drop(drain);
        assert!(actual.is_empty());
        assert!(actual.insert(42));
        assert_eq!(actual.into_iter().collect::<Vec<_>>(), vec![42]);
    }
    #[test]
    fn bulk_retain_bounds_sparse_spill_capacity() {
        let mut values = IdentitySet::default();
        values.extend(0..8192);
        values.retain(|value| *value >= 8192 - 1000);
        let capacity = values.spill.as_ref().map_or(0, HashSet::capacity);
        assert!(capacity <= 4 * values.len());
        values.retain(|value| *value == 8191);
        let capacity = values.spill.as_ref().map_or(0, HashSet::capacity);
        assert!(capacity <= 4 * values.len());
        for _ in 0..8192 {
            values.retain(|_| true);
        }
        assert_eq!(values.into_iter().collect::<Vec<_>>(), vec![8191]);
    }
}
