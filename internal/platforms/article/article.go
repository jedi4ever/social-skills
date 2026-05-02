// Package article handles generic HTML article pages — blog posts,
// news sites, and anything else that exposes useful Open Graph or
// schema.org metadata. It's the catch-all fetcher: it claims any http(s)
// URL not already grabbed by a more specific source.
//
// Per-host extractors (Medium, Substack) live in their own platform
// packages alongside their bridge-aware fetchers. Those packages reuse
// this package's BaseFromPage / RenderArticle helpers but own their
// site-specific selectors. The article package itself only ships the
// GenericExtractor.
package article

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/patrickdebois/social-skills/internal/core"
	"github.com/patrickdebois/social-skills/internal/render/htmlmd"
	"github.com/patrickdebois/social-skills/internal/util/htmlmeta"
)

// Extractor turns a parsed HTML page into a populated *core.Item. The
// interface is exported so platform packages with their own extractors
// (medium, substack) implement the same contract — useful when other
// code wants to handle a heterogeneous list of extractors uniformly.
type Extractor interface {
	Name() string
	Match(host string) bool
	Extract(rawURL string, page *htmlmeta.Page) (*core.Item, error)
}

// Fetcher pulls a URL, parses it, and runs it through GenericExtractor.
// Per-host fetchers (medium, substack) are registered before this in
// the top-level fetch registry so they claim their hosts first.
type Fetcher struct {
	extractor Extractor
}

func New() *Fetcher {
	return &Fetcher{
		extractor: &GenericExtractor{},
	}
}

func (Fetcher) Name() string { return "article" }

// Match accepts any http(s) URL. Because the registry consults fetchers
// in order, this should be registered LAST in the top-level fetch
// registry so more specific fetchers (hackernews, reddit, github,
// twitter, rss) get first dibs.
func (Fetcher) Match(u *url.URL) bool {
	return u != nil && (u.Scheme == "http" || u.Scheme == "https")
}

func (f *Fetcher) Fetch(ctx context.Context, raw string, opts core.Options) (*core.Item, error) {
	ctx = core.WithAudit(ctx, opts.Audit)

	// HTML2MD_READER=jina opts into a service-backed fetch path that
	// runs the URL through r.jina.ai for cleaning. Useful when the
	// site is behind Cloudflare or renders only via JS — Jina handles
	// both. Skips the local GetBytes + htmlmeta parse + extractor
	// chain entirely; we still build a metadata-bearing core.Item but
	// the body comes pre-cleaned as markdown.
	if reader := htmlmd.DefaultReader(); reader != nil {
		opts.Audit.Logf("article: routing via service-backed reader")
		md, err := reader.Read(ctx, raw)
		if err != nil {
			return nil, fmt.Errorf("article: %w", err)
		}
		return &core.Item{
			Source:      "article",
			Kind:        "article",
			URL:         raw,
			CanonicalID: raw,
			Content:     strings.TrimSpace(md),
			FetchedAt:   time.Now().UTC(),
			Extra: map[string]any{
				"requested_url": raw,
				"via":           "reader",
			},
		}, nil
	}

	body, err := core.GetBytes(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("article: %w", err)
	}
	page, err := htmlmeta.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("article: parse: %w", err)
	}

	host := hostOf(raw)

	// --generic-extraction is now a no-op for this fetcher (the only
	// extractor here IS the generic one) — kept as a logged signal so
	// the audit trail still records the user's intent. Per-host
	// extractors live in their own packages and have their own bypass.
	if opts.GenericExtraction {
		opts.Audit.Logf("article: forced generic extractor (host=%s)", host)
	} else {
		opts.Audit.Logf("article: %s extractor", f.extractor.Name())
	}
	return f.extractor.Extract(raw, page)
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Host, "www.")
}
