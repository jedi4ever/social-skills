package main

// `social-browser provider <name> <verb>` — substrate-specific
// management. Today only daytona is wired; future providers
// (local, playwright) plug in here.

import (
	"fmt"
)

func cmdProvider(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("provider: <name> <verb> required (e.g. `provider daytona up -n 1`)")
	}
	name, verb := args[0], args[1]
	rest := args[2:]
	switch name {
	case "daytona":
		return cmdProviderDaytona(verb, rest)
	default:
		return fmt.Errorf("provider: unknown name %q (today: daytona)", name)
	}
}
