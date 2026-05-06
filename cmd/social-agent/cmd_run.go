package main

// cmd_run.go — `social-agent run "<prompt>"`. The one-shot path:
// up + exec(harness.InvokePrompt) + down. Output streams straight
// to the operator's stdout as claude generates it.
//
// Same shared upFlags as `social-agent up` so flag surface is
// consistent.

import (
	"flag"
	"fmt"
	"strings"

	"github.com/jedi4ever/social-skills/internal/agent"
)

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	flags := &upFlags{}
	flags.attach(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("run: <prompt> required (e.g. `social-agent run \"summarise README.md\"`)")
	}
	prompt := strings.Join(fs.Args(), " ")
	prov, err := buildProvider(flags.provider)
	if err != nil {
		return err
	}
	ctx, cancel := signalCtx()
	defer cancel()
	return prov.Run(ctx, agent.UpOpts{
		Image:   flags.resolveImage(),
		Harness: flags.harness,
		Workdir: flags.workdir,
		Name:    flags.name,
	}, prompt)
}
