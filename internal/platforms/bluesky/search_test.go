package bluesky

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jedi4ever/social-skills/internal/core"
)

// TestSearchPagedCursor confirms Bluesky's `cursor` parameter flows
// both directions: opts.Cursor → upstream `cursor=…` query param,
// and response's `cursor` → SearchPage.NextCursor.
func TestSearchPagedCursor(t *testing.T) {
	var seenCursor string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/com.atproto.server.createSession"):
			// Mint a fake session.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accessJwt":"jwt-test","handle":"x.bsky.social"}`))
		case strings.HasSuffix(r.URL.Path, "/app.bsky.feed.searchPosts"):
			seenCursor = r.URL.Query().Get("cursor")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"cursor":"page2","posts":[
                {"uri":"at://did:plc:abc/app.bsky.feed.post/r1","author":{"handle":"alice.bsky.social","displayName":"Alice"},"record":{"text":"hello world","createdAt":"2026-04-01T12:00:00Z"},"likeCount":0,"replyCount":0}
            ]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	p := NewSearchProvider()
	p.AuthBase = srv.URL
	p.PublicBase = srv.URL
	p.Handle = "x.bsky.social"
	p.AppPassword = "app-pw"

	// First call — no cursor input.
	page, err := p.SearchPaged(context.Background(), "hello", core.SearchOptions{Max: 5})
	if err != nil {
		t.Fatalf("first SearchPaged: %v", err)
	}
	if seenCursor != "" {
		t.Errorf("first call should not send cursor, got %q", seenCursor)
	}
	if page.NextCursor != "page2" {
		t.Errorf("NextCursor = %q, want page2", page.NextCursor)
	}
	if len(page.Results) != 1 {
		t.Fatalf("want 1 result, got %d", len(page.Results))
	}

	// Second call — pass the cursor back.
	_, err = p.SearchPaged(context.Background(), "hello", core.SearchOptions{Max: 5, Cursor: page.NextCursor})
	if err != nil {
		t.Fatalf("second SearchPaged: %v", err)
	}
	if seenCursor != "page2" {
		t.Errorf("second call should forward cursor, got %q", seenCursor)
	}
}

// TestSearchPagedNoMore — when Bluesky returns an empty cursor it
// means the result set is exhausted.
func TestSearchPagedNoMore(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/com.atproto.server.createSession"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"accessJwt":"jwt","handle":"x.bsky.social"}`))
		case strings.HasSuffix(r.URL.Path, "/app.bsky.feed.searchPosts"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"posts":[]}`))
		}
	}))
	defer srv.Close()

	p := NewSearchProvider()
	p.AuthBase = srv.URL
	p.Handle = "x"
	p.AppPassword = "y"

	page, err := p.SearchPaged(context.Background(), "x", core.SearchOptions{Max: 5})
	if err != nil {
		t.Fatalf("SearchPaged: %v", err)
	}
	if page.NextCursor != "" {
		t.Errorf("NextCursor should be empty when upstream omits it, got %q", page.NextCursor)
	}
}

// Quick guard so the test file's session mocking matches what session()
// actually expects — keeps regression risk low when auth flow changes.
func TestSessionMocking(t *testing.T) {
	p := NewSearchProvider()
	p.sessJWT = "preset"
	p.sessTime = time.Now()
	jwt, err := p.session(context.Background())
	if err != nil {
		t.Fatalf("session with preset jwt: %v", err)
	}
	if jwt != "preset" {
		t.Errorf("preset session should be reused, got %q", jwt)
	}
}
