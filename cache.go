package ip2country

import (
	"container/list"
	"sync"
	"sync/atomic"
)

// cacheEntry holds the data for a single cached lookup result.
// Fields are ordered for optimal memory alignment.
type cacheEntry struct {
	country string
	code    string
	ip      uint32
	found   bool // Used to cache misses as well.
}

// cacheItem is the object stored in the LRU list.
// Fields are ordered for optimal memory alignment.
type cacheItem struct {
	value cacheEntry
	key   uint32
}

// lruCache is a thread-safe, in-memory LRU (Least Recently Used) cache.
type lruCache struct {
	mu        sync.Mutex
	capacity  int
	items     map[uint32]*list.Element
	evictList *list.List
	hits      int64
	misses    int64
}

// newLRUCache creates a new LRU cache with the given capacity.
func newLRUCache(capacity int) *lruCache {
	return &lruCache{
		capacity:  capacity,
		items:     make(map[uint32]*list.Element),
		evictList: list.New(),
	}
}

// get retrieves a value from the cache.
func (c *lruCache) get(key uint32) (cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.evictList.MoveToFront(elem)
		atomic.AddInt64(&c.hits, 1)
		return elem.Value.(*cacheItem).value, true
	}

	atomic.AddInt64(&c.misses, 1)
	return cacheEntry{}, false
}

// put adds or updates a key-value pair in the cache.
func (c *lruCache) put(key uint32, value cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.evictList.MoveToFront(elem)
		elem.Value.(*cacheItem).value = value
		return
	}

	if c.evictList.Len() >= c.capacity {
		c.removeOldest()
	}

	item := &cacheItem{key: key, value: value}
	elem := c.evictList.PushFront(item)
	c.items[key] = elem
}

// removeOldest removes the least recently used item from the cache.
func (c *lruCache) removeOldest() {
	elem := c.evictList.Back()
	if elem != nil {
		c.evictList.Remove(elem)
		item := elem.Value.(*cacheItem)
		delete(c.items, item.key)
	}
}

// clear removes all items from the cache.
func (c *lruCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[uint32]*list.Element)
	c.evictList.Init()
	atomic.StoreInt64(&c.hits, 0)
	atomic.StoreInt64(&c.misses, 0)
}

// getStats returns the current number of cache hits and misses.
func (c *lruCache) getStats() (hits, misses int64) {
	return atomic.LoadInt64(&c.hits), atomic.LoadInt64(&c.misses)
}
