// Package docker is the local-docker Provider for social-agent
// sessions. Shells out to the `docker` CLI rather than calling
// the docker SDK directly — same approach social-browser's daytona
// provider takes, and the same approach dclaude's docker provider
// uses. Smaller binary, no docker-go-sdk pin, easier debugging
// (operator can `docker ...` the same flags by hand).
package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/jedi4ever/social-skills/internal/agent"
	"github.com/jedi4ever/social-skills/internal/agent/harness"
)

// LabelKey is what the provider stamps on every container it
// creates. List filters by this label so List() returns only "our"
// containers, not random ones the operator launched separately.
const LabelKey = "social-agent"

// DefaultImage is the image:tag launched when UpOpts.Image is
// empty. Matches the tag `make agent-build-<arch>` produces.
const DefaultImage = "social-skills-agent:latest"

// Provider is the docker substrate. Stateless beyond the docker
// daemon itself — every method shells out per-call.
type Provider struct{}

// New returns a Provider. Cheap to construct; safe to call from
// every entrypoint without caching.
func New() *Provider { return &Provider{} }

// Name identifies the provider in CLI output and Session.Provider
// stamping.
func (p *Provider) Name() string { return "docker" }

// Up creates a new agent container and returns its metadata.
// Reuses an existing one with the same UpOpts.Name when it's still
// running — `up` is idempotent on names. Streams docker's stderr
// through to the caller's stderr so a pull-on-first-use shows
// progress.
func (p *Provider) Up(ctx context.Context, opts agent.UpOpts) (*agent.Session, error) {
	hName := opts.Harness
	if hName == "" {
		hName = "claude-code"
	}
	h, err := harness.Get(hName)
	if err != nil {
		return nil, err
	}
	image := opts.Image
	if image == "" {
		image = DefaultImage
	}

	// Reuse-existing path: if a container with this name is already
	// running, return its metadata instead of failing on `docker run`.
	if opts.Name != "" {
		if s, err := p.inspect(ctx, opts.Name); err == nil && s != nil && s.State == "running" {
			s.Harness = hName
			return s, nil
		}
	}

	// Compose the docker run argv. -d keeps the container in the
	// background; --label tags it as ours; --rm is NOT set so
	// crashed containers leave a corpse the operator can `docker
	// logs` after the fact.
	args := []string{"run", "-d",
		"--label", LabelKey + "=true",
		"--label", LabelKey + "-harness=" + hName,
		"--label", LabelKey + "-image=" + image,
	}
	if opts.Name != "" {
		args = append(args, "--name", opts.Name)
	}
	if opts.Workdir != "" {
		// Bind-mount opt-in: the host path lands at /workspace
		// inside (matches Dockerfile.agent's WORKDIR).
		args = append(args, "-v", opts.Workdir+":/workspace")
		args = append(args, "--label", LabelKey+"-workdir="+opts.Workdir)
	}

	// Env: harness-selected auth vars first, then operator-supplied
	// extras, then a CLAUDE_OAUTH_CREDENTIALS injection if the
	// caller pre-extracted it. The order matters because Env
	// entries with the same key win on the right (last one set).
	envForContainer, err := h.EnvFromHost(parseEnviron(os.Environ()))
	if err != nil {
		return nil, fmt.Errorf("harness %s: env: %w", hName, err)
	}
	for k, v := range opts.Env {
		envForContainer[k] = v
	}
	if opts.CredentialsBlob != "" {
		envForContainer["CLAUDE_OAUTH_CREDENTIALS"] = opts.CredentialsBlob
	}
	for k, v := range envForContainer {
		args = append(args, "-e", k+"="+v)
	}

	// Optional caller-supplied labels.
	for k, v := range opts.Labels {
		args = append(args, "--label", k+"="+v)
	}

	args = append(args, image)
	// Default CMD is "sleep" (entrypoint keeps the container alive
	// via tail -f /dev/null). Run() overrides this for the one-shot
	// path.

	cid, err := dockerOutput(ctx, args)
	if err != nil {
		return nil, err
	}
	cid = strings.TrimSpace(cid)

	return &agent.Session{
		ID:       cid,
		Provider: p.Name(),
		Harness:  hName,
		Image:    image,
		Workdir:  opts.Workdir,
		Created:  time.Now(),
		State:    "running",
		Labels: map[string]string{
			LabelKey:              "true",
			LabelKey + "-harness": hName,
			LabelKey + "-image":   image,
			LabelKey + "-workdir": opts.Workdir,
		},
	}, nil
}

// Down removes containers by ID. Empty ids = remove every container
// labelled as ours. `docker rm -f` so a still-running container
// gets a SIGKILL — the alternative (graceful stop, then remove)
// pays a 10s docker-stop timeout per container which adds up at
// fleet teardown time.
func (p *Provider) Down(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		owned, err := p.listIDs(ctx)
		if err != nil {
			return err
		}
		ids = owned
	}
	if len(ids) == 0 {
		return nil
	}
	args := append([]string{"rm", "-f"}, ids...)
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdout = os.Stderr
	c.Stderr = os.Stderr
	return c.Run()
}

// List returns every container labelled as ours. Reads docker's
// own filter mechanism so we don't have to parse `docker ps`'s
// human-readable output — `--format json` returns one JSON object
// per line.
func (p *Provider) List(ctx context.Context) ([]agent.Session, error) {
	out, err := dockerOutput(ctx, []string{
		"ps", "-a",
		"--filter", "label=" + LabelKey + "=true",
		"--format", "{{json .}}",
	})
	if err != nil {
		return nil, err
	}
	var sessions []agent.Session
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		// docker ps --format json emits PascalCase keys; CamelCase
		// the few we care about.
		var entry struct {
			ID     string `json:"ID"`
			Names  string `json:"Names"`
			Image  string `json:"Image"`
			State  string `json:"State"`
			Labels string `json:"Labels"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Don't fail the whole list on one parse error —
			// surface to stderr and continue. Docker's format is
			// stable but a future docker-version drift shouldn't
			// brick `social-agent ls` entirely.
			fmt.Fprintf(os.Stderr, "social-agent: skipping malformed docker ps row: %v\n", err)
			continue
		}
		labels := parseDockerLabels(entry.Labels)
		sessions = append(sessions, agent.Session{
			ID:       entry.ID,
			Provider: p.Name(),
			Harness:  labels[LabelKey+"-harness"],
			Image:    entry.Image,
			Workdir:  labels[LabelKey+"-workdir"],
			State:    entry.State,
			Labels:   labels,
		})
	}
	return sessions, nil
}

// Exec runs a command inside an existing container. Empty cmd = the
// harness's interactive form. Streams stdin/stdout/stderr through
// the supplied opts. Allocates a TTY when opts.TTY is set or when
// stdin is a terminal.
func (p *Provider) Exec(ctx context.Context, id string, opts agent.ExecOpts) error {
	if id == "" {
		return errors.New("docker provider: Exec requires a container id")
	}

	cmd := opts.Cmd
	if len(cmd) == 0 {
		// Default to the harness's interactive form. We need to
		// know which harness this container runs — fetch via
		// inspect rather than asking the caller a second time.
		s, err := p.inspect(ctx, id)
		if err != nil {
			return err
		}
		hName := "claude-code"
		if s != nil && s.Harness != "" {
			hName = s.Harness
		}
		h, err := harness.Get(hName)
		if err != nil {
			return err
		}
		cmd = h.InteractiveCmd()
	}

	args := []string{"exec"}
	if opts.TTY {
		args = append(args, "-it")
	} else {
		args = append(args, "-i")
	}
	// Always go through the entrypoint so credential decoding /
	// env passthrough has a consistent shape. The entrypoint's
	// "exec" mode just exec(2)s the rest of argv.
	args = append(args, id, "/usr/local/bin/docker-agent-entrypoint.sh", "exec")
	args = append(args, cmd...)

	c := exec.CommandContext(ctx, "docker", args...)
	c.Stdin = opts.Stdin
	c.Stdout = opts.Stdout
	c.Stderr = opts.Stderr
	if c.Stdin == nil {
		c.Stdin = os.Stdin
	}
	if c.Stdout == nil {
		c.Stdout = os.Stdout
	}
	if c.Stderr == nil {
		c.Stderr = os.Stderr
	}
	return c.Run()
}

// Run is the one-shot path: Up + harness.InvokePrompt + capture
// stdout + Down. Streams claude's response straight to opts.Stdout
// as it arrives so the operator sees output in real time.
//
// We deliberately use Up + Exec rather than `docker run --rm
// <image> run "<prompt>"` — the latter would skip the entrypoint's
// credential decoding because docker exec's CMD-replacement
// semantics mean the entrypoint sees `run "<prompt>"` as argv[1]
// argv[2] and dispatches correctly, but exec'ing into an
// already-running container lets us capture the session ID for
// debugging if the prompt errors.
func (p *Provider) Run(ctx context.Context, opts agent.UpOpts, prompt string) error {
	hName := opts.Harness
	if hName == "" {
		hName = "claude-code"
	}
	h, err := harness.Get(hName)
	if err != nil {
		return err
	}
	s, err := p.Up(ctx, opts)
	if err != nil {
		return err
	}
	defer func() {
		// Best-effort teardown — bracket Up with Down so a panic
		// or context cancellation doesn't leak the container.
		downCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = p.Down(downCtx, s.ID)
	}()
	return p.Exec(ctx, s.ID, agent.ExecOpts{
		Cmd: h.InvokePrompt(prompt),
		// Run is non-interactive — no TTY. Stdout/stderr stream
		// through to the caller (the social-agent CLI passes
		// os.Stdout / os.Stderr).
	})
}

// ----- helpers -----

// dockerOutput runs `docker <args...>` and returns combined stdout.
// Stderr goes through to the caller's stderr so docker's progress
// output (image pulls, etc) is visible. Used everywhere we need
// to read docker's output (inspect, ps, run -d → container id).
func dockerOutput(ctx context.Context, args []string) (string, error) {
	c := exec.CommandContext(ctx, "docker", args...)
	c.Stderr = os.Stderr
	out, err := c.Output()
	if err != nil {
		return "", fmt.Errorf("docker %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// parseEnviron converts an os.Environ()-shaped slice into the
// map shape harness.EnvFromHost expects.
func parseEnviron(env []string) map[string]string {
	out := make(map[string]string, len(env))
	for _, kv := range env {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		out[kv[:i]] = kv[i+1:]
	}
	return out
}

// parseDockerLabels splits docker's comma-separated label string
// (the form `docker ps --format '{{.Labels}}'` emits) into a map.
// Format: `key1=val1,key2=val2,…`. Values can't contain commas
// (docker enforces this at run time).
func parseDockerLabels(s string) map[string]string {
	out := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		kv = strings.TrimSpace(kv)
		if kv == "" {
			continue
		}
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		out[kv[:i]] = kv[i+1:]
	}
	return out
}

// listIDs returns just the container IDs of our sessions. Used by
// Down for the "remove all of ours" path.
func (p *Provider) listIDs(ctx context.Context) ([]string, error) {
	out, err := dockerOutput(ctx, []string{
		"ps", "-a", "-q",
		"--filter", "label=" + LabelKey + "=true",
	})
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			ids = append(ids, line)
		}
	}
	return ids, nil
}

// inspect returns one container's session metadata, or nil + nil
// when the container doesn't exist (so Up can fall through to
// `docker run`). Errors only on actual docker failures.
func (p *Provider) inspect(ctx context.Context, idOrName string) (*agent.Session, error) {
	out, err := dockerOutput(ctx, []string{
		"inspect", idOrName,
		"--format", "{{json .}}",
	})
	if err != nil {
		// `docker inspect` exits 1 when the target doesn't exist.
		// Treat as a clean miss; caller decides what to do.
		return nil, nil
	}
	var entry struct {
		ID    string `json:"Id"`
		Name  string `json:"Name"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		Config struct {
			Image  string            `json:"Image"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if err := json.Unmarshal([]byte(out), &entry); err != nil {
		return nil, fmt.Errorf("inspect %s: parse: %w", idOrName, err)
	}
	if entry.Config.Labels[LabelKey] != "true" {
		// Not one of ours — refuse to manage.
		return nil, nil
	}
	return &agent.Session{
		ID:       entry.ID,
		Provider: "docker",
		Harness:  entry.Config.Labels[LabelKey+"-harness"],
		Image:    entry.Config.Image,
		Workdir:  entry.Config.Labels[LabelKey+"-workdir"],
		State:    entry.State.Status,
		Labels:   entry.Config.Labels,
	}, nil
}

// _ keeps io imported — used by the ExecOpts streaming docs above
// even when none of the helper functions reference it directly.
var _ io.Reader = (*os.File)(nil)
