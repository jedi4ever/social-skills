// Package askmcp is the MCP server that runs *inside* a
// social-agent container. Exposes one tool, `ask_user`, which
// the inner claude-code calls when it needs information from the
// outer human operator. The tool body relays the question over
// HTTP to the outer social-agent process (via
// SOCIAL_AGENT_CALLBACK_URL), which in turn fires
// elicitation/create on the outer Claude Code session and waits
// for the user's reply.
//
// Lives on its own from internal/agent/mcp because that one is
// the *outer* MCP — the surface Claude Code talks to. This is
// the *inner* MCP — the surface the inner claude talks to.
// Keeping them separate avoids accidentally exposing
// social_agent_run / social_agent_up etc. to the inner agent
// (which would let it spawn its own sub-sandboxes — fun in
// theory, mess in practice).
package askmcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Config carries the per-server settings. Version stamps the
// MCP server's reported version; mirrors the binary's Version
// constant when wired by cmd/social-agent.
type Config struct {
	Version string
}

// NewServer returns an MCP server with the ask_user tool
// registered. Run via server.ServeStdio in cmd/social-agent's
// ask-mcp serve handler.
func NewServer(cfg Config) *server.MCPServer {
	s := server.NewMCPServer(
		"ask",
		cfg.Version,
		server.WithToolCapabilities(false),
	)
	addAskUserTool(s)
	return s
}

type askUserArgs struct {
	Question string `json:"question"`
}

func addAskUserTool(s *server.MCPServer) {
	tool := mcpgo.NewTool("ask_user",
		mcpgo.WithDescription("Ask the human operator a free-text question and wait for their answer. Use this when you need information that is not in your context — credentials, file locations, business decisions, anything you would otherwise have to guess. Don't use it for trivial things; the operator's attention is expensive. The result is the operator's reply as a string, or an error if they declined to answer."),
		mcpgo.WithString("question", mcpgo.Required(), mcpgo.Description("Plain-English question to show the operator. Be specific — they only see this string, not your conversation context.")),
	)
	s.AddTool(tool, mcpgo.NewTypedToolHandler(func(ctx context.Context, _ mcpgo.CallToolRequest, args askUserArgs) (*mcpgo.CallToolResult, error) {
		if strings.TrimSpace(args.Question) == "" {
			return mcpgo.NewToolResultError("question is required"), nil
		}
		callback := os.Getenv("SOCIAL_AGENT_CALLBACK_URL")
		if callback == "" {
			return mcpgo.NewToolResultError("ask_user is not available — this container has no SOCIAL_AGENT_CALLBACK_URL (likely running outside a social-agent MCP run; CLI runs don't have an outer client to ask)"), nil
		}
		token := os.Getenv("SOCIAL_AGENT_CALLBACK_TOKEN")

		body, _ := json.Marshal(map[string]any{"question": args.Question})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(callback, "/")+"/elicit", bytes.NewReader(body))
		if err != nil {
			return mcpgo.NewToolResultError("build request: " + err.Error()), nil
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		// No timeout on the HTTP call — elicitation is a human
		// in the loop, can take however long the operator takes.
		// Cancel via ctx (the inner claude's tool-call deadline).
		client := &http.Client{Timeout: 0}
		resp, err := client.Do(req)
		if err != nil {
			return mcpgo.NewToolResultError("callback: " + err.Error()), nil
		}
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			return mcpgo.NewToolResultError(fmt.Sprintf("callback HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))), nil
		}
		var out struct {
			Accepted bool   `json:"accepted"`
			Answer   string `json:"answer"`
		}
		if err := json.Unmarshal(respBody, &out); err != nil {
			return mcpgo.NewToolResultError("bad callback response: " + err.Error()), nil
		}
		if !out.Accepted {
			return mcpgo.NewToolResultError("operator declined to answer"), nil
		}
		return mcpgo.NewToolResultText(out.Answer), nil
	}))
}
