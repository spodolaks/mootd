package outfit

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"
	"time"
)

// TestRunWithProgress_NoRaceTerminalIsLast exercises the #109 D1 fix:
// the heartbeat goroutine and the terminal stage must never write the
// SSE sink concurrently, and `done` must be the last event.
//
// The onProgress callback deliberately appends to a slice WITHOUT its
// own lock — exactly like the real SSE handler writing an
// http.ResponseWriter, which has no internal synchronisation. If the
// serialisation/join regresses, `go test -race` flags the concurrent
// append. Run with -race for full value.
func TestRunWithProgress_NoRaceTerminalIsLast(t *testing.T) {
	s := &Service{logger: log.New(io.Discard, "", 0), heartbeatInterval: time.Millisecond}

	var stages []ProgressStage
	onProgress := func(p GenerateProgress) error {
		stages = append(stages, p.Stage) // unsynchronised on purpose
		return nil
	}
	work := func(ctx context.Context) ([]Outfit, error) {
		time.Sleep(25 * time.Millisecond) // ~25 heartbeat ticks of contention
		return []Outfit{{Name: "x"}}, nil
	}

	out, err := s.runWithProgress(context.Background(), onProgress, work)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 outfit, got %d", len(out))
	}
	if len(stages) < 2 {
		t.Fatalf("expected connecting + done at minimum, got %v", stages)
	}
	if stages[0] != StageConnecting {
		t.Errorf("first stage should be connecting, got %q", stages[0])
	}
	if last := stages[len(stages)-1]; last != StageDone {
		t.Errorf("terminal stage must be done (no heartbeat after it), got %q", last)
	}
	// No streaming heartbeat may appear after the terminal done.
	for i, st := range stages {
		if st == StageDone && i != len(stages)-1 {
			t.Errorf("done at index %d but not last; stages=%v", i, stages)
		}
	}
}

func TestRunWithProgress_ErrorPathEmitsErrorLast(t *testing.T) {
	s := &Service{logger: log.New(io.Discard, "", 0), heartbeatInterval: time.Millisecond}
	var stages []ProgressStage
	onProgress := func(p GenerateProgress) error {
		stages = append(stages, p.Stage)
		return nil
	}
	work := func(ctx context.Context) ([]Outfit, error) {
		time.Sleep(10 * time.Millisecond)
		return nil, errors.New("boom")
	}
	_, err := s.runWithProgress(context.Background(), onProgress, work)
	if err == nil {
		t.Fatal("expected error")
	}
	if last := stages[len(stages)-1]; last != StageError {
		t.Errorf("terminal stage must be error, got %q", last)
	}
}

func TestRunWithProgress_NilCallbackRunsWorkDirectly(t *testing.T) {
	s := &Service{logger: log.New(io.Discard, "", 0)}
	called := false
	out, err := s.runWithProgress(context.Background(), nil, func(ctx context.Context) ([]Outfit, error) {
		called = true
		return []Outfit{{Name: "y"}}, nil
	})
	if err != nil || !called || len(out) != 1 {
		t.Fatalf("nil callback should call work directly; called=%v out=%v err=%v", called, out, err)
	}
}
