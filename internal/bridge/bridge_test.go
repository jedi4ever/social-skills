package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// End-to-end round trip: a fake "extension" connects, the test posts a
// command to /cmd, the extension echoes a reply, and the HTTP response
// body matches what the extension sent.
func TestBridgeRoundTrip(t *testing.T) {
	srv := New()
	mux := http.NewServeMux()
	srv.Routes(mux)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := strings.Replace(httpSrv.URL, "http://", "ws://", 1) + "/ws/extension"

	// Fake extension: connect, await one command, reply with a fixed body.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")

	go func() {
		// Send hello (informational; bridge logs and ignores).
		_ = c.Write(ctx, websocket.MessageText, []byte(`{"type":"hello","agent":"test"}`))
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var req map[string]any
		if err := json.Unmarshal(data, &req); err != nil {
			return
		}
		reply := map[string]any{
			"id":      req["id"],
			"command": "get_html",
			"status":  "ok",
			"html":    "<p>hi</p>",
			"url":     "https://example.com/x",
		}
		raw, _ := json.Marshal(reply)
		_ = c.Write(ctx, websocket.MessageText, raw)
	}()

	// Give the fake extension a moment to register before we POST.
	for i := 0; i < 20; i++ {
		if srv.Connected() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !srv.Connected() {
		t.Fatalf("extension never registered")
	}

	resp, err := http.Post(httpSrv.URL+"/cmd", "application/json",
		strings.NewReader(`{"command":"get_html","url":"https://example.com/x"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got["status"] != "ok" || got["html"] != "<p>hi</p>" {
		t.Errorf("unexpected reply: %+v", got)
	}
}

// /cmd should return 503 when no extension is connected.
func TestBridgeNotConnected(t *testing.T) {
	srv := New()
	mux := http.NewServeMux()
	srv.Routes(mux)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	resp, err := http.Post(httpSrv.URL+"/cmd", "application/json",
		strings.NewReader(`{"command":"ping"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.StatusCode)
	}
}

// /cmd with bad JSON returns 400.
func TestBridgeBadJSON(t *testing.T) {
	srv := New()
	mux := http.NewServeMux()
	srv.Routes(mux)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	resp, err := http.Post(httpSrv.URL+"/cmd", "application/json", strings.NewReader(`not json`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

// /cmd should return 504 when the extension never replies in time.
func TestBridgeTimeout(t *testing.T) {
	srv := New()
	srv.CommandTimeout = 100 * time.Millisecond
	mux := http.NewServeMux()
	srv.Routes(mux)
	httpSrv := httptest.NewServer(mux)
	defer httpSrv.Close()

	wsURL := strings.Replace(httpSrv.URL, "http://", "ws://", 1) + "/ws/extension"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Close(websocket.StatusNormalClosure, "")
	// Don't reply at all.

	for i := 0; i < 20 && !srv.Connected(); i++ {
		time.Sleep(20 * time.Millisecond)
	}
	resp, err := http.Post(httpSrv.URL+"/cmd", "application/json",
		strings.NewReader(`{"command":"ping"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusGatewayTimeout {
		t.Errorf("status = %d, want 504", resp.StatusCode)
	}
}
