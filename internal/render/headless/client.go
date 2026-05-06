package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jedi4ever/social-skills/internal/browser"
)

// DaemonClient talks to a running headless daemon (`social-fetch
// headless start`) or a social-browser pool (`social-browser daemon
// start`). Cheap to construct — no resources held until Fetch is
// called. Used transparently by Fetcher when a daemon is reachable;
// falls through to in-process spawn otherwise.
type DaemonClient struct {
	BaseURL string        // e.g. http://127.0.0.1:5556 — picked from candidates by Reachable
	HTTP    *http.Client  // override for tests; default has 90s Timeout
	Timeout time.Duration // per-request deadline (default 90s)
	// Token, when non-empty, is sent as Authorization: Bearer <token>
	// AND X-Daytona-Preview-Token: <token> on every request. Daytona's
	// signed proxy URLs accept either header form; we send both so the
	// same client reaches a self-hosted daemon (which may want plain
	// Bearer for its own auth) or a Daytona-tunneled one without
	// branching on URL shape.
	Token string
	// candidates holds the ordered list of URLs Reachable will probe
	// when no explicit SOCIAL_FETCH_HEADLESS_DAEMON_URL was given.
	// First responder wins; BaseURL is overwritten on hit. Empty when
	// the URL was pinned via env — Reachable then probes BaseURL only.
	candidates []string
}

// NewDaemonClient builds a client pointed at the configured daemon
// URL. SOCIAL_FETCH_HEADLESS_DAEMON_URL overrides; otherwise
// Reachable autodetects between two well-known loopback ports:
//
//	5560 — social-browser pool daemon (round-robins across a fleet)
//	5556 — social-fetch headless daemon (single-host chromedp)
//
// Pool wins when both are up so multi-instance setups beat a leftover
// single-host daemon. Operators who explicitly want the single-host
// path (e.g. session-affinity flows that can't tolerate round-robin)
// set SOCIAL_FETCH_HEADLESS_POOL_DISABLE=1 to skip the 5560 probe.
//
// SOCIAL_FETCH_HEADLESS_DAEMON_TOKEN, when set, attaches as a
// bearer + Daytona-preview header on every call — required when
// the URL points at a Daytona tunnel (`https://5556-<id>.daytonaproxy01.net`)
// or any other auth-gated reverse proxy.
func NewDaemonClient() *DaemonClient {
	token := strings.TrimSpace(os.Getenv("SOCIAL_FETCH_HEADLESS_DAEMON_TOKEN"))
	if url := strings.TrimSpace(os.Getenv("SOCIAL_FETCH_HEADLESS_DAEMON_URL")); url != "" {
		return &DaemonClient{
			BaseURL: url,
			Timeout: 90 * time.Second,
			Token:   token,
		}
	}
	// Default candidate chain — try the pool first, then fall back to
	// the single-host daemon. BaseURL stays at the fallback so callers
	// that skip Reachable() (rare, but possible in tests) keep today's
	// behaviour.
	single := fmt.Sprintf("http://127.0.0.1:%d", DefaultDaemonPort)
	candidates := []string{single}
	if !poolDisabled() {
		pool := fmt.Sprintf("http://127.0.0.1:%d", browser.DefaultDaemonPort)
		candidates = []string{pool, single}
	}
	return &DaemonClient{
		BaseURL:    single,
		Timeout:    90 * time.Second,
		Token:      token,
		candidates: candidates,
	}
}

// poolDisabled reports whether SOCIAL_FETCH_HEADLESS_POOL_DISABLE is
// set to a truthy value. Same lax parser as the rest of the env-flag
// surface in this repo: empty / "0" / "false" / "no" mean "leave the
// pool probe enabled."
func poolDisabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SOCIAL_FETCH_HEADLESS_POOL_DISABLE"))) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// applyAuth adds bearer + Daytona-preview headers to req when
// the client has a token. No-op for local daemons.
func (c *DaemonClient) applyAuth(req *http.Request) {
	if c.Token == "" {
		return
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("X-Daytona-Preview-Token", c.Token)
}

// Reachable does a cheap GET /status to check whether the daemon is
// alive. Used by Fetcher.Fetch to decide between daemon-mode and
// in-process spawn. ~50 ms when up locally, ~connection-refused-fast
// when down. Cap at 1.5s per candidate — covers cross-region
// Daytona-tunnel latency (~500ms RTT EU↔US) plus TLS overhead on the
// proxy. The previous 250ms cap silently routed every Daytona-remote
// daemon call to in-process spawn because the probe always timed out.
//
// When the client was built without an explicit URL, walks the
// candidate list (pool 5560 → single-host 5556 by default) and pins
// BaseURL to the first responder. Subsequent calls (Fetch /
// Screenshot / Status) hit that URL directly without re-probing.
func (c *DaemonClient) Reachable(ctx context.Context) bool {
	candidates := c.candidates
	if len(candidates) == 0 {
		candidates = []string{c.BaseURL}
	}
	for _, url := range candidates {
		if c.probe(ctx, url) {
			c.BaseURL = url
			return true
		}
	}
	return false
}

// probe is the per-URL half of Reachable — single GET /status with a
// 1.5s cap. Split out so Reachable can iterate candidates without
// duplicating the http.Client + auth-header dance.
func (c *DaemonClient) probe(ctx context.Context, url string) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, url+"/status", nil)
	if err != nil {
		return false
	}
	c.applyAuth(req)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 1500 * time.Millisecond}
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == http.StatusOK
}

// Fetch sends the URL to the daemon's /fetch endpoint and unwraps
// the response. Returns the same Result shape as in-process Fetch
// so callers can swap implementations without branching on origin.
func (c *DaemonClient) Fetch(ctx context.Context, url string) (*Result, error) {
	return c.FetchWithSettle(ctx, url, 0)
}

// FetchWithSettle is Fetch with a per-call settle override. settle
// of 0 falls back to the daemon's configured default (today: 2s).
// Used by the article fetcher's thin-content retry path —
// "retry the same URL with a longer hydration wait."
func (c *DaemonClient) FetchWithSettle(ctx context.Context, url string, settle time.Duration) (*Result, error) {
	body, err := json.Marshal(fetchRequest{URL: url})
	if err != nil {
		return nil, err
	}

	timeout := c.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := c.BaseURL + "/fetch"
	if settle > 0 {
		endpoint += "?settle=" + settle.String()
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("daemon: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}
	var fr fetchResponse
	if err := json.NewDecoder(resp.Body).Decode(&fr); err != nil {
		return nil, fmt.Errorf("daemon: decode: %w", err)
	}
	return &Result{
		HTML:     fr.HTML,
		FinalURL: fr.FinalURL,
		Engine:   fr.Engine + "+daemon",
	}, nil
}

// Screenshot POSTs to the daemon's /screenshot endpoint and returns
// the PNG bytes wrapped in a ScreenshotResult. settle of 0 falls back
// to the daemon's configured default; fullPage=true matches the
// in-process default.
func (c *DaemonClient) Screenshot(ctx context.Context, url string, settle time.Duration, fullPage bool) (*ScreenshotResult, error) {
	body, err := json.Marshal(screenshotRequest{URL: url})
	if err != nil {
		return nil, err
	}

	timeout := c.Timeout
	if timeout == 0 {
		timeout = 90 * time.Second
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := c.BaseURL + "/screenshot"
	q := []string{}
	if !fullPage {
		q = append(q, "full_page=0")
	}
	if settle > 0 {
		q = append(q, "settle="+settle.String())
	}
	if len(q) > 0 {
		endpoint += "?" + strings.Join(q, "&")
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("daemon: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("daemon: read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Body is plain-text on error (http.Error from the daemon).
		msg := strings.TrimSpace(string(respBody))
		if len(msg) > 1024 {
			msg = msg[:1024]
		}
		return nil, fmt.Errorf("daemon: HTTP %d: %s", resp.StatusCode, msg)
	}
	finalURL := resp.Header.Get("X-Final-URL")
	if finalURL == "" {
		finalURL = url
	}
	return &ScreenshotResult{
		PNG:      respBody,
		FinalURL: finalURL,
		Engine:   "chromedp+daemon",
	}, nil
}

// StatusResponse is the parsed /status JSON, exported so CLI
// commands can format it without re-decoding. Field names match
// the JSON tags on the daemon's internal statusResponse so tests
// and CLI use the same shape.
type StatusResponse = statusResponse

// SlotState is the per-slot snapshot exported for CLI rendering.
type SlotState = slotState

// FetchEntry is one row in the recent-fetch history.
type FetchEntry = fetchEntry

// Status hits GET /status and returns the parsed response. Used by
// the `social-fetch headless status` CLI subcommand.
func (c *DaemonClient) Status(ctx context.Context) (*StatusResponse, error) {
	probeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, c.BaseURL+"/status", nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var s statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, err
	}
	return &s, nil
}
