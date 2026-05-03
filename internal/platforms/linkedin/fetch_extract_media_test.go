package linkedin

import (
	"strings"
	"testing"

	"golang.org/x/net/html"
)

// TestExtractMedia covers the extraction matrix:
//
//   - Real post photo (media.licdn.com, full size) → kept
//   - Author avatar (static.licdn.com OR media.licdn.com inside an
//     `actor-image` ancestor) → dropped
//   - Reaction badge (tiny dim, inside `reactions` class) → dropped
//   - Lazy-loaded image (src="data:..." but data-delayed-url is real)
//     → URL pulled from data-delayed-url
//   - Video poster (inside class containing "video") → kept with
//     Type="video-poster"
//   - Comment-section image (inside `comment-` class) → dropped
//
// Each fixture is a minimal HTML fragment that mirrors the LinkedIn
// DOM shape — class names match real post markup so the chrome
// denylist gets exercised.
func TestExtractMedia(t *testing.T) {
	cases := []struct {
		name string
		// fragment is wrapped in a body tag at parse time so the
		// extractor walks a real document tree.
		fragment    string
		wantURLs    []string // expected URLs in extracted Media
		wantTypes   []string // matching Type values, same length
		wantDropped []string // URLs that must NOT appear
	}{
		{
			name: "post photo with descriptive alt",
			fragment: `<div class="feed-shared-update-v2__content">
                <div class="feed-shared-image">
                    <img src="https://media.licdn.com/dms/image/v2/D5605AQHabc/feedshare-shrink_2048_1536/0/img.jpg"
                         alt="A diagram of the AHE system architecture"
                         width="800" height="600">
                </div>
            </div>`,
			wantURLs:  []string{"https://media.licdn.com/dms/image/v2/D5605AQHabc/feedshare-shrink_2048_1536/0/img.jpg"},
			wantTypes: []string{"image"},
		},
		{
			name: "author avatar dropped",
			fragment: `<div class="actor-image presence-entity__image">
                <img src="https://media.licdn.com/dms/image/v2/D5603AQXyz/profile-displayphoto-shrink_400_400/0/avatar.jpg"
                     alt="Cole Medin">
            </div>
            <div class="feed-shared-image">
                <img src="https://media.licdn.com/dms/image/v2/D5605AQHpost/feedshare-shrink_2048_1536/0/post.jpg"
                     alt="Post photo" width="800" height="600">
            </div>`,
			wantURLs:    []string{"https://media.licdn.com/dms/image/v2/D5605AQHpost/feedshare-shrink_2048_1536/0/post.jpg"},
			wantTypes:   []string{"image"},
			wantDropped: []string{"https://media.licdn.com/dms/image/v2/D5603AQXyz/profile-displayphoto-shrink_400_400/0/avatar.jpg"},
		},
		{
			name: "reaction badge dropped via chrome class + tiny dim",
			fragment: `<div class="social-detail-social-counts__reactions">
                <img src="https://media.licdn.com/dms/image/sync/v2/D5527AQHrx/articleshare-shrink_24_24/0/like.png"
                     alt="like" width="24" height="24">
            </div>
            <div class="feed-shared-image">
                <img src="https://media.licdn.com/dms/image/v2/D5605AQHpost/feedshare-shrink_2048_1536/0/post.jpg"
                     alt="" width="800" height="600">
            </div>`,
			wantURLs:    []string{"https://media.licdn.com/dms/image/v2/D5605AQHpost/feedshare-shrink_2048_1536/0/post.jpg"},
			wantTypes:   []string{"image"},
			wantDropped: []string{"https://media.licdn.com/dms/image/sync/v2/D5527AQHrx/articleshare-shrink_24_24/0/like.png"},
		},
		{
			name: "lazy-loaded image picks up data-delayed-url",
			fragment: `<div class="feed-shared-image">
                <img src="data:image/gif;base64,R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"
                     data-delayed-url="https://media.licdn.com/dms/image/v2/D5605AQHlazy/feedshare-shrink_2048_1536/0/lazy.jpg"
                     alt="Lazy" width="800" height="600">
            </div>`,
			wantURLs:  []string{"https://media.licdn.com/dms/image/v2/D5605AQHlazy/feedshare-shrink_2048_1536/0/lazy.jpg"},
			wantTypes: []string{"image"},
		},
		{
			name: "video poster tagged separately",
			fragment: `<div class="feed-shared-linkedin-video">
                <img src="https://media.licdn.com/dms/image/v2/D5605AQHvid/feedshare-video-poster/0/poster.jpg"
                     alt="Video poster" width="800" height="450">
            </div>`,
			wantURLs:  []string{"https://media.licdn.com/dms/image/v2/D5605AQHvid/feedshare-video-poster/0/poster.jpg"},
			wantTypes: []string{"video-poster"},
		},
		{
			name: "comment-thread image dropped",
			fragment: `<div class="comments-comment-item__main-content">
                <img src="https://media.licdn.com/dms/image/v2/D5622AQHcomment/feedshare-shrink_800_800/0/comment.jpg"
                     alt="Comment image" width="800" height="600">
            </div>
            <div class="feed-shared-image">
                <img src="https://media.licdn.com/dms/image/v2/D5605AQHpost/feedshare-shrink_2048_1536/0/post.jpg"
                     alt="" width="800" height="600">
            </div>`,
			wantURLs:    []string{"https://media.licdn.com/dms/image/v2/D5605AQHpost/feedshare-shrink_2048_1536/0/post.jpg"},
			wantTypes:   []string{"image"},
			wantDropped: []string{"https://media.licdn.com/dms/image/v2/D5622AQHcomment/feedshare-shrink_800_800/0/comment.jpg"},
		},
		{
			name: "static.licdn.com (profile pic CDN) dropped — wrong host",
			fragment: `<div class="feed-shared-image">
                <img src="https://static.licdn.com/sc/h/c1tecdbnvk1c8na3hbpj7vhfk"
                     alt="logo" width="200" height="200">
            </div>`,
			wantURLs: nil, // no media — wrong CDN host
		},
		{
			name: "external (non-licdn) image dropped",
			fragment: `<div class="feed-shared-image">
                <img src="https://example.com/img.jpg" alt="external" width="800" height="600">
            </div>`,
			wantURLs: nil,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			doc, err := html.Parse(strings.NewReader("<html><body>" + c.fragment + "</body></html>"))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := extractMedia(doc)

			gotURLs := make([]string, len(got))
			gotTypes := make([]string, len(got))
			for i, m := range got {
				gotURLs[i] = m.URL
				gotTypes[i] = m.Type
			}

			if len(gotURLs) != len(c.wantURLs) {
				t.Fatalf("got %d media (%v), want %d (%v)",
					len(gotURLs), gotURLs, len(c.wantURLs), c.wantURLs)
			}
			for i, u := range c.wantURLs {
				if gotURLs[i] != u {
					t.Errorf("URL[%d] = %q, want %q", i, gotURLs[i], u)
				}
				if gotTypes[i] != c.wantTypes[i] {
					t.Errorf("Type[%d] = %q, want %q", i, gotTypes[i], c.wantTypes[i])
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

// TestMinImageSizeConfigurable verifies the SOCIAL_FETCH_MIN_IMAGE_SIZE
// env var actually shifts the dimension threshold isTinyImage uses,
// so operators can tune it for their content (drop thumbnails by
// setting it high, catch small-but-meaningful images by setting it
// low).
func TestMinImageSizeConfigurable(t *testing.T) {
	// Image with width=80 — kept by default (64 threshold), dropped
	// when threshold is bumped to 100.
	mkImg := func() *html.Node {
		return &html.Node{
			Type: html.ElementNode,
			Data: "img",
			Attr: []html.Attribute{
				{Key: "src", Val: "https://media.licdn.com/dms/image/v2/post.jpg"},
				{Key: "width", Val: "80"},
				{Key: "height", Val: "80"},
			},
		}
	}

	// Default threshold (64): 80×80 is NOT tiny.
	t.Setenv("SOCIAL_FETCH_MIN_IMAGE_SIZE", "")
	if isTinyImage(mkImg()) {
		t.Errorf("80×80 should not be tiny at default threshold")
	}

	// Bump threshold to 100: 80×80 IS now tiny.
	t.Setenv("SOCIAL_FETCH_MIN_IMAGE_SIZE", "100")
	if !isTinyImage(mkImg()) {
		t.Errorf("80×80 should be tiny when threshold=100")
	}

	// Drop threshold to 32: 80×80 is not tiny.
	t.Setenv("SOCIAL_FETCH_MIN_IMAGE_SIZE", "32")
	if isTinyImage(mkImg()) {
		t.Errorf("80×80 should not be tiny when threshold=32")
	}

	// Garbage value falls back to default.
	t.Setenv("SOCIAL_FETCH_MIN_IMAGE_SIZE", "frobnicator")
	if isTinyImage(mkImg()) {
		t.Errorf("80×80 should not be tiny when env is garbage (falls back to 64)")
	}
}

// TestMediaFromImg_TinyImageHints exercises the URL-based size
// heuristics — LinkedIn embeds dimensions in the asset URL when
// no width/height attribute is present (common on lazy-loaded
// thumbnails). The tinyImage check should catch them so they
// don't slip past the chrome filter.
func TestMediaFromImg_TinyImageHints(t *testing.T) {
	cases := []struct {
		src      string
		wantTiny bool
	}{
		{"https://media.licdn.com/dms/image/v2/D5605AQHbig/feedshare-shrink_2048_1536/0/img.jpg", false},
		{"https://media.licdn.com/dms/image/v2/D5605AQHsmall_h_48,w_48,c_fill/0/thumb.jpg", true},
		{"https://media.licdn.com/dms/image/v2/D5605AQHsmall_h_64,w_64,c_fill/0/thumb.jpg", true},
		{"https://media.licdn.com/sc/h/c1tecdbnvk1c8na3hbpj7vhfk", true}, // sc/h URLs are sprite icons
	}
	for _, c := range cases {
		t.Run(c.src, func(t *testing.T) {
			n := &html.Node{
				Type: html.ElementNode,
				Data: "img",
				Attr: []html.Attribute{{Key: "src", Val: c.src}},
			}
			if got := isTinyImage(n); got != c.wantTiny {
				t.Errorf("isTinyImage = %v, want %v", got, c.wantTiny)
			}
		})
	}
}
