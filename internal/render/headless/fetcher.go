package headless

// Fetcher is the public façade callers use for "render this URL via
// headless chromedp". From v0.15.0 onward it is daemon-only — every
// call goes through a social-browser daemon (proxy or local-pool).
// When no daemon is reachable, Fetch / Screenshot return a clear
// error pointing at `social-browser daemon start --provider local`
// instead of silently spawning chromedp in-process.
//
// Pre-v0.15.0 versions of this type spawned a fresh chromedp
// browser per call when no daemon was around. That path was deleted
// to consolidate chromedp in social-browser; see the v0.15.0
// changelog and CLAUDE.md "Versioning" for the rationale.

import (
	"context"
	"errors"
	"fmt"
)

// Fetcher is cheap to construct and stateless — the only state it
// owns is the Options struct (used to forward Settle / Cookies to the
// daemon). The DaemonClient is built per call so env-var changes
// (SOCIAL_FETCH_HEADLESS_DAEMON_URL etc.) take effect immediately.
type Fetcher struct {
	Options Options
}

// New builds a Fetcher with DefaultOptions overlaid by env vars.
func New() *Fetcher {
	return NewWithOptions(OptionsFromEnv())
}

// NewWithOptions builds a Fetcher from explicit options. Empty
// fields fall back to DefaultOptions equivalents so callers can set
// just the field they care about.
func NewWithOptions(opts Options) *Fetcher {
	if opts.Timeout == 0 {
		opts.Timeout = DefaultOptions.Timeout
	}
	if opts.UserAgent == "" {
		opts.UserAgent = DefaultOptions.UserAgent
	}
	if opts.Locale == "" {
		opts.Locale = DefaultOptions.Locale
	}
	if opts.Timezone == "" {
		opts.Timezone = DefaultOptions.Timezone
	}
	if opts.ViewportWidth == 0 {
		opts.ViewportWidth = DefaultOptions.ViewportWidth
	}
	if opts.ViewportHeight == 0 {
		opts.ViewportHeight = DefaultOptions.ViewportHeight
	}
	if opts.Settle == 0 {
		opts.Settle = DefaultOptions.Settle
	}
	return &Fetcher{Options: opts}
}

// daemonUnreachableError is what callers see when no daemon answers
// the autodetect probe. Worded so the resolution is obvious — most
// people will hit this once on first install and the error itself
// tells them what to type.
func daemonUnreachableError(baseURL string) error {
	return fmt.Errorf(
		"browser daemon not reachable at %s — start one with `social-browser daemon start --provider local` (or set SOCIAL_FETCH_HEADLESS_DAEMON_URL to a remote daemon)",
		baseURL,
	)
}

// Fetch sends raw to a reachable daemon's /fetch and returns the
// rendered HTML. Settle in Options is forwarded as a per-call
// query param when it differs from the default.
//
// Cookies are NOT supported in v0.15.0 — the daemon's chromedp pool
// is anonymous-only. Callers that need cookie injection (LinkedIn
// li_at, etc.) should use the bridge transport via the Chrome
// extension instead.
func (f *Fetcher) Fetch(ctx context.Context, raw string) (*Result, error) {
	if raw == "" {
		return nil, errors.New("headless: empty URL")
	}
	if len(f.Options.Cookies) > 0 {
		return nil, errors.New("headless: cookie injection is not supported by the daemon transport — use the bridge for authenticated fetches")
	}
	c := NewDaemonClient()
	if !c.Reachable(ctx) {
		return nil, daemonUnreachableError(c.BaseURL)
	}
	settle := f.Options.Settle
	if settle == DefaultOptions.Settle {
		// Don't forward the default — the daemon already applies it.
		settle = 0
	}
	return c.FetchWithSettle(ctx, raw, settle)
}

// Screenshot captures a PNG of raw via the daemon's /screenshot
// endpoint. Per-call viewport overrides on opts are ignored — the
// daemon's slots are launched with a fixed viewport at startup
// (this was a real divergence from the in-process path; documented
// rather than silently fudged).
func (f *Fetcher) Screenshot(ctx context.Context, raw string, opts ScreenshotOptions) (*ScreenshotResult, error) {
	if raw == "" {
		return nil, errors.New("headless: empty URL")
	}
	if len(f.Options.Cookies) > 0 {
		return nil, errors.New("headless: cookie injection is not supported by the daemon transport")
	}
	c := NewDaemonClient()
	if !c.Reachable(ctx) {
		return nil, daemonUnreachableError(c.BaseURL)
	}
	settle := opts.Settle
	if settle == 0 && f.Options.Settle != DefaultOptions.Settle {
		settle = f.Options.Settle
	}
	return c.Screenshot(ctx, raw, settle, opts.FullPage)
}
