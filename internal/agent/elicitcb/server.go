// Package elicitcb is the outer-side callback HTTP server that
// turns inner-claude `ask_user` tool calls into MCP
// elicitation/create requests on the outer Claude Code session.
//
// Lifecycle: started by the social-agent MCP server's
// runStreaming path before launching the inner container, torn
// down when the run completes. Listens on 127.0.0.1:<random>;
// the URL is passed into the container via
// SOCIAL_AGENT_CALLBACK_URL (the docker provider auto-rewrites
// 127.0.0.1 → host.docker.internal so the container can reach
// it). A bearer token (SOCIAL_AGENT_CALLBACK_TOKEN) gates the
// /elicit endpoint as defence-in-depth — the loopback bind is
// the primary boundary.
//
// Why bother with a callback at all? MCP server-to-client
// requests (RequestElicitation) require the *outer* MCP
// server's session context. The in-container MCP server has its
// own outer client (the inner claude itself), so it can't
// elicit on the outer Claude Code's behalf — it has to relay
// through this callback back to the social-agent process that
// owns the outer session.
package elicitcb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// ElicitFunc is the work the callback server delegates to. The
// social-agent MCP layer supplies an implementation that calls
// MCPServer.RequestElicitation against the outer client. Returns
// (answer, accepted, error) — `accepted` is false when the user
// declined or cancelled, in which case answer is empty and the
// caller (in-container MCP) returns a tool-result indicating the
// user said no rather than an error.
type ElicitFunc func(ctx context.Context, question string) (answer string, accepted bool, err error)

// Server is one running callback endpoint. URL() and Token() are
// the values the container needs in env. Close shuts down the
// listener; safe to call multiple times.
type Server struct {
	listener net.Listener
	httpd    *http.Server
	token    string
	url      string
}

// Start binds a random loopback port, generates a bearer token,
// and starts the HTTP server. Returns immediately — the server
// runs in a goroutine. Caller plumbs URL() + Token() into the
// container's env and Close()s when done.
func Start(elicit ElicitFunc) (*Server, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen 127.0.0.1: %w", err)
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		_ = l.Close()
		return nil, fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/elicit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		// Bearer token check — defence-in-depth even though
		// we're on loopback.
		auth := r.Header.Get("Authorization")
		want := "Bearer " + token
		if auth != want {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var req struct {
			Question string `json:"question"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Question) == "" {
			http.Error(w, "question is required", http.StatusBadRequest)
			return
		}
		answer, accepted, err := elicit(r.Context(), req.Question)
		if err != nil {
			http.Error(w, "elicit: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"accepted": accepted,
			"answer":   answer,
		})
	})

	srv := &Server{
		listener: l,
		token:    token,
		url:      "http://127.0.0.1:" + strings.TrimPrefix(l.Addr().String(), "127.0.0.1:"),
		httpd: &http.Server{
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}
	go func() { _ = srv.httpd.Serve(l) }()
	return srv, nil
}

// URL is the loopback URL operators (or the docker provider's
// rewriteLoopbackURL) substitute into env. Container side reads
// it as SOCIAL_AGENT_CALLBACK_URL.
func (s *Server) URL() string { return s.url }

// Token is the bearer the container's ask-mcp must send on the
// Authorization header. Distributed as SOCIAL_AGENT_CALLBACK_TOKEN.
func (s *Server) Token() string { return s.token }

// Close stops the HTTP server. Idempotent — re-calling on a
// closed server returns nil.
func (s *Server) Close() error {
	if s.httpd == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := s.httpd.Shutdown(ctx)
	s.httpd = nil
	return err
}
