package main

// `social-agent ask-mcp serve` — runs the in-container ask-MCP
// server on stdio. The container's claude-code is configured to
// spawn this binary with these args via --mcp-config so the
// inner agent has an `ask_user` tool that round-trips to the
// outer Claude Code session via the callback HTTP server.
//
// stdio is the transport because that's how claude-code spawns
// MCP child processes — same pattern social-agent's outer mcp
// subcommand uses.

import (
	"flag"
	"fmt"

	"github.com/mark3labs/mcp-go/server"

	"github.com/jedi4ever/social-skills/internal/agent/askmcp"
)

func cmdAskMCP(verb string, args []string) error {
	switch verb {
	case "serve":
		return runAskMCPServe(args)
	case "":
		return fmt.Errorf("ask-mcp: missing verb (try: serve)")
	default:
		return fmt.Errorf("ask-mcp: unknown verb %q (try: serve)", verb)
	}
}

func runAskMCPServe(args []string) error {
	fs := flag.NewFlagSet("ask-mcp serve", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	s := askmcp.NewServer(askmcp.Config{Version: Version})
	return server.ServeStdio(s)
}
