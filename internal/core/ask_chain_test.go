package core

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// stubAsker is a minimal Asker for chain tests. ans/err describe the
// behavior; calls counts how many times Ask was invoked so we can
// assert the chain stops after the first success.
type stubAsker struct {
	name  string
	ans   *Answer
	err   error
	calls int
}

func (s *stubAsker) Name() string { return s.name }
func (s *stubAsker) Ask(_ context.Context, q string, _ AskOptions) (*Answer, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	if s.ans == nil {
		return nil, nil
	}
	out := *s.ans
	out.Question = q
	out.Asked = time.Now()
	return &out, nil
}

func TestAskChainFirstWins(t *testing.T) {
	first := &stubAsker{name: "first", ans: &Answer{Provider: "first", Text: "ok"}}
	second := &stubAsker{name: "second", ans: &Answer{Provider: "second", Text: "also ok"}}

	chain := &AskChain{Askers: []Asker{first, second}}
	ans, err := chain.Ask(context.Background(), "q", AskOptions{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if ans.Provider != "first" {
		t.Errorf("got %q, want first", ans.Provider)
	}
	if first.calls != 1 || second.calls != 0 {
		t.Errorf("calls: first=%d second=%d (expected 1, 0)", first.calls, second.calls)
	}
}

func TestAskChainSkipsMissingKey(t *testing.T) {
	first := &stubAsker{name: "first", err: errors.New("first: API_KEY not set")}
	second := &stubAsker{name: "second", ans: &Answer{Provider: "second", Text: "won"}}

	chain := &AskChain{Askers: []Asker{first, second}}
	ans, err := chain.Ask(context.Background(), "q", AskOptions{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if ans.Provider != "second" {
		t.Errorf("got %q, want second", ans.Provider)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Errorf("calls: first=%d second=%d (expected 1, 1)", first.calls, second.calls)
	}
}

func TestAskChainSkipsEmptyAnswer(t *testing.T) {
	first := &stubAsker{name: "first", ans: &Answer{Provider: "first"}} // empty Text + Sources
	second := &stubAsker{name: "second", ans: &Answer{Provider: "second", Text: "got it"}}

	chain := &AskChain{Askers: []Asker{first, second}}
	ans, err := chain.Ask(context.Background(), "q", AskOptions{})
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if ans.Provider != "second" {
		t.Errorf("got %q, want second", ans.Provider)
	}
}

func TestAskChainAllFailReturnsLastError(t *testing.T) {
	first := &stubAsker{name: "first", err: errors.New("first: 500 internal")}
	second := &stubAsker{name: "second", err: errors.New("second: 502 bad gateway")}

	chain := &AskChain{Askers: []Asker{first, second}}
	_, err := chain.Ask(context.Background(), "q", AskOptions{})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "502 bad gateway") {
		t.Errorf("expected last error to bubble up, got: %v", err)
	}
}

func TestAskChainAllSkippedReturnsConfigError(t *testing.T) {
	first := &stubAsker{name: "first", err: errors.New("FIRST_KEY not set")}
	second := &stubAsker{name: "second", err: errors.New("SECOND_KEY not set")}

	chain := &AskChain{Askers: []Asker{first, second}}
	_, err := chain.Ask(context.Background(), "q", AskOptions{})
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "no API keys configured") {
		t.Errorf("expected key-config error, got: %v", err)
	}
}
