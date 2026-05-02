package core

import (
	"context"
	"fmt"
	"strings"
)

// SearchChain wraps an ordered list of SearchProviders and tries them
// sequentially until one returns non-empty results. Mirror of
// AskChain — same fall-through semantics:
//
//   - Missing-key error → silent skip (the user didn't configure
//     this provider on purpose).
//   - Real upstream error → audit-log it, try next.
//   - Zero results without error → still soft-fail and try next, so
//     the chain recovers when an early provider knows nothing about
//     the query (Tavily on a domain it doesn't index, X without a
//     valid handle, etc.).
//
// Use NewSearchChain to build one from an ordered name list against
// an existing SearchRegistry.
type SearchChain struct {
	Providers []SearchProvider
}

func NewSearchChain(reg *SearchRegistry, names []string) (*SearchChain, error) {
	providers := make([]SearchProvider, 0, len(names))
	for _, name := range names {
		p, err := reg.Get(name)
		if err != nil {
			return nil, err
		}
		providers = append(providers, p)
	}
	return &SearchChain{Providers: providers}, nil
}

func (c *SearchChain) Name() string {
	parts := make([]string, len(c.Providers))
	for i, p := range c.Providers {
		parts[i] = p.Name()
	}
	return "chain[" + strings.Join(parts, "→") + "]"
}

func (c *SearchChain) Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error) {
	audit, _ := ctx.Value(auditCtxKey).(*AuditLogger)
	if audit == nil {
		audit = NewAuditLogger(nil)
	}
	var lastErr error
	skipped := 0
	for _, p := range c.Providers {
		results, err := p.Search(ctx, query, opts)
		if err != nil {
			if isMissingKeyErr(err) {
				audit.Logf("search chain: skipping %s (no key)", p.Name())
				skipped++
				continue
			}
			audit.Logf("search chain: %s failed (%v) — trying next", p.Name(), err)
			lastErr = err
			continue
		}
		if len(results) == 0 {
			audit.Logf("search chain: %s returned 0 results — trying next", p.Name())
			continue
		}
		audit.Logf("search chain: %d results from %s", len(results), p.Name())
		return results, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("search chain: all providers failed; last error: %w", lastErr)
	}
	if skipped == len(c.Providers) {
		return nil, fmt.Errorf("search chain: every provider was skipped (no API keys configured for %s)", c.Name())
	}
	return nil, fmt.Errorf("search chain: every provider returned 0 results")
}
