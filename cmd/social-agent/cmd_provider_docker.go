package main

// Docker-specific subcommands under `social-agent provider docker
// <verb>`. The lifecycle verbs (up | ls | down | exec | run)
// delegate to the same handlers as the top-level shortcuts —
// having both surfaces costs one indirection but keeps the CLI
// shape consistent with social-browser. The one verb that lives
// only here is `build`, which cross-compiles the social-skills
// binaries on the host and `docker buildx`-builds Dockerfile.agent.

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/jedi4ever/social-skills/internal/build"
)

func cmdProviderDocker(verb string, args []string) error {
	switch verb {
	case "build":
		return runProviderDockerBuild(args)
	case "up", "start":
		return cmdUp(args)
	case "ls", "list":
		return cmdLs(args)
	case "down", "stop":
		return cmdDown(args)
	case "exec":
		return cmdExec(args)
	case "run":
		return cmdRun(args)
	default:
		return fmt.Errorf("provider docker: unknown verb %q (try: build | up | ls | down | exec | run)", verb)
	}
}

// runProviderDockerBuild cross-compiles Go binaries on the host
// for linux/<arch>, then docker-buildx-builds Dockerfile.agent
// COPYing them in. Same flow social-browser's
// `provider daytona build` uses, just pointed at Dockerfile.agent
// + the social-skills-agent tag set.
func runProviderDockerBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	tag := fs.String("tag", "social-skills-agent:"+Version, "docker image tag to build")
	arch := fs.String("arch", hostArch(), "target architecture: amd64 | arm64 (default: host's)")
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

// hostArch returns the Go-style arch string (amd64 / arm64) for
// the build host. uname's "x86_64" maps to "amd64"; "aarch64" /
// "arm64" both map to "arm64". Anything else falls through to
// amd64 (the safer default for unknown CI runners).
func hostArch() string {
	out, err := exec.Command("uname", "-m").Output()
	if err != nil {
		return "amd64"
	}
	switch s := trim(string(out)); s {
	case "arm64", "aarch64":
		return "arm64"
	case "x86_64", "amd64":
		return "amd64"
	default:
		return "amd64"
	}
}

func trim(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
