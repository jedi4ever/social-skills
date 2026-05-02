//go:build live

package htmlmd

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Live test — hits r.jina.ai. No key required for the free tier.
//
//	go test -tags=live -timeout 2m -run TestLiveJinaReader ./internal/render/htmlmd/...
//
// Uses example.com as the most stable URL on the public internet.
// We only verify a non-empty markdown body comes back; Jina's
// formatting can change subtly between deploys, so we don't assert
// specific characters.
func TestLiveJinaReader(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	md, err := NewJinaReader().Read(ctx, "https://example.com/")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if strings.TrimSpace(md) == "" {
		t.Errorf("expected non-empty markdown, got empty")
	}
	// example.com always contains the word "Example".
	if !strings.Contains(strings.ToLower(md), "example") {
		t.Errorf("output looked unexpected: %q", md[:min(200, len(md))])
	}
	t.Logf("len=%d preview=%q", len(md), md[:min(200, len(md))])
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
