package execution

const ownershipCacheSize = 8

type cachedOwned[K comparable] struct {
	key   K
	entry *owned[K]
}

// ownership keeps recent receipts in fixed slots and evicted live receipts in
// the cold map. Pinned receipts are excluded from iteration candidate scans.
type ownership[K comparable] struct {
	cold    map[K]*owned[K]
	current *ownershipScope[K]
	root    ownershipScope[K]
	cache   [][]K
	spare   []*owned[K]
	nextPin uint64
	recent  [16]cachedOwned[K]
	count   int
	release func(K, uint64)
}
type owned[K comparable] struct {
	owner    *ownershipScope[K]
	weight   uint64
	pins     uint64
	position int
}
type ownershipPin[K comparable] struct {
	release func()
	keys    []K
}
type ownershipScope[K comparable] struct {
	ledger     *ownership[K]
	parent     *ownershipScope[K]
	candidates []K
	pins       map[uint64]ownershipPin[K]
	pinnedKeys int
}

func newOwnership[K comparable](release func(K, uint64)) *ownership[K] {
	ledger := &ownership[K]{cold: map[K]*owned[K]{}, release: release}
	ledger.root.ledger = ledger
	ledger.current = &ledger.root
	return ledger
}
func (ledger *ownership[K]) enter(scope *ownershipScope[K]) {
	scope.ledger, scope.parent = ledger, ledger.current
	ledger.current = scope
}
func (scope *ownershipScope[K]) candidate(key K) {
	if scope.candidates == nil {
		cache := scope.ledger.cache
		if len(cache) == 0 {
			scope.candidates = make([]K, 0, ownershipCacheSize)
		} else {
			scope.candidates = cache[len(cache)-1]
			cache[len(cache)-1] = nil
			scope.ledger.cache = cache[:len(cache)-1]
		}
	}
	entry := scope.ledger.lookup(key)
	entry.position = len(scope.candidates)
	scope.candidates = append(scope.candidates, key)
}
func (scope *ownershipScope[K]) remove(entry *owned[K]) {
	position := entry.position
	if position < 0 {
		position = ^position
	}
	last := len(scope.candidates) - 1
	if position != last {
		key := scope.candidates[last]
		scope.candidates[position] = key
		moved := scope.ledger.lookup(key)
		// A negative position marks a root during Keep. Preserve that mark.
		if moved.position < 0 {
			moved.position = ^position
		} else {
			moved.position = position
		}
	}
	var empty K
	scope.candidates[last] = empty
	scope.candidates = scope.candidates[:last]
}
func ownedIndex[K comparable](key K) int {
	if id, ok := any(key).(uint64); ok {
		return int(id & 15)
	}
	return 0
}
func (ledger *ownership[K]) lookup(key K) *owned[K] {
	cached := &ledger.recent[ownedIndex(key)]
	if cached.entry != nil && cached.key == key {
		return cached.entry
	}
	return ledger.cold[key]
}
func (ledger *ownership[K]) add(key K, weight uint64) {
	var entry *owned[K]
	if len(ledger.spare) == 0 {
		entry = &owned[K]{}
	} else {
		last := len(ledger.spare) - 1
		entry = ledger.spare[last]
		ledger.spare[last] = nil
		ledger.spare = ledger.spare[:last]
	}
	*entry = owned[K]{owner: ledger.current, weight: weight}
	cached := &ledger.recent[ownedIndex(key)]
	if cached.entry != nil {
		ledger.cold[cached.key] = cached.entry
	}
	*cached = cachedOwned[K]{key, entry}
	ledger.count++
	ledger.current.candidate(key)
}
func (ledger *ownership[K]) drop(key K) {
	entry := ledger.lookup(key)
	if entry == nil {
		return
	}
	weight := entry.weight
	if entry.pins == 0 {
		entry.owner.remove(entry)
	}
	cached := &ledger.recent[ownedIndex(key)]
	if cached.key == key {
		*cached = cachedOwned[K]{}
	} else {
		delete(ledger.cold, key)
	}
	ledger.count--
	*entry = owned[K]{}
	if len(ledger.spare) < ownershipCacheSize {
		ledger.spare = append(ledger.spare, entry)
	}
	ledger.release(key, weight)
}
func (scope *ownershipScope[K]) keepKey(key K) {
	entry := scope.ledger.lookup(key)
	if entry != nil && entry.owner == scope && entry.pins == 0 && entry.position >= 0 {
		entry.position = ^entry.position
	}
}
func (scope *ownershipScope[K]) keep() {
	for index := 0; index < len(scope.candidates); {
		key := scope.candidates[index]
		entry := scope.ledger.lookup(key)
		if entry.position >= 0 {
			scope.ledger.drop(key)
		} else {
			entry.position = ^entry.position
			index++
		}
	}
}
func (scope *ownershipScope[K]) retainKey(key K) {
	entry := scope.ledger.lookup(key)
	if entry == nil || entry.owner != scope {
		return
	}
	if entry.pins == 0 {
		scope.remove(entry)
	}
	entry.owner = scope.parent
	if entry.pins == 0 {
		scope.parent.candidate(key)
	}
}
func releaseEmptyPin() {}

func (scope *ownershipScope[K]) pin(roots []K) func() {
	keys := roots[:0]
	for _, key := range roots {
		entry := scope.ledger.lookup(key)
		if entry == nil {
			continue
		}
		if entry.pins == 0 {
			entry.owner.remove(entry)
		}
		entry.pins++
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return releaseEmptyPin
	}
	scope.pinnedKeys += len(keys)
	scope.ledger.nextPin++
	id := scope.ledger.nextPin
	if scope.pins == nil {
		scope.pins = map[uint64]ownershipPin[K]{}
	}
	released := false
	release := func() {
		if released {
			return
		}
		released = true
		scope.pinnedKeys -= len(keys)
		delete(scope.pins, id)
		for _, key := range keys {
			if entry := scope.ledger.lookup(key); entry != nil {
				entry.pins--
				if entry.pins == 0 {
					entry.owner.candidate(key)
				}
			}
		}
	}
	scope.pins[id] = ownershipPin[K]{release: release, keys: keys}
	return release
}
func (scope *ownershipScope[K]) releasePins() {
	for _, pin := range scope.pins {
		pin.release()
	}
}
func (scope *ownershipScope[K]) close() {
	for len(scope.candidates) != 0 {
		scope.ledger.drop(scope.candidates[len(scope.candidates)-1])
	}
	// Only small empty buffers are cached. Large traversals release their arrays.
	if scope.candidates != nil && cap(scope.candidates) <= ownershipCacheSize {
		scope.ledger.cache = append(scope.ledger.cache, scope.candidates)
	}
	scope.candidates = nil
	scope.ledger.current = scope.parent
}
