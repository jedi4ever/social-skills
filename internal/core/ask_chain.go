package core

import (
	"context"
	"fmt"
	"strings"
)

// AskChain wraps an ordered list of Askers and tries them sequentially
// until one returns a non-empty answer. Lets callers configure
// graceful degradation:
//
//	perplexity (best when it works) → grok → openai → anthropic →
//	tavily → serpapi (last resort)
//
// Two failure modes get distinct treatment:
//
//   - Missing-key errors (e.g. "PERPLEXITY_API_KEY not set") — silent
//     skip, no audit log noise. The user explicitly didn't configure
//     this provider; treating it as a real error would spam logs on
//     every call.
//   - Real upstream errors (HTTP 4xx/5xx, decode errors, timeouts) —
//     logged via the audit logger so the operator knows degradation
//     happened, then continues to the next provider.
//
// "Empty answer" (nil error but no Text and no Sources) is also
// treated as a soft failure: the chain advances. Lets us recover from
// providers that nominally succeed but return nothing useful (Tavily
// "no results", SerpAPI "no AI Overview generated").
type AskChain struct {
	Askers []Asker
}

// NewAskChain builds a chain from an ordered name list, looking each
// up in the given registry. Unknown names return an error; missing
// (skipped at runtime due to env vars) is fine — that's the chain's
// whole point.
func NewAskChain(reg *AskRegistry, names []string) (*AskChain, error) {
	askers := make([]Asker, 0, len(names))
	for _, name := range names {
		a, err := reg.Get(name)
		if err != nil {
			return nil, err
		}
		askers = append(askers, a)
	}
	return &AskChain{Askers: askers}, nil
}

func (c *AskChain) Name() string {
	parts := make([]string, len(c.Askers))
	for i, a := range c.Askers {
		parts[i] = a.Name()
	}
	return "chain[" + strings.Join(parts, "→") + "]"
}

// Ask walks the chain. Returns the first non-empty answer; if every
// provider falls through, returns the last real (non-skip) error
// seen, or a generic "all providers exhausted" error if every
// provider was skipped.
func (c *AskChain) Ask(ctx context.Context, question string, opts AskOptions) (*Answer, error) {
	audit, _ := ctx.Value(auditCtxKey).(*AuditLogger)
	if audit == nil {
		audit = NewAuditLogger(nil)
	}
	var lastErr error
	skipped := 0
	for _, a := range c.Askers {
		ans, err := a.Ask(ctx, question, opts)
		if err != nil {
			if isMissingKeyErr(err) {
				audit.Logf("ask chain: skipping %s (no key)", a.Name())
				skipped++
				continue
			}
			audit.Logf("ask chain: %s failed (%v) — trying next", a.Name(), err)
			lastErr = err
			continue
		}
		if ans == nil || (strings.TrimSpace(ans.Text) == "" && len(ans.Sources) == 0) {
			audit.Logf("ask chain: %s returned empty — trying next", a.Name())
			continue
		}
		audit.Logf("ask chain: answered by %s", a.Name())
		return ans, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("ask chain: all providers failed; last error: %w", lastErr)
	}
	if skipped == len(c.Askers) {
		return nil, fmt.Errorf("ask chain: every provider was skipped (no API keys configured for %s)", c.Name())
	}
	return nil, fmt.Errorf("ask chain: every provider returned an empty answer")
}

// isMissingKeyErr matches the "FOO_API_KEY not set" / "missing
// credentials" shapes used by the platform packages. It's pattern-
// based rather than typed because errors here are flat strings — a
// proper sentinel would require touching every provider.
func isMissingKeyErr(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not set") ||
		strings.Contains(s, "no api key") ||
		strings.Contains(s, "missing credentials") ||
		strings.Contains(s, "credentials not configured")
}
