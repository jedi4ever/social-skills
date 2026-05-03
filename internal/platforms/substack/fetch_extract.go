package substack

import (
	"strings"

	"github.com/jedi4ever/social-skills/internal/core"
	"github.com/jedi4ever/social-skills/internal/platforms/article"
	"github.com/jedi4ever/social-skills/internal/util/htmlmeta"
)

// articleSelectors lists Substack's article body containers in
// priority order. ".body.markup" is the rendered prose; ".available-content"
// is what readers see before the paywall on locked posts.
var articleSelectors = []string{
	"div.body.markup",
	".body.markup",
	".available-content",
	".post-content",
	"article",
}

// Extractor handles substack.com and any subdomain (newsletters
// are usually `name.substack.com`, but custom domains exist too — those
// fall back to the generic extractor unless someone wires a CNAME map).
//
// Lives in this package so it can sit alongside the bridge-aware
// fetcher that uses it. The article package's catch-all fetcher no
// longer dispatches to a Substack extractor — substack.com URLs always
// route through this package's Fetcher first.
type Extractor struct{}

func (*Extractor) Name() string { return "substack" }

func (*Extractor) Match(host string) bool {
	return host == "substack.com" || strings.HasSuffix(host, ".substack.com")
}

func (s *Extractor) Extract(rawURL string, page *htmlmeta.Page) (*core.Item, error) {
	item := article.BaseFromPage(rawURL, page, "substack")
	item.Content = article.RenderArticle(page, articleSelectors, item.Summary)

	// Append body-embedded images. Substack serves figures from
	// substackcdn.com and the bucketeer-* / cloudfront-* CDN hosts
	// it routes through. Hero from BaseFromPage (og:image) is
	// deduped automatically.
	article.AppendBodyImages(item, page, articleSelectors, substackImageHost)

	// Substack-specific extras: subtitle, publication name, like &
	// comment counts. All optional.
	if n := htmlmeta.SelectFirst(page.Doc, "h3.subtitle-text"); n != nil {
		item.Extra["subtitle"] = strings.TrimSpace(htmlmeta.TextOf(n))
	} else if n := htmlmeta.SelectFirst(page.Doc, ".subtitle"); n != nil {
		item.Extra["subtitle"] = strings.TrimSpace(htmlmeta.TextOf(n))
	}
	if pub := page.Meta["og:site_name"]; pub != "" {
		item.Extra["publication"] = pub
	}
	if n := htmlmeta.SelectFirst(page.Doc, ".like-count"); n != nil {
		item.Extra["likes"] = strings.TrimSpace(htmlmeta.TextOf(n))
	}
	if n := htmlmeta.SelectFirst(page.Doc, ".post-ufi-comment-button"); n != nil {
		item.Extra["comment_count"] = strings.TrimSpace(htmlmeta.TextOf(n))
	}

	return item, nil
}

// substackImageHost is the article.HostMatcher for Substack body
// images. Substack rotates between several CDN hosts:
//
//   - substackcdn.com
//   - substack-post-media.s3.amazonaws.com
//   - bucketeer-*.s3.amazonaws.com (newer)
//   - *.cloudfront.net (rare; some custom-domain newsletters)
//
// External images embedded in posts (Twitter/X media, imgur, etc.)
// are skipped — they're often the most informative but live outside
// our CDN matcher. Could be relaxed once we have an external-image
// allowlist, but for now we err toward predictable behaviour.
func substackImageHost(src string) bool {
	low := strings.ToLower(src)
	return strings.Contains(low, "substackcdn.com") ||
		strings.Contains(low, "substack-post-media") ||
		(strings.Contains(low, "bucketeer-") && strings.Contains(low, ".s3.amazonaws.com"))
}
