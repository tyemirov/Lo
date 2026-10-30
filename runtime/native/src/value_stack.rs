const INLINE_CAPACITY: usize = 8;

pub(crate) struct PendingStack<T> {
    inline: [Option<T>; INLINE_CAPACITY],
    inline_len: usize,
    spill: Vec<T>,
}

impl<T> Default for PendingStack<T> {
    fn default() -> Self {
        Self {
            inline: std::array::from_fn(|_| None),
            inline_len: 0,
            spill: Vec::new(),
        }
    }
}

impl<T> PendingStack<T> {
    pub fn push(&mut self, value: T) {
        if self.inline_len < INLINE_CAPACITY {
            self.inline[self.inline_len] = Some(value);
            self.inline_len += 1;
        } else {
            self.spill.push(value);
        }
    }
    pub fn pop(&mut self) -> Option<T> {
        self.spill.pop().or_else(|| {
            if self.inline_len == 0 {
                None
            } else {
                self.inline_len -= 1;
                self.inline[self.inline_len].take()
            }
        })
    }
}

impl<T> Extend<T> for PendingStack<T> {
    fn extend<I: IntoIterator<Item = T>>(&mut self, values: I) {
        let values = values.into_iter();
        let available = INLINE_CAPACITY - self.inline_len;
        self.spill
            .reserve(values.size_hint().0.saturating_sub(available));
        for value in values {
            self.push(value);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::PendingStack;
    use std::{cell::Cell, rc::Rc};

    #[test]
    fn push_pop_and_extend_match_vec_order() {
        let mut actual = PendingStack::default();
        let mut expected = Vec::new();
        let mut random = 0x713b_u64;
        for step in 0..10000 {
            random = random.wrapping_mul(6364136223846793005).wrapping_add(1);
            if step % 31 == 0 {
                let count = ((random >> 32) % 32) as usize;
                actual.extend(step..step + count);
                expected.extend(step..step + count);
            } else if random & 1 == 0 {
                actual.push(step);
                expected.push(step);
            } else {
                assert_eq!(actual.pop(), expected.pop());
            }
        }
        while let Some(value) = expected.pop() {
            assert_eq!(actual.pop(), Some(value));
        }
        assert!(actual.pop().is_none());
    }

    #[test]
    fn pop_then_push_across_spill_keeps_lifo_order() {
        let mut values = PendingStack::default();
        values.extend(0..64);
        for value in (4..64).rev() {
            assert_eq!(values.pop(), Some(value));
        }
        values.extend(100..132);
        for value in (100..132).rev() {
            assert_eq!(values.pop(), Some(value));
        }
        for value in (0..4).rev() {
            assert_eq!(values.pop(), Some(value));
        }
        assert!(values.pop().is_none());
    }

    #[test]
    fn partial_pop_and_drop_release_each_owned_value_once() {
        struct Tracked(Rc<Cell<usize>>);
        impl Drop for Tracked {
            fn drop(&mut self) {
                self.0.set(self.0.get() + 1);
            }
        }
        let drops = Rc::new(Cell::new(0));
        let mut values = PendingStack::default();
        values.extend((0..64).map(|_| Tracked(drops.clone())));
        drop(values.pop());
        assert_eq!(drops.get(), 1);
        drop(values);
        assert_eq!(drops.get(), 64);
    }
}
