package outfit

import "testing"

// #109 D2: the generation-slot semaphore must cap concurrency and refuse
// the overflow, then free a slot on release.
func TestGenSlot_CapsAndReleases(t *testing.T) {
	h := (&Handler{}).WithMaxConcurrent(2)

	if !h.acquireGenSlot() || !h.acquireGenSlot() {
		t.Fatal("first two acquires should succeed")
	}
	if h.acquireGenSlot() {
		t.Fatal("third acquire should be refused when cap=2 is full")
	}
	h.releaseGenSlot()
	if !h.acquireGenSlot() {
		t.Fatal("acquire should succeed after a release frees a slot")
	}
}

func TestGenSlot_DisabledWhenUnset(t *testing.T) {
	h := &Handler{} // no WithMaxConcurrent → cap disabled
	for i := 0; i < 100; i++ {
		if !h.acquireGenSlot() {
			t.Fatalf("acquire %d should always succeed when cap is disabled", i)
		}
	}
	// releaseGenSlot must be safe even with no semaphore.
	h.releaseGenSlot()
}

func TestGenSlot_ReleaseNeverPanicsOnOverRelease(t *testing.T) {
	h := (&Handler{}).WithMaxConcurrent(1)
	h.releaseGenSlot() // release without acquire — must not block or panic
	if !h.acquireGenSlot() {
		t.Fatal("acquire should still succeed after a stray release")
	}
}
