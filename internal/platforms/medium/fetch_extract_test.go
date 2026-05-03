package medium

import (
	"strings"
	"testing"

	"github.com/jedi4ever/social-skills/internal/util/htmlmeta"
)

// TestMediumImageHost confirms the host matcher accepts Medium's CDN
// shapes (cdn-images-1, cdn-images-2, miro) and rejects external
// hosts. Catches drift if Medium adds a new CDN host or renames an
// existing one — that'd silently drop body images otherwise.
func TestMediumImageHost(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"https://cdn-images-1.medium.com/max/1024/figure.png", true},
		{"https://cdn-images-2.medium.com/v2/resize:fit:1200/0*xyz.png", true},
		{"https://miro.medium.com/v2/resize:fit:1400/1*abc.png", true},
		{"https://cdn-images-12.medium.com/max/2400/img.png", true},
		{"https://imgur.com/external.jpg", false},
		{"https://i.imgur.com/external.jpg", false},
		{"https://medium.com/static/icon.svg", false}, // not a CDN host
		{"data:image/svg+xml;base64,xxx", false},
		{"", false},
	}
	for _, c := range cases {
		if got := mediumImageHost(c.src); got != c.want {
			t.Errorf("mediumImageHost(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}

// Verifies the Medium extractor picks the host-specific .pw-post-body
// container instead of falling back to the generic <article> body. If
// Medium ever renames .pw-post-body again this test surfaces it
// immediately.
func TestMediumExtractorPicksHostSelector(t *testing.T) {
	const html = `<!DOCTYPE html>
<html><body>
  <section class="pw-post-body"><p>per-host body</p></section>
  <article><p>generic fallback</p></article>
</body></html>`

	page, err := htmlmeta.Parse(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got, err := (&Extractor{}).Extract("https://medium.com/@a/x", page)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(got.Content, "per-host body") {
		t.Errorf("expected Medium selector hit, got: %q", got.Content)
	}
	if got.Source != "medium" {
		t.Errorf("source: %q, want medium", got.Source)
	}
}

func TestMediumExtractorMatch(t *testing.T) {
	ex := &Extractor{}
	cases := map[string]bool{
		"medium.com":             true,
		"alice.medium.com":       true,
		"engineering.medium.com": true,
		"example.com":            false,
		"medium.org":             false,
	}
	for host, want := range cases {
		if got := ex.Match(host); got != want {
			t.Errorf("Match(%q) = %v, want %v", host, got, want)
		}
	}
}
