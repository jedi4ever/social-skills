package reddit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jedi4ever/social-skills/internal/core"
)

// TestSearchPagedCursor confirms Reddit's `after` cursor flows both
// directions: opts.Cursor → upstream `after=…` query param, and
// response's `data.after` → SearchPage.NextCursor.
func TestSearchPagedCursor(t *testing.T) {
	var seenAfter string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAfter = r.URL.Query().Get("after")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"after":"t3_next_page_id","children":[
            {"data":{"title":"hello","permalink":"/r/test/comments/abc/","url":"https://example.com/x","subreddit":"test","author":"alice","score":42,"num_comments":3,"created_utc":1735000000}}
        ]}}`))
	}))
	defer srv.Close()

	p := NewSearchProvider()
	p.BaseURL = srv.URL

	// First call — no cursor.
	page, err := p.SearchPaged(context.Background(), "x", core.SearchOptions{Max: 5})
	if err != nil {
		t.Fatalf("first SearchPaged: %v", err)
	}
	if seenAfter != "" {
		t.Errorf("first call should not send after, got %q", seenAfter)
	}
	if page.NextCursor != "t3_next_page_id" {
		t.Errorf("NextCursor = %q, want t3_next_page_id", page.NextCursor)
	}
	if len(page.Results) != 1 {
		t.Fatalf("want 1 result, got %d", len(page.Results))
	}

	// Second call — pass the cursor back.
	_, err = p.SearchPaged(context.Background(), "x", core.SearchOptions{Max: 5, Cursor: page.NextCursor})
	if err != nil {
		t.Fatalf("second SearchPaged: %v", err)
	}
	if seenAfter != "t3_next_page_id" {
		t.Errorf("second call should forward cursor as after, got %q", seenAfter)
	}
}

// TestSearchPagedExhausted — Reddit returns `"after":null` when the
// query has no more pages; we surface that as an empty NextCursor.
func TestSearchPagedExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Reddit emits null for `after` on the last page; Go decodes
		// that to the zero value (empty string) since After is a
		// plain string field.
		_, _ = w.Write([]byte(`{"data":{"after":null,"children":[
            {"data":{"title":"last","permalink":"/r/test/comments/zzz/","url":"https://example.com/z","subreddit":"test","author":"bob","score":1,"num_comments":0,"created_utc":1735000000}}
        ]}}`))
	}))
	defer srv.Close()

	p := NewSearchProvider()
	p.BaseURL = srv.URL

	page, err := p.SearchPaged(context.Background(), "x", core.SearchOptions{Max: 5})
	if err != nil {
		t.Fatalf("SearchPaged: %v", err)
	}
	if page.NextCursor != "" {
		t.Errorf("NextCursor should be empty when after is null, got %q", page.NextCursor)
	}
}

// TestSearchPreservesContract — the legacy Search() method (returns
// just []SearchResult) keeps working. SearchPaged is the additive
// path; Search wraps it and discards the cursor.
func TestSearchPreservesContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"after":"t3_x","children":[
            {"data":{"title":"a","permalink":"/r/t/comments/a/","url":"https://e.com/a","subreddit":"t","author":"u","score":1,"num_comments":0,"created_utc":1735000000}}
        ]}}`))
	}))
	defer srv.Close()

	p := NewSearchProvider()
	p.BaseURL = srv.URL

	results, err := p.Search(context.Background(), "x", core.SearchOptions{Max: 5})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("want 1 result, got %d", len(results))
	}
}
