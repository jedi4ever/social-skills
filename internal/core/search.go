// Package search defines the Provider interface that backends — DuckDuckGo,
// SerpAPI, others — implement. A SearchResult is intentionally tiny: just enough
// to feed back into the fetch pipeline.
package core

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// SearchResult is one search hit.
type SearchResult struct {
	Title     string     `json:"title"`
	URL       string     `json:"url"`
	Snippet   string     `json:"snippet,omitempty"`
	Source    string     `json:"source"`
	Published *time.Time `json:"published,omitempty"`
}

// Options shape a single search call. Date and domain filters are
// best-effort: providers that don't support a native filter ignore it;
// providers with coarse granularity (Tavily's "last N days") round to
// the closest supported window.
type SearchOptions struct {
	Max            int        // max results; <=0 means provider default
	Before         *time.Time // only results published before this time
	After          *time.Time // only results published after this time
	IncludeDomains []string   // allowlist; if non-empty, restrict to these
	ExcludeDomains []string   // denylist

	// Start is the pagination offset (0-based result index, not page
	// number). Providers that support offset paging respect this;
	// ones that don't ignore it. SerpAPI / HackerNews / arXiv /
	// Brave / Google CSE all support offset pagination natively.
	Start int

	// Cursor is the opaque page token for cursor-paginated
	// providers (Reddit, X, YouTube, Bluesky). On the first call
	// leave it empty; the provider's SearchPaged returns a
	// next_cursor in the result envelope when more results exist.
	// Pass that token back as Cursor on the next call to continue.
	//
	// Cursor and Start are independent — providers use one or the
	// other, never both. Calling a cursor-only provider with Start
	// set ignores Start; calling an offset-only provider with
	// Cursor set ignores Cursor.
	Cursor string
}

// SearchPage is the cursor-aware return shape for providers that
// implement CursorPaginator. Callers that don't care about cursors
// can keep using SearchProvider.Search directly — the simpler
// []SearchResult shape stays the canonical interface. Cursor-
// based providers OPT IN by implementing both Search() (which calls
// SearchPaged internally and discards the cursor) and SearchPaged()
// (which surfaces it).
type SearchPage struct {
	Results    []SearchResult `json:"results"`
	NextCursor string         `json:"next_cursor,omitempty"`
}

// CursorPaginator is implemented by providers whose APIs paginate
// via opaque tokens (Reddit `after=`, X `next_token`, YouTube
// `pageToken`, Bluesky `cursor`). Offset-paginated providers
// (SerpAPI, HackerNews, arXiv, Brave, Google CSE) don't need this —
// they take opts.Start instead and never produce a cursor.
//
// Callers detect cursor support with a type assertion on
// SearchProvider:
//
//	if cp, ok := p.(core.CursorPaginator); ok {
//	    page, err := cp.SearchPaged(ctx, query, opts)
//	    // page.NextCursor is populated when more pages exist
//	}
type CursorPaginator interface {
	SearchPaged(ctx context.Context, query string, opts SearchOptions) (*SearchPage, error)
}

// DefaultOptions returns options with the provider's own defaults.
func DefaultSearchOptions() SearchOptions { return SearchOptions{} }

// Provider performs queries against a backend.
type SearchProvider interface {
	Name() string
	Search(ctx context.Context, query string, opts SearchOptions) ([]SearchResult, error)
}

// Registry holds the known search providers.
type SearchRegistry struct {
	providers []SearchProvider
}

func NewSearchRegistry(providers ...SearchProvider) *SearchRegistry {
	return &SearchRegistry{providers: providers}
}

// searchAliases maps common synonyms to canonical provider names.
// Picks up CLI users typing `-p twitter` (canonical is `x`) and
// agents that infer the wrong name from a tool description. Keep the
// list short — surface area to maintain. Lowercase keys + values.
var searchAliases = map[string]string{
	"twitter": "x",
	"tweet":   "x",
	"hn":      "hackernews",
	"ddg":     "duckduckgo",
	"bsky":    "bluesky",
}

// Get returns the named provider, or an error listing the known names.
func (r *SearchRegistry) Get(name string) (SearchProvider, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if alias, ok := searchAliases[name]; ok {
		name = alias
	}
	for _, p := range r.providers {
		if strings.ToLower(p.Name()) == name {
			return p, nil
		}
	}
	return nil, fmt.Errorf("unknown search provider %q (known: %s)", name, strings.Join(r.Names(), ", "))
}

func (r *SearchRegistry) Names() []string {
	out := make([]string, 0, len(r.providers))
	for _, p := range r.providers {
		out = append(out, p.Name())
	}
	return out
}

func (r *SearchRegistry) Providers() []SearchProvider {
	out := make([]SearchProvider, len(r.providers))
	copy(out, r.providers)
	return out
}
