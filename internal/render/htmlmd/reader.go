package htmlmd

import (
	"context"
	"os"
	"strings"
	"sync"
)

// Reader is the interface for service-backed URL→markdown converters.
// Unlike Converter (which takes pre-fetched HTML), a Reader does the
// fetch itself — useful when the local fetch path is blocked
// (Cloudflare, JS-rendered SPAs) or when the service can do
// readability extraction better than the local pipeline.
//
// Implementations must respect the context for cancellation/deadline
// and be safe for concurrent use.
type Reader interface {
	Read(ctx context.Context, url string) (markdown string, err error)
}

// DefaultReader returns the URL→markdown reader selected by the
// HTML2MD_READER env var. Returns nil for the "local" sentinel —
// callers interpret that as "use the existing fetch + Converter
// pipeline rather than a service-backed reader".
//
// Cached on first call.
func DefaultReader() Reader {
	defaultReaderOnce.Do(func() {
		defaultReader = pickReader(os.Getenv("HTML2MD_READER"))
	})
	return defaultReader
}

// IsServiceBacked reports whether DefaultReader returned a real
// implementation (vs. nil for "local"). Sugar so callers don't have
// to nil-check.
func IsServiceBacked() bool {
	return DefaultReader() != nil
}

func pickReader(name string) Reader {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "jina":
		return NewJinaReader()
	case "", "local", "off", "none":
		return nil
	}
	// Unknown value → behave like "local" rather than failing builds.
	return nil
}

var (
	defaultReaderOnce sync.Once
	defaultReader     Reader
)
