package main

// `social-agent provider daytona <verb>` — daytona-substrate
// surface: build / push / up / ls / down / exec / run / env.
//
// build / push are daytona-specific (cross-compile + ship the
// agent image to Daytona's registry as a snapshot). The
// lifecycle verbs (up / ls / down / exec / run) delegate to the
// same handlers as the top-level shortcuts but force
// --provider daytona — same pattern social-browser uses for its
// daytona namespace.

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/jedi4ever/social-skills/internal/build"
)

func cmdProviderDaytona(verb string, args []string) error {
	switch verb {
	case "build":
		return runProviderDaytonaBuild(args)
	case "push":
		return runProviderDaytonaPush(args)
	case "up", "start":
		return cmdUp(forceProvider("daytona", args))
	case "ls", "list":
		return cmdLs(forceProvider("daytona", args))
	case "down", "stop":
		return cmdDown(forceProvider("daytona", args))
	case "exec":
		return cmdExec(forceProvider("daytona", args))
	case "run":
		return cmdRun(forceProvider("daytona", args))
	case "pull":
		return cmdPull(forceProvider("daytona", args))
	default:
		return fmt.Errorf("provider daytona: unknown verb %q (try: build | push | up | ls | down | exec | run | pull)", verb)
	}
}

// forceProvider prepends `--provider daytona` so the lifecycle
// shortcuts default to the right substrate even when called via
// `provider daytona`. The bare verbs already accept --provider
// as a flag, so this just shadows whatever default they have.
func forceProvider(name string, args []string) []string {
	return append([]string{"--provider", name}, args...)
}

// ----- build -----

func runProviderDaytonaBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	tag := fs.String("tag", "social-skills-agent:"+Version, "docker image tag to build")
	arch := fs.String("arch", "amd64", "target architecture: amd64 (Daytona) | arm64")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *arch != "amd64" && *arch != "arm64" {
		return fmt.Errorf("--arch must be amd64 or arm64, got %q", *arch)
	}
	if err := build.LinuxBinaries(*arch); err != nil {
		return fmt.Errorf("cross-compile linux/%s binaries: %w", *arch, err)
	}
	cmdArgs := []string{"buildx", "build",
		"--platform", "linux/" + *arch,
		"-f", "Dockerfile.agent",
		"-t", *tag,
		"-t", "social-skills-agent:latest",
		"--load",
		".",
	}
	c := exec.Command("docker", cmdArgs...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Env = os.Environ()
	return c.Run()
}

// ----- push -----

func runProviderDaytonaPush(args []string) error {
	fs := flag.NewFlagSet("push", flag.ContinueOnError)
	tag := fs.String("tag", "social-skills-agent:"+Version, "docker image tag to push")
	name := fs.String("name", "", "snapshot name (default: --tag)")
	cpu := fs.Int("cpu", 2, "CPU cores per sandbox")
	memory := fs.Int("memory", 2, "memory in GB")
	disk := fs.Int("disk", 3, "disk in GB")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *name == "" {
		*name = *tag
	}
	c := exec.Command("daytona", "snapshot", "push", *tag,
		"--name", *name,
		"--cpu", fmt.Sprintf("%d", *cpu),
		"--memory", fmt.Sprintf("%d", *memory),
		"--disk", fmt.Sprintf("%d", *disk),
	)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	// daytona CLI needs both DAYTONA_API_URL (auth-key path silently
	// drops creds otherwise) AND DOCKER_HOST (otherwise it can't
	// list local images on macOS — the docker socket isn't always
	// at the unix:///var/run/docker.sock that's hardcoded into
	// daytona's defaults).
	c.Env = ensureDaytonaAPIEnv(ensureDockerHost(os.Environ()))
	return c.Run()
}

// ensureDaytonaAPIEnv mirrors the helper in
// internal/browser/providers/daytona — daytona CLI silently
// drops API-key auth when DAYTONA_API_URL is empty.
func ensureDaytonaAPIEnv(env []string) []string {
	for _, v := range env {
		if len(v) > len("DAYTONA_API_URL=") && v[:len("DAYTONA_API_URL=")] == "DAYTONA_API_URL=" {
			return env
		}
	}
	return append(env, "DAYTONA_API_URL=https://app.daytona.io/api")
}

// ensureDockerHost resolves the active docker context's socket
// when DOCKER_HOST isn't already in env. Same shim the browser
// provider uses on macOS where the docker socket lives at
// ~/.docker/run/docker.sock (Docker Desktop) or
// ~/.colima/default/docker.sock (Colima) instead of the
// /var/run/docker.sock daytona's CLI expects.
func ensureDockerHost(env []string) []string {
	for _, v := range env {
		if len(v) > len("DOCKER_HOST=") && v[:len("DOCKER_HOST=")] == "DOCKER_HOST=" {
			return env
		}
	}
	host := activeDockerHost()
	if host == "" {
		return env
	}
	return append(env, "DOCKER_HOST="+host)
}

// activeDockerHost reads `docker context show` + `docker context
// inspect` to find the docker daemon's actual socket. Empty
// return means we couldn't resolve it; caller falls through to
// daytona's default and the operator gets a clear error from
// daytona itself.
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
	// Look for "Host":"unix:///..." in the JSON.
	idx := strings.Index(string(out), `"Host":"`)
	if idx < 0 {
		return ""
	}
	start := idx + len(`"Host":"`)
	end := strings.Index(string(out)[start:], `"`)
	if end < 0 {
		return ""
	}
	return string(out)[start : start+end]
}
