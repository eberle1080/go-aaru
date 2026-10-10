package aaru

import "container/list"

// lruCache is a byte-budgeted least-recently-used cache keyed by uint64.
// A maxBytes of 0 means unlimited. The entry just inserted is never evicted.
type lruCache struct {
	maxBytes uint64
	curBytes uint64
	ll       *list.List // front = most recently used
	items    map[uint64]*list.Element
}

type lruEntry struct {
	key   uint64
	value any
	size  uint64
}

func newLRUCache(maxBytes uint64) *lruCache {
	return &lruCache{maxBytes: maxBytes, ll: list.New(), items: make(map[uint64]*list.Element)}
}

// get returns the cached value for key, marking it most recently used.
func (c *lruCache) get(key uint64) (any, bool) {
	if c == nil {
		return nil, false
	}
	if e, ok := c.items[key]; ok {
		c.ll.MoveToFront(e)
		return e.Value.(*lruEntry).value, true
	}
	return nil, false
}

// add inserts or replaces key, evicting least recently used entries while the
// cache is over budget.
func (c *lruCache) add(key uint64, value any, size uint64) {
	if c == nil {
		return
	}
	if e, ok := c.items[key]; ok {
		ent := e.Value.(*lruEntry)
		c.curBytes -= ent.size
		ent.value, ent.size = value, size
		c.curBytes += size
		c.ll.MoveToFront(e)
	} else {
		c.items[key] = c.ll.PushFront(&lruEntry{key: key, value: value, size: size})
		c.curBytes += size
	}
	if c.maxBytes == 0 {
		return
	}
	for c.curBytes > c.maxBytes && c.ll.Len() > 1 {
		back := c.ll.Back()
		if back == c.ll.Front() {
			break
		}
		c.remove(back)
	}
}

func (c *lruCache) remove(e *list.Element) {
	ent := e.Value.(*lruEntry)
	c.ll.Remove(e)
	delete(c.items, ent.key)
	c.curBytes -= ent.size
}

// clear drops every entry.
func (c *lruCache) clear() {
	if c == nil {
		return
	}
	c.ll.Init()
	clear(c.items)
	c.curBytes = 0
}

// len returns the number of cached entries.
func (c *lruCache) len() int {
	if c == nil {
		return 0
	}
	return c.ll.Len()
}
