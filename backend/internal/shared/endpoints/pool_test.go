package endpoints

import (
	"sync"
	"testing"
)

func TestPool_SingleURL(t *testing.T) {
	p := NewPool("http://a")
	for i := 0; i < 3; i++ {
		if got := p.Next(); got != "http://a" {
			t.Errorf("call %d: got %q, want http://a", i, got)
		}
	}
}

func TestPool_RoundRobin(t *testing.T) {
	p := NewPool("http://a, http://b, http://c")
	want := []string{"http://a", "http://b", "http://c", "http://a", "http://b"}
	for i, w := range want {
		if got := p.Next(); got != w {
			t.Errorf("call %d: got %q, want %q", i, got, w)
		}
	}
}

func TestPool_DedupeAndTrim(t *testing.T) {
	p := NewPool("  http://a , http://b , http://a , ")
	if got := p.Size(); got != 2 {
		t.Errorf("Size = %d, want 2", got)
	}
	if got := p.All(); got[0] != "http://a" || got[1] != "http://b" {
		t.Errorf("All = %v, want [http://a http://b]", got)
	}
}

func TestPool_EmptyString(t *testing.T) {
	p := NewPool("")
	if got := p.Size(); got != 0 {
		t.Errorf("Size = %d, want 0", got)
	}
	if got := p.Next(); got != "" {
		t.Errorf("Next = %q, want empty", got)
	}
}

func TestPool_Fallback_MultipleHosts(t *testing.T) {
	p := NewPool("http://a, http://b, http://c")
	got := p.Fallback("http://a")
	if got == "http://a" {
		t.Errorf("Fallback returned the failed host: %q", got)
	}
}

func TestPool_Fallback_Single(t *testing.T) {
	p := NewPool("http://only")
	if got := p.Fallback("http://only"); got != "http://only" {
		t.Errorf("Single-host fallback should return self, got %q", got)
	}
}

func TestPool_ConcurrentRoundRobin(t *testing.T) {
	p := NewPool("http://a, http://b, http://c, http://d")
	const total = 1000
	var wg sync.WaitGroup
	counts := make([]int, len(p.All()))
	mu := sync.Mutex{}
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u := p.Next()
			mu.Lock()
			defer mu.Unlock()
			for idx, listed := range p.All() {
				if listed == u {
					counts[idx]++
					return
				}
			}
		}()
	}
	wg.Wait()
	// Each entry should be picked total/4 = 250 times. Allow
	// ±5% slack for any (currently impossible given the atomic
	// counter, but worth asserting).
	for i, c := range counts {
		if c < 220 || c > 280 {
			t.Errorf("entry %d picked %d times; want ~%d ±slack", i, c, total/len(counts))
		}
	}
}
