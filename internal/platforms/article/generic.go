package article

import (
	"strings"

	"github.com/jedi4ever/social-skills/internal/core"
	"github.com/jedi4ever/social-skills/internal/util/htmlmeta"
)

// genericArticleSelectors is the catch-all selector list — same set
// htmlmeta uses by default. Listed here so callers can inspect or extend
// it.
var genericArticleSelectors = []string{
	"article",
	"main",
	"[role=main]",
	".post-content",
	".entry-content",
	".article-body",
	".article-content",
	"#content",
	".content",
}

// GenericExtractor is the fallback for any host without a dedicated
// extractor. It uses the broadest selector list and adds no extras.
type GenericExtractor struct{}

func (*GenericExtractor) Name() string           { return "generic" }
func (*GenericExtractor) Match(host string) bool { return true }

func (g *GenericExtractor) Extract(rawURL string, page *htmlmeta.Page) (*core.Item, error) {
	item := BaseFromPage(rawURL, page, "article")
	item.Content = RenderArticle(page, genericArticleSelectors, item.Summary)

	// Append body-embedded images. The generic article fetcher
	// runs against arbitrary blogs, so the host matcher accepts
	// any absolute http(s) URL — we don't know which CDN host(s)
	// the blog uses. The chrome denylist + size threshold +
	// dedupe-against-hero in AppendBodyImages do the filtering.
	AppendBodyImages(item, page, genericArticleSelectors, anyHTTPHost)
	return item, nil
}

// anyHTTPHost accepts any absolute http(s) URL. Used by the generic
// article extractor — we can't know in advance which CDN host an
// arbitrary blog uses, so we accept anything that looks like a real
// remote resource and let the chrome / size filters do the work.
// `data:` URIs and protocol-relative or relative URLs get dropped.
func anyHTTPHost(src string) bool {
	low := strings.ToLower(src)
	return strings.HasPrefix(low, "http://") || strings.HasPrefix(low, "https://")
}
