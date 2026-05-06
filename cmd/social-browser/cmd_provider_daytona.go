package main

// Daytona-specific subcommands under `social-browser provider
// daytona ...`. Most logic comes from the previous social-daytona
// CLI; we route into the same internal/browser/providers/daytona
// helpers + the bare daytona REST client for one-off ops (push,
// build).

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jedi4ever/social-skills/internal/browser"
	dprovider "github.com/jedi4ever/social-skills/internal/browser/providers/daytona"
)

func cmdProviderDaytona(verb string, args []string) error {
	switch verb {
	case "up":
		return runProviderDaytonaUp(args)
	case "ls", "list":
		return runProviderDaytonaLs(args)
	case "down", "stop":
		return runProviderDaytonaDown(args)
	case "env":
		return runProviderDaytonaEnv(args)
	case "build":
		return runProviderDaytonaBuild(args)
	case "push":
		return runProviderDaytonaPush(args)
	default:
		return fmt.Errorf("provider daytona: unknown verb %q (try: up | ls | down | env | build | push)", verb)
	}
}

// ----- up ----------------------------------------------------

func runProviderDaytonaUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	n := fs.Int("n", 1, "number of sandboxes to spin up")
	snapshot := fs.String("snapshot", "social-skills:"+Version, "snapshot name to launch from")
	cpu := fs.Int("cpu", 2, "CPU cores per sandbox")
	memory := fs.Int("memory", 2, "memory per sandbox in GB")
	disk := fs.Int("disk", 3, "disk per sandbox in GB")
	target := fs.String("target", "", "target region (eu, us)")
	autoStop := fs.Int("auto-stop", 0, "auto-stop after N minutes idle (0 = never)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prov, err := dprovider.NewProvider()
	if err != nil {
		return err
	}
	bs, err := prov.Up(context.Background(), browser.UpOpts{
		N:           *n,
		Image:       *snapshot,
		Region:      *target,
		CPU:         *cpu,
		Memory:      *memory,
		Disk:        *disk,
		AutoStopMin: *autoStop,
	})
	if err != nil {
		return err
	}
	for i, b := range bs {
		switch b.State {
		case "ready":
			fmt.Printf("[%d]  %s  %s\n", i, b.ID, b.URL)
		default:
			fmt.Printf("[%d]  %s  %s  %s\n", i, b.ID, b.State, b.Labels["error"])
		}
	}
	fmt.Fprintln(os.Stderr, "\nNext: `social-browser daemon start` to expose the fleet on http://127.0.0.1:5560")
	return nil
}

// ----- ls ----------------------------------------------------

func runProviderDaytonaLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	format := fs.String("f", "text", "output format: text (default) | json")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prov, err := dprovider.NewProvider()
	if err != nil {
		return err
	}
	bs, err := prov.List(context.Background())
	if err != nil {
		return err
	}
	if *format == "json" {
		return json.NewEncoder(os.Stdout).Encode(bs)
	}
	if len(bs) == 0 {
		fmt.Println("(no sandboxes — `social-browser provider daytona up -n N`)")
		return nil
	}
	for _, b := range bs {
		fmt.Printf("%-40s  %-10s  %s\n", b.ID, b.State, b.URL)
	}
	return nil
}

// ----- down --------------------------------------------------

func runProviderDaytonaDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	prov, err := dprovider.NewProvider()
	if err != nil {
		return err
	}
	if err := prov.Down(context.Background(), fs.Args()...); err != nil {
		return err
	}
	if len(fs.Args()) == 0 {
		fmt.Println("deleted all social-daytona sandboxes")
	} else {
		for _, id := range fs.Args() {
			fmt.Printf("deleted %s\n", id)
		}
	}
	return nil
}

// ----- env ---------------------------------------------------
//
// Print export statements pointing the local social-fetch /
// social-ledger directly at one sandbox's per-instance URL.
// Useful to bypass the daemon (when debugging or running a
// single-instance setup).

func runProviderDaytonaEnv(args []string) error {
	fs := flag.NewFlagSet("env", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := dprovider.New()
	if err != nil {
		return err
	}
	ctx := context.Background()
	var id string
	switch fs.NArg() {
	case 0:
		// Auto-pick when there's exactly one of ours.
		ws, err := c.ListWorkspaces(ctx)
		if err != nil {
			return err
		}
		var ours []string
		for _, w := range ws {
			if w.Labels[dprovider.LabelKey] == "true" {
				ours = append(ours, w.ID)
			}
		}
		switch len(ours) {
		case 0:
			return fmt.Errorf("env: no daytona sandboxes — run `social-browser provider daytona up -n N`")
		case 1:
			id = ours[0]
		default:
			return fmt.Errorf("env: %d sandboxes — pass an id (see `social-browser provider daytona ls`)", len(ours))
		}
	default:
		id = fs.Arg(0)
	}
	headless, err := c.GetPreviewURL(ctx, id, 5556, 0)
	if err != nil {
		return fmt.Errorf("preview-url 5556: %w", err)
	}
	ledger, err := c.GetPreviewURL(ctx, id, 5557, 0)
	if err != nil {
		return fmt.Errorf("preview-url 5557: %w", err)
	}
	fmt.Printf("# social-browser env for sandbox %s\n", id)
	fmt.Printf("# eval \"$(social-browser provider daytona env %s)\" then run social-fetch / social-ledger as usual.\n", id)
	fmt.Println()
	fmt.Printf("export SOCIAL_FETCH_HEADLESS_DAEMON_URL=%q\n", headless.URL)
	fmt.Printf("export SOCIAL_FETCH_HEADLESS_DAEMON_TOKEN=%q\n", headless.Token)
	fmt.Printf("export SOCIAL_LEDGER_DAEMON_URL=%q\n", ledger.URL)
	fmt.Printf("export SOCIAL_LEDGER_DAEMON_TOKEN=%q\n", ledger.Token)
	return nil
}

// ----- build / push -----------------------------------------
//
// Same shape as the old social-daytona equivalents. Keeps
// existing operator muscle memory.

func runProviderDaytonaBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	tag := fs.String("tag", "social-skills:"+Version, "docker image tag to build")
	native := fs.Bool("native", false, "build for the host's architecture (skip the linux/amd64 cross-compile)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cmdArgs := []string{"build"}
	if !*native {
		cmdArgs = append(cmdArgs, "--platform", "linux/amd64")
	}
	cmdArgs = append(cmdArgs, "-t", *tag, "-t", "social-skills:latest", ".")
	cmd := exec.Command("docker", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = ensureDockerHost(os.Environ())
	return cmd.Run()
}

func runProviderDaytonaPush(args []string) error {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	tag := fs.String("tag", "social-skills:"+Version, "docker image tag to push")
	name := fs.String("name", "", "snapshot name (default: --tag)")
	cpu := fs.Int("cpu", 2, "CPU cores allocated to sandboxes")
	memory := fs.Int("memory", 2, "memory in GB")
	disk := fs.Int("disk", 3, "disk in GB")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		*name = *tag
	}
	cmd := exec.Command("daytona", "snapshot", "push", *tag,
		"--name", *name,
		"--cpu", fmt.Sprintf("%d", *cpu),
		"--memory", fmt.Sprintf("%d", *memory),
		"--disk", fmt.Sprintf("%d", *disk),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = ensureDaytonaAPIEnv(ensureDockerHost(os.Environ()))
	return cmd.Run()
}

// ensureDockerHost / ensureDaytonaAPIEnv: same shims used by the
// old social-daytona push path. Daytona's CLI silently misroutes
// when these env vars aren't set the way it expects on macOS.

func ensureDockerHost(env []string) []string {
	for _, v := range env {
		if strings.HasPrefix(v, "DOCKER_HOST=") && len(v) > len("DOCKER_HOST=") {
			return env
		}
	}
	host := activeDockerHost()
	if host == "" {
		return env
	}
	return append(env, "DOCKER_HOST="+host)
}

func ensureDaytonaAPIEnv(env []string) []string {
	for _, v := range env {
		if strings.HasPrefix(v, "DAYTONA_API_URL=") && len(v) > len("DAYTONA_API_URL=") {
			return env
		}
	}
	return append(env, "DAYTONA_API_URL=https://app.daytona.io/api")
}

func activeDockerHost() string {
	out, err := exec.Command("docker", "context", "show").Output()
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return ""
	}
	out, err = exec.Command("docker", "context", "inspect", name).Output()
	if err != nil {
		return ""
	}
	var ctxs []struct {
		Endpoints map[string]struct {
			Host string `json:"Host"`
		} `json:"Endpoints"`
	}
	if err := json.Unmarshal(out, &ctxs); err != nil || len(ctxs) == 0 {
		return ""
	}
	if d, ok := ctxs[0].Endpoints["docker"]; ok && d.Host != "" {
		return d.Host
	}
	return ""
}
