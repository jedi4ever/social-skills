package article

import (
	"strings"
	"testing"

	"github.com/jedi4ever/social-skills/internal/util/htmlmeta"
)

// TestGenericExtractorAppendsBodyImages confirms that the generic
// article extractor finds <img> tags inside the article container,
// drops chrome (avatars, related-post thumbnails), and surfaces the
// real body images on item.Media — same behaviour Medium and
// Substack inherit, exercised here against an arbitrary blog shape.
//
// The fixture mimics a typical blog post: og:image hero (already
// picked up by BaseFromPage), one in-body figure, one
// related-posts thumbnail (chrome — must be dropped), one author
// avatar (chrome — must be dropped).
func TestGenericExtractorAppendsBodyImages(t *testing.T) {
	const page = `<!DOCTYPE html>
<html><head>
  <title>Test article</title>
  <meta property="og:image" content="https://example.com/hero.jpg">
</head><body>
  <article>
    <div class="author-avatar">
      <img src="https://cdn.example.com/avatar.jpg" alt="Author" width="48" height="48">
    </div>
    <p>Article opening.</p>
    <figure>
      <img src="https://cdn.example.com/diagram.png" alt="System diagram" width="800" height="600">
    </figure>
    <p>More prose.</p>
    <div class="related-posts">
      <img src="https://cdn.example.com/related.jpg" width="200" height="120">
    </div>
  </article>
</body></html>`

	parsed, err := htmlmeta.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	item, err := (&GenericExtractor{}).Extract("https://example.com/post", parsed)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	urls := make([]string, len(item.Media))
	for i, m := range item.Media {
		urls[i] = m.URL
	}

	// Hero from og:image MUST be present.
	hasHero := false
	for _, u := range urls {
		if u == "https://example.com/hero.jpg" {
			hasHero = true
		}
	}
	if !hasHero {
		t.Errorf("hero (og:image) not in Media: %v", urls)
	}

	// Body diagram MUST be present (figure inside article container).
	hasDiagram := false
	for _, u := range urls {
		if u == "https://cdn.example.com/diagram.png" {
			hasDiagram = true
		}
	}
	if !hasDiagram {
		t.Errorf("body diagram not surfaced as Media: %v", urls)
	}

	// Author avatar must NOT be present.
	for _, u := range urls {
		if u == "https://cdn.example.com/avatar.jpg" {
			t.Errorf("author avatar should be dropped, but is present: %v", urls)
		}
	}

	// Related-posts thumbnail must NOT be present.
	for _, u := range urls {
		if u == "https://cdn.example.com/related.jpg" {
			t.Errorf("related-posts thumbnail should be dropped: %v", urls)
		}
	}
}

// TestAnyHTTPHost guards the generic extractor's permissive matcher —
// any http(s) URL is acceptable, but data: / protocol-relative /
// relative paths are not.
func TestAnyHTTPHost(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"https://example.com/img.jpg", true},
		{"http://example.com/img.jpg", true},
		{"HTTPS://EXAMPLE.COM/IMG.JPG", true}, // case-insensitive scheme
		{"//cdn.example.com/img.jpg", false},  // protocol-relative
		{"/img.jpg", false},                   // relative path
		{"data:image/png;base64,xxx", false},
		{"javascript:void(0)", false},
		{"", false},
	}
	for _, c := range cases {
		if got := anyHTTPHost(c.src); got != c.want {
			t.Errorf("anyHTTPHost(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}
