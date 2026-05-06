package main

// `social-agent harness {list}` — discovery for which coding-agent
// CLIs are baked into this binary. Today only claude-code; later
// codex / gemini / etc.

import (
	"fmt"

	"github.com/jedi4ever/social-skills/internal/agent/harness"
)

func cmdHarness(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("harness: <verb> required (try: list)")
	}
	switch args[0] {
	case "list":
		for _, name := range harness.Names() {
			fmt.Println(name)
		}
		return nil
	default:
		return fmt.Errorf("harness: unknown verb %q (try: list)", args[0])
	}
}
