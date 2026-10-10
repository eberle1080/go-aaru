package aaru

import "testing"

func TestLRUCacheEviction(t *testing.T) {
	c := newLRUCache(100)
	c.add(1, "a", 40)
	c.add(2, "b", 40)
	if _, ok := c.get(1); !ok { // 1 becomes most recent
		t.Fatal("missing key 1")
	}
	c.add(3, "c", 40) // over budget: evicts LRU = 2
	if _, ok := c.get(2); ok {
		t.Fatal("key 2 should have been evicted")
	}
	if v, ok := c.get(1); !ok || v.(string) != "a" {
		t.Fatal("key 1 should survive")
	}
	if c.curBytes != 80 || c.len() != 2 {
		t.Fatalf("curBytes=%d len=%d", c.curBytes, c.len())
	}
	// Replacing keeps accounting correct.
	c.add(1, "A", 10)
	if c.curBytes != 50 {
		t.Fatalf("curBytes=%d after replace", c.curBytes)
	}
	// A single oversize entry is kept.
	c.add(4, "huge", 1000)
	if _, ok := c.get(4); !ok || c.len() != 1 {
		t.Fatalf("oversize entry should remain alone, len=%d", c.len())
	}
	c.clear()
	if c.len() != 0 || c.curBytes != 0 {
		t.Fatal("clear failed")
	}
}

func TestFiletime(t *testing.T) {
	ft := int64(133000000000000000)
	if got := TimeToFiletime(FiletimeToTime(ft)); got != ft {
		t.Fatalf("round trip %d != %d", got, ft)
	}
	if FiletimeToTime(116444736000000000).Unix() != 0 {
		t.Fatal("unix epoch mismatch")
	}
}
