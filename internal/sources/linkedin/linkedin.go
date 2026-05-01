// Package linkedin fetches a LinkedIn post by routing through the local
// bridge (internal/bridge) — the user's logged-in browser does the
// actual page render, then the bridge streams the resulting HTML back.
//
// Why a bridge instead of a direct HTTP fetch? LinkedIn's public
// endpoints don't return post content without an authenticated session,
// and JavaScript-rendered DOM is required for the post body. The
// extension running in the user's logged-in browser handles both.
package linkedin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/patrickdebois/social-skills/internal/core"
	"github.com/patrickdebois/social-skills/internal/htmlmd"
)

// DefaultBridgeURL is the local bridge endpoint the fetcher POSTs to.
// Override for tests via Fetcher.BridgeURL.
const DefaultBridgeURL = "http://127.0.0.1:5555/cmd"

type Fetcher struct {
	BridgeURL string
}

func New() *Fetcher { return &Fetcher{BridgeURL: DefaultBridgeURL} }

func (Fetcher) Name() string { return "linkedin" }

func (Fetcher) Match(u *url.URL) bool {
	if u == nil {
		return false
	}
	host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
	if host != "linkedin.com" && !strings.HasSuffix(host, ".linkedin.com") {
		return false
	}
	p := u.Path
	return strings.Contains(p, "/posts/") ||
		strings.Contains(p, "/feed/update/") ||
		strings.HasPrefix(p, "/in/") ||
		strings.HasPrefix(p, "/pulse/")
}

// reply mirrors the extension's get_html response.
type reply struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
	HTML   string `json:"html"`
	URL    string `json:"url"`
	Title  string `json:"title"`
}

func (f *Fetcher) Fetch(ctx context.Context, raw string, opts core.Options) (*core.Item, error) {
	endpoint := f.BridgeURL
	if endpoint == "" {
		endpoint = DefaultBridgeURL
	}

	// Two-step: explicit `navigate` first, then `get_html`. The
	// extension's tab matcher broadens to origin-only patterns, so
	// `get_html` alone may scrape whichever LinkedIn tab is already
	// open. Forcing navigate first ensures we read the requested URL.
	opts.Audit.Logf("linkedin: bridge navigate %s", raw)
	if _, err := f.bridgeCall(ctx, endpoint, map[string]any{
		"command": "navigate",
		"url":     raw,
	}); err != nil {
		return nil, err
	}

	opts.Audit.Logf("linkedin: bridge get_html")
	respBody, err := f.bridgeCall(ctx, endpoint, map[string]any{
		"command": "get_html",
		"url":     raw,
	})
	if err != nil {
		return nil, err
	}

	var r reply
	if err := json.Unmarshal(respBody, &r); err != nil {
		return nil, fmt.Errorf("linkedin: decode bridge reply: %w", err)
	}
	if r.Status != "ok" {
		return nil, fmt.Errorf("linkedin: extension error: %s", r.Error)
	}
	if r.HTML == "" {
		return nil, fmt.Errorf("linkedin: extension returned empty HTML for %s", raw)
	}

	finalURL := r.URL
	if finalURL == "" {
		finalURL = raw
	}
	cleanedHTML, comments := cleanHTML(r.HTML)
	body2 := trimBoilerplate(htmlmd.Convert(cleanedHTML))

	author, authorURL := extractAuthor(r.HTML)
	canonical := canonicalID(finalURL)

	return &core.Item{
		Source:      "linkedin",
		Kind:        kindFor(finalURL),
		URL:         finalURL,
		CanonicalID: canonical,
		Title:       firstLine(pickFirst(r.Title, body2), 120),
		Author:      author,
		AuthorURL:   authorURL,
		Content:     body2,
		Comments:    comments,
		FetchedAt:   time.Now().UTC(),
		Extra: map[string]any{
			"via":           "bridge",
			"comment_count": len(comments),
		},
	}, nil
}

// bridgeCall posts a single JSON command to the bridge and returns the
// raw reply body. Centralizes error mapping so callers don't repeat the
// 503/timeout/non-2xx handling.
func (f *Fetcher) bridgeCall(ctx context.Context, endpoint string, payload map[string]any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := core.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("linkedin: bridge unreachable (start it with `socialfetch bridge` and connect the extension): %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, fmt.Errorf("linkedin: bridge has no extension connected — open your browser with the PatAI extension running")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("linkedin: bridge returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

// canonicalID pulls the activity / ugcPost numeric id out of a LinkedIn
// URL when present, so dedup and JSON consumers have a stable key.
var idRE = regexp.MustCompile(`(?:activity|ugcPost)[-:](\d{15,})`)

func canonicalID(raw string) string {
	if m := idRE.FindStringSubmatch(raw); len(m) >= 2 {
		return m[1]
	}
	return ""
}

func kindFor(raw string) string {
	switch {
	case strings.Contains(raw, "/in/"):
		return "profile"
	case strings.Contains(raw, "/pulse/"):
		return "article"
	default:
		return "post"
	}
}

// extractAuthor pulls the most useful author signal we can from the raw
// HTML without parsing the whole document. LinkedIn injects an
// `og:title` plus an actor-name span; we look for both and pick the
// first that yields a sensible value. Returns (display name, profile URL).
var ogTitleRE = regexp.MustCompile(`<meta[^>]+property=["']og:title["'][^>]+content=["']([^"']+)["']`)
var actorURLRE = regexp.MustCompile(`href=["']([^"']*linkedin\.com/in/[^"'?]+)`)

func extractAuthor(html string) (name, profile string) {
	if m := ogTitleRE.FindStringSubmatch(html); len(m) >= 2 {
		// og:title looks like "Jane Doe on LinkedIn: …"; trim the suffix.
		name = strings.TrimSpace(m[1])
		if i := strings.Index(name, " on LinkedIn"); i > 0 {
			name = name[:i]
		}
	}
	if m := actorURLRE.FindStringSubmatch(html); len(m) >= 2 {
		profile = strings.TrimRight(m[1], "/")
		if !strings.HasPrefix(profile, "http") {
			profile = "https://www." + strings.TrimPrefix(profile, "//")
		}
	}
	return
}

// trimBoilerplate drops the LinkedIn chrome (cookie banners, "Sign in to
// view more", CTA buttons) that survive cleanHTML because they live in
// regular markup with no class signal. We strip matching substrings,
// drop empty markdown anchors, and collapse repeated lines.
func trimBoilerplate(md string) string {
	deny := []string{
		"Sign in to view more",
		"Skip to main content",
		"Cookie Policy",
		"User Agreement",
		"Privacy Policy",
		"Continue with Google",
		"Join now",
		"New to LinkedIn?",
		"Add section",
		"View my services",
		"Create a post",
		"Contact info",
		"Cover photo",
		"More",
		"Sign in",
	}
	out := md
	for _, d := range deny {
		out = strings.ReplaceAll(out, d, "")
	}

	// Drop empty-text markdown anchors: "[](url)" and "[](#)".
	out = emptyAnchorRE.ReplaceAllString(out, "")

	// Strip leading whitespace on each line — htmlmd preserves the
	// nested-div indentation LinkedIn uses, which produces lines that
	// start with 6-12 spaces. Markdown only treats indentation as
	// meaningful inside code blocks (which we don't have here).
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
		// Don't strip leading whitespace on list items / blockquotes —
		// those are syntactically meaningful.
		trim := strings.TrimLeft(lines[i], " \t")
		if strings.HasPrefix(trim, "- ") || strings.HasPrefix(trim, "* ") ||
			strings.HasPrefix(trim, "> ") || strings.HasPrefix(trim, "#") {
			lines[i] = trim
		} else {
			lines[i] = strings.TrimLeft(lines[i], " \t")
		}
	}

	// Dedup adjacent identical non-empty lines (LinkedIn often renders
	// the same CTA two or three times in the same card).
	deduped := lines[:0]
	var prev string
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" && t == prev {
			continue
		}
		deduped = append(deduped, l)
		prev = strings.TrimSpace(l)
	}
	out = strings.Join(deduped, "\n")

	// Collapse runs of blank lines left behind.
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}

var emptyAnchorRE = regexp.MustCompile(`\[\]\([^)]*\)`)

func firstLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\n\r"); i >= 0 {
		s = s[:i]
	}
	if len(s) > max {
		s = s[:max-1] + "…"
	}
	return s
}

func pickFirst(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
