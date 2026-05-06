package main

// cmd_session.go holds the session-lifecycle shortcuts:
// up / down / exec / ls. Each is a thin wrapper over the docker
// provider; the `social-agent provider docker <verb>` form (in
// cmd_provider_docker.go) is the explicit equivalent.
//
// Shared helper: buildProvider resolves the --provider flag (today
// only "docker") and returns the matching agent.Provider. Same
// shape social-browser uses in cmd_daemon.go's buildProvider.

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jedi4ever/social-skills/internal/agent"
	daytonaprov "github.com/jedi4ever/social-skills/internal/agent/providers/daytona"
	dockerprov "github.com/jedi4ever/social-skills/internal/agent/providers/docker"
)

// buildProvider returns the agent.Provider matching name. Lone
// indirection point so adding a new substrate touches one switch
// case here.
func buildProvider(name string) (agent.Provider, error) {
	switch strings.ToLower(name) {
	case "", "docker":
		return dockerprov.New(), nil
	case "daytona":
		return daytonaprov.NewProvider()
	default:
		return nil, fmt.Errorf("unknown provider %q (today: docker | daytona)", name)
	}
}

// signalCtx returns a context that cancels on SIGINT / SIGTERM.
// Used by every long-running subcommand so Ctrl-C cleanly tears
// down the in-flight docker call.
func signalCtx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

// upFlags is shared between `up`, `run`, and the provider-namespaced
// equivalents. Kept in one place so the surface is consistent across
// the two invocation forms.
type upFlags struct {
	provider string
	image    string
	harness  string
	workdir  string
	name     string
	env      envList // repeated --env KEY=VAL
}

func (u *upFlags) attach(fs *flag.FlagSet) {
	fs.StringVar(&u.provider, "provider", "docker", "where to run the session: docker today; daytona later")
	fs.StringVar(&u.image, "image", "", "docker image:tag (default: social-skills-agent:<Version>)")
	fs.StringVar(&u.harness, "harness", "claude-code", "coding-agent CLI to run inside")
	fs.StringVar(&u.workdir, "workdir", "", "host path to bind-mount at /workspace (default: no mount)")
	fs.StringVar(&u.name, "name", "", "explicit container name (idempotent up)")
	fs.Var(&u.env, "env", "set env var inside the container (KEY=VAL); may repeat")
}

// resolveImage applies the default-from-Version when --image was not
// passed. Centralised so both cmd_session.go and cmd_provider_docker.go
// agree on the default tag.
func (u *upFlags) resolveImage() string {
	if u.image != "" {
		return u.image
	}
	return "social-skills-agent:" + Version
}

// resolveEnv parses the --env KEY=VAL pairs into the map shape
// agent.UpOpts wants. Empty pairs are skipped silently; malformed
// entries (no `=`) return an error so the operator sees the typo
// before the container starts and the env var goes missing.
func (u *upFlags) resolveEnv() (map[string]string, error) {
	if len(u.env) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(u.env))
	for _, kv := range u.env {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			return nil, fmt.Errorf("--env %q: expected KEY=VAL", kv)
		}
		out[kv[:i]] = kv[i+1:]
	}
	return out, nil
}

// envList is a flag.Value that collects repeated --env occurrences
// into a slice. Default flag.StringVar would let only the last
// instance win, which is wrong for multi-env. The standard pattern
// for "may repeat" string flags in stdlib `flag`.
type envList []string

func (e *envList) String() string { return strings.Join(*e, ",") }
func (e *envList) Set(v string) error {
	*e = append(*e, v)
	return nil
}

// ----- up -----

func cmdUp(args []string) error {
	fs := flag.NewFlagSet("up", flag.ContinueOnError)
	flags := &upFlags{}
	flags.attach(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	envMap, err := flags.resolveEnv()
	if err != nil {
		return err
	}
	prov, err := buildProvider(flags.provider)
	if err != nil {
		return err
	}
	ctx, cancel := signalCtx()
	defer cancel()
	s, err := prov.Up(ctx, agent.UpOpts{
		Image:   flags.resolveImage(),
		Harness: flags.harness,
		Workdir: flags.workdir,
		Name:    flags.name,
		Env:     envMap,
	})
	if err != nil {
		return err
	}
	fmt.Println(s.ID)
	if s.Workdir != "" {
		fmt.Fprintf(os.Stderr, "session %s: workdir=%s harness=%s\n", short(s.ID), s.Workdir, s.Harness)
	} else {
		fmt.Fprintf(os.Stderr, "session %s: harness=%s (no workdir mount)\n", short(s.ID), s.Harness)
	}
	fmt.Fprintf(os.Stderr, "next: social-agent exec %s\n", short(s.ID))
	return nil
}

// ----- down -----

func cmdDown(args []string) error {
	fs := flag.NewFlagSet("down", flag.ContinueOnError)
	provider := fs.String("provider", "docker", "substrate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prov, err := buildProvider(*provider)
	if err != nil {
		return err
	}
	ctx, cancel := signalCtx()
	defer cancel()
	ids := fs.Args()
	if err := prov.Down(ctx, ids...); err != nil {
		return err
	}
	if len(ids) == 0 {
		fmt.Println("removed all social-agent sessions")
	} else {
		for _, id := range ids {
			fmt.Println("removed", id)
		}
	}
	return nil
}

// ----- ls -----

func cmdLs(args []string) error {
	fs := flag.NewFlagSet("ls", flag.ContinueOnError)
	provider := fs.String("provider", "docker", "substrate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	prov, err := buildProvider(*provider)
	if err != nil {
		return err
	}
	ctx, cancel := signalCtx()
	defer cancel()
	sessions, err := prov.List(ctx)
	if err != nil {
		return err
	}
	if len(sessions) == 0 {
		fmt.Println("(no sessions — `social-agent up` to start one)")
		return nil
	}
	for _, s := range sessions {
		workdir := s.Workdir
		if workdir == "" {
			workdir = "-"
		}
		fmt.Printf("%s  %-9s  %-12s  %-12s  %s\n",
			short(s.ID),
			s.State,
			s.Harness,
			s.Provider,
			workdir,
		)
	}
	return nil
}

// ----- exec -----

func cmdExec(args []string) error {
	fs := flag.NewFlagSet("exec", flag.ContinueOnError)
	provider := fs.String("provider", "docker", "substrate")
	tty := fs.Bool("tty", true, "allocate a TTY for the exec'd command (default true)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() == 0 {
		return fmt.Errorf("exec: <id> required (see `social-agent ls`)")
	}
	id := fs.Arg(0)
	cmd := fs.Args()[1:]
	prov, err := buildProvider(*provider)
	if err != nil {
		return err
	}
	ctx, cancel := signalCtx()
	defer cancel()
	return prov.Exec(ctx, id, agent.ExecOpts{
		Cmd:    cmd,
		Stdin:  os.Stdin,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		TTY:    *tty,
	})
}

// short returns the first 12 chars of a docker container id, the
// length docker itself uses for the short-form id. Keeps `social-agent
// ls` output legible while still uniquely identifying sessions in
// the typical case.
func short(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// _ keeps time imported — used in upcoming daemon work; cheaper to
// pre-pull the import than re-jiggle it during the follow-up commit.
var _ = time.Second
