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

// DaemonClient talks to a social-browser daemon (proxy on :5560
// fronting remote chromedp endpoints, OR local chromedp pool on
// the same port via `--provider local`). Cheap to construct — no
// resources held until Fetch is called.
type DaemonClient struct {
	BaseURL string        // default http://127.0.0.1:5560
	HTTP    *http.Client  // override for tests; default has 90s Timeout
	Timeout time.Duration // per-request deadline (default 90s)
	// Token, when non-empty, is sent as Authorization: Bearer <token>
	// AND X-Daytona-Preview-Token: <token> on every request. Daytona's
	// signed proxy URLs accept either header form; we send both so the
	// same client reaches a self-hosted daemon (which may want plain
	// Bearer for its own auth) or a Daytona-tunneled one without
	// branching on URL shape.
	Token string
}

// NewDaemonClient builds a client pointed at the social-browser
// daemon. SOCIAL_FETCH_HEADLESS_DAEMON_URL overrides; default is
// http://127.0.0.1:5560 — what `social-browser daemon start`
// listens on by default.
//
// SOCIAL_FETCH_HEADLESS_DAEMON_TOKEN, when set, attaches as a
// bearer + Daytona-preview header on every call — required when
// the URL points at a Daytona tunnel
// (`https://5556-<id>.daytonaproxy01.net`) or any other auth-gated
// reverse proxy. Local-pool deployments don't need a token.
//
// Pre-v0.15.0 versions probed both :5560 and :5556 to support the
// retired single-host `social-fetch headless` daemon. That path is
// gone; the client now only knows about :5560.
func NewDaemonClient() *DaemonClient {
	token := strings.TrimSpace(os.Getenv("SOCIAL_FETCH_HEADLESS_DAEMON_TOKEN"))
	url := strings.TrimSpace(os.Getenv("SOCIAL_FETCH_HEADLESS_DAEMON_URL"))
	if url == "" {
		url = fmt.Sprintf("http://127.0.0.1:%d", browser.DefaultDaemonPort)
	}
	return &DaemonClient{
		BaseURL: url,
		Timeout: 90 * time.Second,
		Token:   token,
	}
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

// Reachable does a cheap GET /status to check whether the daemon
// is alive. ~50 ms when up locally, ~connection-refused-fast when
// down. Cap at 1.5s — covers cross-region Daytona-tunnel latency
// (~500ms RTT EU↔US) plus TLS overhead on the proxy. The previous
// 250ms cap silently failed every Daytona-remote daemon call
// because the probe always timed out.
func (c *DaemonClient) Reachable(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, c.BaseURL+"/status", nil)
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

// fetchRequest / fetchResponse / screenshotRequest are the wire
// shapes the daemon expects on /fetch and /screenshot. Mirrored from
// internal/browser/local/daemon.go (chromedp pool) and
// internal/browser/daemon.go (proxy) — both sides agree on this shape.
type fetchRequest struct {
	URL string `json:"url"`
}

type fetchResponse struct {
	HTML     string `json:"html"`
	FinalURL string `json:"final_url"`
	Engine   string `json:"engine"`
}

type screenshotRequest struct {
	URL string `json:"url"`
}
