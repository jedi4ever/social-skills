// Package htmlmd provides pluggable HTML→Markdown conversion. Two
// shapes of provider live here:
//
//   - Converter: HTML fragment in, markdown string out. Local,
//     deterministic, no I/O. Two implementations:
//
//   - KaufmannConverter (default) — wraps
//     github.com/JohannesKaufmann/html-to-markdown/v2, with the
//     core commonmark plugin enabled (tables, strikethrough, etc.).
//     Significantly better edge-case coverage than the hand-roll.
//
//   - BuiltinConverter — the legacy in-tree hand-roll using only
//     golang.org/x/net/html. Kept as a fallback for restricted
//     builds where pulling the new dependency isn't desired, and
//     as a regression baseline.
//
//   - Reader: URL in, markdown string out. Service-backed, network
//     I/O. One implementation today: JinaReader (r.jina.ai). Useful
//     when the local fetch path is blocked by Cloudflare/JS-rendered
//     SPAs or when you want a pre-cleaned body without parsing.
//
// Selection at runtime:
//
//   - HTML2MD_PROVIDER = kaufmann (default) | builtin — picks the
//     local Converter used by article extraction.
//   - HTML2MD_READER = local (default) | jina — picks the article
//     fetcher's URL→markdown path. "local" means the existing
//     fetch + Converter pipeline; "jina" replaces it with a single
//     JinaReader call.
package htmlmd

import (
	"os"
	"strings"
	"sync"
)

var (
	defaultOnce      sync.Once
	defaultConverter Converter
)

// Converter turns an HTML fragment into markdown. Implementations
// must be safe for concurrent use — the article extractors call
// Convert from many goroutines in batch mode.
type Converter interface {
	Convert(htmlFragment string) string
}

// Default returns the Converter selected by the HTML2MD_PROVIDER env
// var, falling back to KaufmannConverter when the variable is unset
// or unrecognized.
//
// Cached on first call so subsequent calls don't re-parse env or
// reinitialize the Kaufmann library state.
func Default() Converter {
	defaultOnce.Do(func() {
		defaultConverter = pickConverter(os.Getenv("HTML2MD_PROVIDER"))
	})
	return defaultConverter
}

// Convert is sugar for `Default().Convert(s)` — preserves the call
// shape used throughout the codebase before the pluggable system
// landed.
func Convert(htmlFragment string) string {
	return Default().Convert(htmlFragment)
}

func pickConverter(name string) Converter {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "builtin", "legacy", "internal":
		return &BuiltinConverter{}
	case "", "kaufmann", "default":
		return &KaufmannConverter{}
	}
	// Unknown value → fall back to default rather than panicking.
	// The audit log doesn't reach this layer; users notice the wrong
	// converter when output looks off.
	return &KaufmannConverter{}
}
