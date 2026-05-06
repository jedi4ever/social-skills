package headless

// Config types shared between the daemon-only Fetcher in this package
// and the chromedp pool in internal/browser/local. Held here so callers
// can construct an Options struct without pulling chromedp into their
// import graph (the local daemon imports this package; clients don't
// import the local daemon).

import (
	"os"
	"strings"
	"time"
)

// Options shapes the headless fetch. Defaults match the Python
// downloader's stealth profile; operators override per-call via env
// vars (see OptionsFromEnv) or via a constructed Options struct.
type Options struct {
	// Headless toggles the visible-window mode. true = no window
	// (production default); false = a real Chrome window pops up
	// (useful for debugging — watch the page render).
	Headless bool

	// Timeout is the per-fetch deadline including browser launch
	// + navigation + content read. Chrome cold-start adds
	// ~500ms-1s; budget at least 30s for slow pages on top.
	Timeout time.Duration

	// UserAgent is the navigator.userAgent string. Empty means
	// "use chromedp's default" (which advertises HeadlessChrome —
	// trivially detectable). The default below is a real-Chrome UA
	// that paired with the stealth init script makes us look like a
	// regular browser.
	UserAgent string

	// Locale + Timezone shape Intl.Locale + Intl.DateTimeFormat —
	// some bot-detection scripts cross-check these against the IP
	// region. We default to en-US / America/New_York to match
	// what the Python code uses; override for non-US-region tests.
	Locale   string
	Timezone string

	// ViewportWidth + ViewportHeight set window.innerWidth /
	// innerHeight. Real-browser-like defaults (1920x1080) help the
	// stealth profile.
	ViewportWidth  int
	ViewportHeight int

	// Cookies is an optional list of cookies to inject before
	// navigation. The daemon-only Fetcher does NOT honour cookies
	// today — daemon mode is anonymous-only and rejects cookie
	// injection at the HTTP boundary. Field is kept on Options so
	// the local pool (internal/browser/local) can still read it
	// when constructing a slot.
	Cookies []Cookie

	// Settle is the time we sleep after `body` becomes ready,
	// before reading outerHTML. Pages that hydrate via JS (Medium,
	// most React/Next.js apps) finish DOMContentLoaded with an empty
	// article container — the actual prose appears 1-3s later.
	// Without a settle delay we'd see a near-empty body.
	//
	// Default 2s matches the Python downloader's random_delay(2, 4)
	// floor; bump higher for slow-hydrating sites via env var.
	Settle time.Duration

	// ExecPath overrides the Chrome / Chromium binary location.
	// Empty = chromedp auto-detects on PATH (works on macOS via
	// Chrome.app, on Linux via /usr/bin/google-chrome or chromium).
	ExecPath string
}

// Cookie is a single name/value/domain/path triple injected into
// the browser before navigation. The five fields cover what
// LinkedIn's li_at and similar session cookies need.
type Cookie struct {
	Name     string
	Value    string
	Domain   string // e.g. ".linkedin.com"
	Path     string // e.g. "/"
	HTTPOnly bool
	Secure   bool
}

// DefaultOptions is the single source of truth for headless fetch
// behaviour. Mirrors patai/providers/browser_common.py:
//
//   - Real-Chrome UA (no "HeadlessChrome" giveaway)
//   - 1920x1080 viewport
//   - en-US / America/New_York
//   - 60s timeout (matches the Jina default)
var DefaultOptions = Options{
	Headless:       true,
	Timeout:        60 * time.Second,
	UserAgent:      "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
	Locale:         "en-US",
	Timezone:       "America/New_York",
	ViewportWidth:  1920,
	ViewportHeight: 1080,
	Settle:         2 * time.Second,
}

// Result is what a successful fetch returns. HTML is the rendered
// outerHTML after the page is settled; FinalURL is the post-redirect
// URL the browser actually landed on (which can differ from the
// requested URL if the site redirects). Engine names which underlying
// driver served the request — useful in audit logs when we add
// playwright-go as an alternative.
type Result struct {
	HTML     string
	FinalURL string
	Engine   string
}

// ScreenshotOptions tunes a single Screenshot call. Zero values fall
// back to the in-package defaults — full-page true, default viewport
// from Fetcher.Options, no per-call settle override.
type ScreenshotOptions struct {
	// FullPage captures the entire scrollable page (chromedp's
	// FullScreenshot). When false, captures the viewport only
	// (chromedp.CaptureScreenshot). patai's url downloader defaults
	// to full-page; we mirror that.
	FullPage bool
	// Settle overrides Fetcher.Options.Settle for this call. Zero =
	// use the fetcher's configured settle.
	Settle time.Duration
	// ViewportWidth / ViewportHeight override the fetcher's viewport
	// for this call. Honoured only by the local pool (which can
	// reconfigure per-call); the daemon-only client ignores them
	// because the upstream pool's slots are launched at start with a
	// fixed viewport.
	ViewportWidth  int
	ViewportHeight int
}

// ScreenshotResult is the return shape from a Screenshot call. PNG is
// the encoded image bytes; FinalURL is the post-redirect URL the
// browser landed on; Engine names the path that produced the image
// ("chromedp" for the local pool, "chromedp+daemon" for a remote one).
type ScreenshotResult struct {
	PNG      []byte
	FinalURL string
	Engine   string
}

// OptionsFromEnv reads SOCIAL_FETCH_HEADLESS_* env vars and overlays
// them on DefaultOptions. Bad values fall through to defaults rather
// than failing — same fail-soft policy as the Jina knobs.
//
//	SOCIAL_FETCH_HEADLESS_HEADLESS    true (default) | false
//	SOCIAL_FETCH_HEADLESS_TIMEOUT     60s (default), any time.ParseDuration
//	SOCIAL_FETCH_HEADLESS_SETTLE      2s (default) — post-navigate hydration delay
//	SOCIAL_FETCH_HEADLESS_USER_AGENT  custom UA string
//	SOCIAL_FETCH_HEADLESS_EXEC_PATH   path to chrome/chromium binary
//
// Note: no auth-cookie env var. The headless transport is intended
// for anonymous fetches — LinkedIn's guest-preview, Medium's free
// excerpt, etc. all render fine without authentication. Callers
// that want to inject a session cookie programmatically (e.g. for
// a future auth-aware headless flow) can build an Options{Cookies:
// ...} directly.
func OptionsFromEnv() Options {
	opts := DefaultOptions
	if v := strings.TrimSpace(os.Getenv("SOCIAL_FETCH_HEADLESS_HEADLESS")); v != "" {
		switch strings.ToLower(v) {
		case "false", "0", "no", "off":
			opts.Headless = false
		case "true", "1", "yes", "on":
			opts.Headless = true
		}
	}
	if v := strings.TrimSpace(os.Getenv("SOCIAL_FETCH_HEADLESS_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			opts.Timeout = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("SOCIAL_FETCH_HEADLESS_SETTLE")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d >= 0 {
			opts.Settle = d
		}
	}
	if v := strings.TrimSpace(os.Getenv("SOCIAL_FETCH_HEADLESS_USER_AGENT")); v != "" {
		opts.UserAgent = v
	}
	if v := strings.TrimSpace(os.Getenv("SOCIAL_FETCH_HEADLESS_EXEC_PATH")); v != "" {
		opts.ExecPath = v
	}
	return opts
}
