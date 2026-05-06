package harness

// claude_code.go is the first Harness implementation: Anthropic's
// Claude Code (`claude` CLI). Extends to other harnesses (codex,
// gemini, …) by adding parallel files; the agent provider stays
// harness-agnostic.

import (
	"strings"
)

// ClaudeCode is a stateless harness — no fields, no construction.
// Registered at init via the package-level Register call.
type ClaudeCode struct{}

func (ClaudeCode) Name() string { return "claude-code" }

// InvokePrompt returns the argv for a one-shot prompt. The flags
// match what `claude --help` documents:
//
//	--print                          headless mode (write answer
//	                                 to stdout, exit when done)
//	--dangerously-skip-permissions   skip the per-tool prompt — the
//	                                 container is the sandbox, the
//	                                 whole point is to give claude
//	                                 full freedom inside.
//
// The container's entrypoint hardcodes the same shape today, so
// this is mainly the canonical reference for "what does claude-code
// expect"; when we add a second harness, the entrypoint switches on
// $HARNESS to call the right argv.
func (ClaudeCode) InvokePrompt(prompt string) []string {
	return []string{"claude", "--print", "--dangerously-skip-permissions", prompt}
}

// InteractiveCmd is the bare `claude` command — drops the operator
// into Claude Code's interactive UI. Useful when the operator
// `social-agent exec`s into a session and wants to iterate live.
func (ClaudeCode) InteractiveCmd() []string {
	return []string{"claude"}
}

// EnvFromHost selects the auth-related env vars to forward. Today:
//
//   - ANTHROPIC_API_KEY        — direct API key, simplest path
//   - CLAUDE_OAUTH_CREDENTIALS — base64 of the OAuth credentials
//     JSON (operator pre-extracts via
//     dclaude's credentials.sh today;
//     v0.17 will do this automatically
//     from the macOS Keychain).
//
// Either suffices on its own; the entrypoint's auth-precedence
// logic prefers OAuth credentials when both are set. Returning an
// empty map is fine — claude --print will surface its own auth
// error and we don't try to be smarter than upstream.
//
// host is the operator's full env map (typically os.Environ()
// parsed); we read only the keys we recognise so unrelated env
// pollution doesn't leak into the container.
func (ClaudeCode) EnvFromHost(host map[string]string) (map[string]string, error) {
	out := map[string]string{}
	for _, key := range []string{
		"ANTHROPIC_API_KEY",
		"CLAUDE_OAUTH_CREDENTIALS",
	} {
		if v, ok := host[key]; ok && strings.TrimSpace(v) != "" {
			out[key] = v
		}
	}
	return out, nil
}

func init() {
	Register(ClaudeCode{})
}
