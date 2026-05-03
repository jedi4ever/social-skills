package article

import (
	"strings"
	"testing"

	"golang.org/x/net/html"

	"github.com/jedi4ever/social-skills/internal/core"
	"github.com/jedi4ever/social-skills/internal/util/htmlmeta"
)

// TestAppendBodyImages exercises the shared helper across the
// behaviours every consumer relies on:
//
//  1. Images on a matching CDN host get appended.
//  2. Images on a non-matching host get dropped.
//  3. Hero (already on item.Media from BaseFromPage) is deduped.
//  4. Chrome ancestors (avatar / sidebar / related) drop the image.
//  5. Lazy-loaded images pick up data-src instead of src placeholder.
//  6. Sub-threshold dimension drops the image.
//  7. SOCIAL_FETCH_MIN_IMAGE_SIZE shifts the threshold at runtime.
func TestAppendBodyImages(t *testing.T) {
	cases := []struct {
		name        string
		fragment    string
		preExisting []core.Media // already-on-item Media (e.g. hero)
		host        HostMatcher
		envSize     string
		wantURLs    []string
		wantDropped []string
	}{
		{
			name: "matching host kept",
			fragment: `<article>
                <img src="https://cdn.example.com/hero.jpg" alt="Hero" width="800" height="600">
                <img src="https://cdn.example.com/figure.jpg" alt="Figure" width="800" height="450">
            </article>`,
			host: func(src string) bool { return strings.Contains(src, "cdn.example.com") },
			wantURLs: []string{
				"https://cdn.example.com/hero.jpg",
				"https://cdn.example.com/figure.jpg",
			},
		},
		{
			name: "non-matching host dropped",
			fragment: `<article>
                <img src="https://cdn.example.com/keep.jpg" width="800" height="600">
                <img src="https://imgur.com/skip.jpg" width="800" height="600">
            </article>`,
			host:        func(src string) bool { return strings.Contains(src, "cdn.example.com") },
			wantURLs:    []string{"https://cdn.example.com/keep.jpg"},
			wantDropped: []string{"https://imgur.com/skip.jpg"},
		},
		{
			name: "hero is deduped against pre-existing Media",
			fragment: `<article>
                <img src="https://cdn.example.com/hero.jpg" width="800" height="600">
                <img src="https://cdn.example.com/figure.jpg" width="800" height="600">
            </article>`,
			preExisting: []core.Media{
				{URL: "https://cdn.example.com/hero.jpg", Type: "image"},
			},
			host: func(src string) bool { return strings.Contains(src, "cdn.example.com") },
			wantURLs: []string{
				"https://cdn.example.com/hero.jpg",   // already there
				"https://cdn.example.com/figure.jpg", // newly appended
			},
		},
		{
			name: "chrome ancestor drops image",
			fragment: `<article>
                <div class="related-posts">
                    <img src="https://cdn.example.com/related.jpg" width="800" height="600">
                </div>
                <img src="https://cdn.example.com/figure.jpg" width="800" height="600">
            </article>`,
			host:        func(src string) bool { return strings.Contains(src, "cdn.example.com") },
			wantURLs:    []string{"https://cdn.example.com/figure.jpg"},
			wantDropped: []string{"https://cdn.example.com/related.jpg"},
		},
		{
			name: "lazy load picks up data-src",
			fragment: `<article>
                <img src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"
                     data-src="https://cdn.example.com/lazy.jpg"
                     width="800" height="600">
            </article>`,
			host:     func(src string) bool { return strings.Contains(src, "cdn.example.com") },
			wantURLs: []string{"https://cdn.example.com/lazy.jpg"},
		},
		{
			name: "sub-threshold dropped at default 64",
			fragment: `<article>
                <img src="https://cdn.example.com/icon.jpg" width="32" height="32">
                <img src="https://cdn.example.com/figure.jpg" width="800" height="600">
            </article>`,
			host:        func(src string) bool { return strings.Contains(src, "cdn.example.com") },
			wantURLs:    []string{"https://cdn.example.com/figure.jpg"},
			wantDropped: []string{"https://cdn.example.com/icon.jpg"},
		},
		{
			name: "env var bumps threshold to 200, drops mid-size",
			fragment: `<article>
                <img src="https://cdn.example.com/medium.jpg" width="120" height="120">
                <img src="https://cdn.example.com/large.jpg" width="800" height="600">
            </article>`,
			host:        func(src string) bool { return strings.Contains(src, "cdn.example.com") },
			envSize:     "200",
			wantURLs:    []string{"https://cdn.example.com/large.jpg"},
			wantDropped: []string{"https://cdn.example.com/medium.jpg"},
		},
		{
			name: "no dimension hint kept (better to over-include than drop real images)",
			fragment: `<article>
                <img src="https://cdn.example.com/no-dim.jpg" alt="No dim hint">
            </article>`,
			host:     func(src string) bool { return strings.Contains(src, "cdn.example.com") },
			wantURLs: []string{"https://cdn.example.com/no-dim.jpg"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.envSize != "" {
				t.Setenv("SOCIAL_FETCH_MIN_IMAGE_SIZE", c.envSize)
			} else {
				t.Setenv("SOCIAL_FETCH_MIN_IMAGE_SIZE", "")
			}
			doc, err := html.Parse(strings.NewReader("<html><body>" + c.fragment + "</body></html>"))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			page := &htmlmeta.Page{Doc: doc}
			item := &core.Item{Media: append([]core.Media(nil), c.preExisting...)}
			AppendBodyImages(item, page, []string{"article"}, c.host)

			gotURLs := make([]string, len(item.Media))
			for i, m := range item.Media {
				gotURLs[i] = m.URL
			}

			if len(gotURLs) != len(c.wantURLs) {
				t.Fatalf("got %d media (%v), want %d (%v)",
					len(gotURLs), gotURLs, len(c.wantURLs), c.wantURLs)
			}
			for i, u := range c.wantURLs {
				if gotURLs[i] != u {
					t.Errorf("URL[%d] = %q, want %q", i, gotURLs[i], u)
				}
			}
			for _, dropped := range c.wantDropped {
				for _, u := range gotURLs {
					if u == dropped {
						t.Errorf("expected to drop %q, but it was kept", dropped)
					}
				}
			}
		})
	}
}

// TestBestImageSrc covers the lazy-loading priority: data-src wins
// over data-original wins over data-lazy-src wins over srcset wins
// over plain src.
func TestBestImageSrc(t *testing.T) {
	cases := []struct {
		name string
		attr [][2]string
		want string
	}{
		{"plain src", [][2]string{{"src", "https://e.com/a.jpg"}}, "https://e.com/a.jpg"},
		{"data-src wins over src", [][2]string{
			{"src", "data:image/gif;base64,xxx"},
			{"data-src", "https://e.com/real.jpg"},
		}, "https://e.com/real.jpg"},
		{"srcset first URL", [][2]string{
			{"srcset", "https://e.com/1x.jpg 1x, https://e.com/2x.jpg 2x"},
		}, "https://e.com/1x.jpg"},
		{"data-original beats srcset", [][2]string{
			{"data-original", "https://e.com/orig.jpg"},
			{"srcset", "https://e.com/1x.jpg 1x"},
		}, "https://e.com/orig.jpg"},
		{"empty when nothing set", [][2]string{}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := &html.Node{Type: html.ElementNode, Data: "img"}
			for _, a := range c.attr {
				n.Attr = append(n.Attr, html.Attribute{Key: a[0], Val: a[1]})
			}
			if got := bestImageSrc(n); got != c.want {
				t.Errorf("bestImageSrc = %q, want %q", got, c.want)
			}
		})
	}
}
