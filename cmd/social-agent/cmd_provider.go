package main

// `social-agent provider <name> <verb>` — explicit/multi-provider
// form. Today only "docker"; future "daytona" plugs in here as a
// case branch. Mirrors `social-browser provider <name> <verb>`.

import "fmt"

func cmdProvider(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("provider: <name> <verb> required (e.g. `provider docker run \"...\"`)")
	}
	name, verb := args[0], args[1]
	rest := args[2:]
	switch name {
	case "docker":
		return cmdProviderDocker(verb, rest)
	case "daytona":
		return cmdProviderDaytona(verb, rest)
	default:
		return fmt.Errorf("provider: unknown name %q (today: docker | daytona)", name)
	}
}
