package main

// cmd_daemon.go — long-running social-agent daemon. Mirror of
// social-browser's `daemon start/stop/status/run` shape: HTTP API
// for /run, /up, /down, /ls, /status, /health on a configurable
// bind address (default 127.0.0.1:5562).
//
// Stubbed in v0.16.0; full implementation tracked in a follow-up
// commit. The dispatcher entry stays so `social-agent daemon ...`
// returns a clear error today instead of "unknown subcommand".

import (
	"fmt"
)

func cmdDaemon(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("daemon: <start|stop|status|run> required")
	}
	switch args[0] {
	case "start", "stop", "status", "run":
		return fmt.Errorf("daemon %s: not implemented yet — use the per-call shortcuts (`social-agent run`, `social-agent up`, etc.) for now; daemon mode lands in a follow-up release", args[0])
	default:
		return fmt.Errorf("daemon: unknown verb %q (try: start | stop | status | run)", args[0])
	}
}
