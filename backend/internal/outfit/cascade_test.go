package outfit

import (
	"context"
	"errors"
	"io"
	"log"
	"strings"
	"testing"
)

// fakeGen is a controllable Generator for cascade tests. Each call
// to Generate returns whatever's next in the responses queue.
type fakeGen struct {
	name      string
	calls     int
	responses []fakeResp
}

type fakeResp struct {
	outfits []Outfit
	usage   *Usage
	err     error
}

func (f *fakeGen) Name() string { return f.name }

func (f *fakeGen) Generate(_ context.Context, _ GeneratorRequest) ([]Outfit, *Usage, error) {
	idx := f.calls
	f.calls++
	if idx >= len(f.responses) {
		return nil, nil, errors.New("fakeGen: out of programmed responses")
	}
	r := f.responses[idx]
	return r.outfits, r.usage, r.err
}

func quietLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func TestCascade_FirstSuccess(t *testing.T) {
	want := []Outfit{{Name: "Pick A"}}
	first := &fakeGen{name: "first", responses: []fakeResp{{outfits: want}}}
	second := &fakeGen{name: "second"}

	c := NewCascadeGenerator(quietLogger(), first, second)
	got, _, err := c.Generate(context.Background(), GeneratorRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Pick A" {
		t.Errorf("expected outfit from first provider; got %v", got)
	}
	if first.calls != 1 {
		t.Errorf("first provider should have been called once; got %d", first.calls)
	}
	if second.calls != 0 {
		t.Errorf("second provider should not have been called; got %d", second.calls)
	}
}

func TestCascade_FallsThroughOnTransientError(t *testing.T) {
	first := &fakeGen{name: "first", responses: []fakeResp{{err: errors.New("503 Service Unavailable")}}}
	second := &fakeGen{name: "second", responses: []fakeResp{{outfits: []Outfit{{Name: "Pick B"}}}}}

	c := NewCascadeGenerator(quietLogger(), first, second)
	got, _, err := c.Generate(context.Background(), GeneratorRequest{})
	if err != nil {
		t.Fatalf("expected fall-through to succeed; got %v", err)
	}
	if len(got) != 1 || got[0].Name != "Pick B" {
		t.Errorf("expected outfit from second provider; got %v", got)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Errorf("expected both providers called once; got first=%d, second=%d", first.calls, second.calls)
	}
}

func TestCascade_FatalErrorShortCircuits(t *testing.T) {
	first := &fakeGen{name: "first", responses: []fakeResp{{err: ErrFatal{Inner: errors.New("invalid api key")}}}}
	second := &fakeGen{name: "second", responses: []fakeResp{{outfits: []Outfit{{Name: "should not see this"}}}}}

	c := NewCascadeGenerator(quietLogger(), first, second)
	_, _, err := c.Generate(context.Background(), GeneratorRequest{})
	if err == nil {
		t.Fatal("expected error to short-circuit, got nil")
	}
	var fatal ErrFatal
	if !errors.As(err, &fatal) {
		t.Errorf("expected ErrFatal, got %T: %v", err, err)
	}
	if second.calls != 0 {
		t.Errorf("fatal error should not fall through; second called %d times", second.calls)
	}
}

func TestCascade_AllFail(t *testing.T) {
	first := &fakeGen{name: "first", responses: []fakeResp{{err: errors.New("timeout")}}}
	second := &fakeGen{name: "second", responses: []fakeResp{{err: errors.New("network reset")}}}

	c := NewCascadeGenerator(quietLogger(), first, second)
	_, _, err := c.Generate(context.Background(), GeneratorRequest{})
	if err == nil {
		t.Fatal("expected error after exhausting chain")
	}
	if !strings.Contains(err.Error(), "network reset") {
		t.Errorf("expected last error to be wrapped; got %v", err)
	}
}

func TestCascade_ContextCancelledShortCircuits(t *testing.T) {
	first := &fakeGen{name: "first", responses: []fakeResp{{err: context.Canceled}}}
	second := &fakeGen{name: "second", responses: []fakeResp{{outfits: []Outfit{{Name: "should not see"}}}}}

	c := NewCascadeGenerator(quietLogger(), first, second)
	_, _, err := c.Generate(context.Background(), GeneratorRequest{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if second.calls != 0 {
		t.Errorf("context cancellation should not fall through; second called %d times", second.calls)
	}
}

func TestCascade_HealthCooldownSkipsProvider(t *testing.T) {
	// Programmed: first fails 3 times, second succeeds each time.
	flaky := &fakeGen{
		name: "flaky",
		responses: []fakeResp{
			{err: errors.New("oops")},
			{err: errors.New("oops")},
			{err: errors.New("oops")},
		},
	}
	steady := &fakeGen{
		name: "steady",
		responses: []fakeResp{
			{outfits: []Outfit{{Name: "ok"}}},
			{outfits: []Outfit{{Name: "ok"}}},
			{outfits: []Outfit{{Name: "ok"}}},
			{outfits: []Outfit{{Name: "ok"}}},
		},
	}

	c := NewCascadeGenerator(quietLogger(), flaky, steady)

	for i := 0; i < 3; i++ {
		if _, _, err := c.Generate(context.Background(), GeneratorRequest{}); err != nil {
			t.Fatalf("call %d unexpected err: %v", i, err)
		}
	}
	// After 3 consecutive failures the flaky provider should be in
	// cooldown — the next call should skip it entirely.
	if _, _, err := c.Generate(context.Background(), GeneratorRequest{}); err != nil {
		t.Fatalf("call 4 unexpected err: %v", err)
	}
	if flaky.calls != 3 {
		t.Errorf("flaky should have been skipped on call 4 due to cooldown; calls=%d", flaky.calls)
	}
	if steady.calls != 4 {
		t.Errorf("steady should have served all 4 calls; got %d", steady.calls)
	}

	// Health snapshot should reflect the cooldown.
	snap := c.HealthSnapshot()
	if snap["flaky"].UnhealthyUntil.IsZero() {
		t.Errorf("expected flaky to be marked unhealthy in snapshot")
	}
	if snap["steady"].Successes != 4 {
		t.Errorf("expected 4 successes for steady; got %d", snap["steady"].Successes)
	}
}

func TestCascade_EmptyChain(t *testing.T) {
	c := NewCascadeGenerator(quietLogger())
	_, _, err := c.Generate(context.Background(), GeneratorRequest{})
	if err == nil {
		t.Error("expected error on empty chain")
	}
}

func TestCascade_NameDescribesChain(t *testing.T) {
	c := NewCascadeGenerator(quietLogger(),
		&fakeGen{name: "claude"},
		&fakeGen{name: "openai"},
		&fakeGen{name: "ollama"},
	)
	got := c.Name()
	want := "cascade(claude > openai > ollama)"
	if got != want {
		t.Errorf("Name() = %q; want %q", got, want)
	}
}
