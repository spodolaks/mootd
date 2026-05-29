package wardrobe

import "testing"

// #109 D2: the detection-slot semaphore must cap concurrency and refuse
// the overflow, then free a slot on release.
func TestDetectSlot_CapsAndReleases(t *testing.T) {
	h := (&Handler{}).WithMaxConcurrent(2)

	if !h.acquireDetectSlot() || !h.acquireDetectSlot() {
		t.Fatal("first two acquires should succeed")
	}
	if h.acquireDetectSlot() {
		t.Fatal("third acquire should be refused when cap=2 is full")
	}
	h.releaseDetectSlot()
	if !h.acquireDetectSlot() {
		t.Fatal("acquire should succeed after a release frees a slot")
	}
}

func TestDetectSlot_DisabledWhenUnset(t *testing.T) {
	h := &Handler{}
	for i := 0; i < 100; i++ {
		if !h.acquireDetectSlot() {
			t.Fatalf("acquire %d should always succeed when cap is disabled", i)
		}
	}
	h.releaseDetectSlot()
}
