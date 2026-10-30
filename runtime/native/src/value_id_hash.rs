use std::hash::{BuildHasherDefault, Hasher};

pub(crate) type IdentityBuildHasher = BuildHasherDefault<IdentityHasher>;

/// Hashes runtime-owned value addresses, whose bits never come from Lo input.
#[derive(Default)]
pub(crate) struct IdentityHasher(u64);

impl Hasher for IdentityHasher {
    fn finish(&self) -> u64 {
        self.0
    }
    fn write(&mut self, _: &[u8]) {
        unreachable!("value identity keys are usize addresses")
    }
    fn write_usize(&mut self, value: usize) {
        let mut value = value as u64;
        value = (value ^ (value >> 30)).wrapping_mul(0xbf58476d1ce4e5b9);
        value = (value ^ (value >> 27)).wrapping_mul(0x94d049bb133111eb);
        self.0 = value ^ (value >> 31);
    }
}

#[cfg(test)]
mod tests {
    use super::IdentityBuildHasher;
    use std::{
        collections::HashSet,
        hash::{BuildHasher, Hash, Hasher},
    };

    #[test]
    fn aligned_and_high_bit_addresses_mix_without_losing_identity() {
        let builder = IdentityBuildHasher::default();
        let mut keys = HashSet::new();
        keys.extend([0usize, usize::MAX]);
        for bit in 0..usize::BITS {
            keys.insert(1usize << bit);
        }
        for index in 0..4096 {
            keys.insert(0x100000000 + index * 208);
        }
        let mut hashes = HashSet::new();
        let mut buckets = HashSet::new();
        for key in keys {
            let mut hasher = builder.build_hasher();
            key.hash(&mut hasher);
            let hash = hasher.finish();
            assert!(hashes.insert(hash));
            assert_eq!(builder.hash_one(key), hash);
            buckets.insert(hash >> 56);
        }
        assert!(buckets.len() >= 240);
    }
}
