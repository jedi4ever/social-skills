package main

// `social-browser daemon ...` — run / start / stop / status the
// local round-robin daemon. Mirrors the lifecycle shape that
// `social-fetch headless` and `social-ledger daemon` use:
//
//   start = fork detached + write pid file
//   run   = foreground (used by start internally; also useful for
//           docker / systemd / development)
//   stop  = read pid, SIGTERM
//   status = curl :5560/status

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jedi4ever/social-skills/internal/browser"
	"github.com/jedi4ever/social-skills/internal/browser/local"
	dprovider "github.com/jedi4ever/social-skills/internal/browser/providers/daytona"
	"github.com/jedi4ever/social-skills/internal/render/headless"
)

func cmdDaemon(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("daemon: <start|stop|status|run> required")
	}
	switch args[0] {
	case "start":
		return runDaemonStart(args[1:])
	case "stop":
		return runDaemonStop(args[1:])
	case "status":
		return runDaemonStatus(args[1:])
	case "run":
		return runDaemonForeground(args[1:])
	default:
		return fmt.Errorf("daemon: unknown verb %q", args[0])
	}
}

// runDaemonForeground is the actual long-running process — block
// in browser.Daemon.Run until ctx cancels. Used directly by
// operators who want the daemon attached to their terminal, and
// indirectly by `start` (which forks a child running this).
func runDaemonForeground(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	bind := fs.String("bind", fmt.Sprintf("127.0.0.1:%d", browser.DefaultDaemonPort), "listen address")
	provider := fs.String("provider", "daytona", "backend provider (daytona | local)")
	// Daytona-only flags — no-op for --provider local.
	onlyID := fs.String("id", "", "(daytona) pin the fleet to a single backend id (debugging)")
	verbose := fs.Bool("verbose", false, "(daytona) log outgoing URL + token prefix + non-2xx response bodies")
	// Local-only flags — no-op for --provider daytona.
	poolSize := fs.Int("pool-size", local.DefaultPoolSize, "(local) number of warm chromedp browsers to keep ready")
	recycleAfter := fs.Int("recycle-after", local.DefaultRecycleAfter, "(local) recycle each browser after N fetches; 0 = never")
	if err := fs.Parse(args); err != nil {
		return err
	}

	logf := func(format string, a ...any) { fmt.Fprintf(os.Stderr, "browser-daemon: "+format+"\n", a...) }

	// Cancel on SIGINT / SIGTERM — clean exit so the parent
	// `start` orchestrator can wait properly.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	switch strings.ToLower(*provider) {
	case "local":
		d := &local.Daemon{
			PoolSize:     *poolSize,
			RecycleAfter: *recycleAfter,
			Options:      headless.OptionsFromEnv(),
			Logf:         logf,
		}
		return d.Run(ctx, *bind)
	case "daytona":
		prov, err := buildProvider(*provider)
		if err != nil {
			return err
		}
		d := &browser.Daemon{
			Provider: prov,
			OnlyID:   *onlyID,
			Verbose:  *verbose,
			Logf:     logf,
		}
		return d.Run(ctx, *bind)
	default:
		return fmt.Errorf("daemon: unknown --provider %q (try: daytona | local)", *provider)
	}
}

// runDaemonStart forks a detached child running `daemon run` so
// the operator's shell prompt returns. Same pid-file pattern as
// `social-fetch headless start` — write to ~/Library/Caches/...
// /browser-daemon.pid (macOS) or $XDG_RUNTIME_DIR equivalent on
// Linux.
func runDaemonStart(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	bind := fs.String("bind", fmt.Sprintf("127.0.0.1:%d", browser.DefaultDaemonPort), "listen address")
	provider := fs.String("provider", "daytona", "backend provider (daytona | local)")
	pool := fs.Int("pool", 0, "(daytona) ensure the fleet has at least N backends; spawns more via the provider when below")
	onlyID := fs.String("id", "", "(daytona) pin the fleet to a single backend id (debugging)")
	verbose := fs.Bool("verbose", false, "(daytona) log outgoing URL + token prefix + non-2xx response bodies")
	poolSize := fs.Int("pool-size", local.DefaultPoolSize, "(local) number of warm chromedp browsers to keep ready")
	recycleAfter := fs.Int("recycle-after", local.DefaultRecycleAfter, "(local) recycle each browser after N fetches; 0 = never")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// Top-up the fleet first, so by the time the daemon comes up
	// it already sees the requested capacity. Only meaningful for
	// daytona — for local, the pool is built in-process by the
	// child via --pool-size.
	if *pool > 0 && strings.EqualFold(*provider, "daytona") {
		if err := topUpFleet(context.Background(), *provider, *pool); err != nil {
			fmt.Fprintf(os.Stderr, "warning: fleet top-up failed: %v\n", err)
		}
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}
	pidPath := pidFilePath()
	logPath := logFilePath()

	// Reject double-start.
	if pid, ok := readPID(pidPath); ok {
		if isProcessAlive(pid) {
			return fmt.Errorf("daemon already running (pid %d, %s)", pid, pidPath)
		}
	}

	logF, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log %s: %w", logPath, err)
	}

	childArgs := []string{"daemon", "run", "--bind", *bind, "--provider", *provider}
	if *onlyID != "" {
		childArgs = append(childArgs, "--id", *onlyID)
	}
	if *verbose {
		childArgs = append(childArgs, "--verbose")
	}
	if strings.EqualFold(*provider, "local") {
		childArgs = append(childArgs,
			"--pool-size", strconv.Itoa(*poolSize),
			"--recycle-after", strconv.Itoa(*recycleAfter),
		)
	}
	cmd := exec.Command(self, childArgs...)
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.Env = os.Environ() // inherit DAYTONA_* etc.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		return err
	}
	fmt.Printf("daemon started (pid %d, bind %s, provider %s, log %s)\n",
		cmd.Process.Pid, *bind, *provider, logPath)
	// Don't Wait — let it run detached.
	return nil
}

func runDaemonStop(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	pidPath := pidFilePath()
	pid, ok := readPID(pidPath)
	if !ok {
		return fmt.Errorf("no daemon running (pid file %s missing)", pidPath)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("kill %d: %w", pid, err)
	}
	_ = os.Remove(pidPath)
	fmt.Printf("daemon stopped (pid %d)\n", pid)
	return nil
}

func runDaemonStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	bind := fs.String("bind", fmt.Sprintf("127.0.0.1:%d", browser.DefaultDaemonPort), "daemon address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	resp, err := http.Get("http://" + *bind + "/status")
	if err != nil {
		return fmt.Errorf("status: %w (is the daemon running? `social-browser daemon start`)", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("daemon returned HTTP %d", resp.StatusCode)
	}
	// Plain pass-through — the status handler returns JSON.
	_, _ = os.Stdout.ReadFrom(resp.Body)
	return nil
}

// ----- helpers -----

// topUpFleet calls Provider.List and, if below target, calls Up
// for the difference. Used by `daemon start --pool N` for the
// one-shot bootstrap case.
func topUpFleet(ctx context.Context, provName string, target int) error {
	prov, err := buildProvider(provName)
	if err != nil {
		return err
	}
	have, err := prov.List(ctx)
	if err != nil {
		return err
	}
	if len(have) >= target {
		return nil
	}
	missing := target - len(have)
	fmt.Fprintf(os.Stderr, "fleet has %d backend(s), spawning %d more to reach %d ...\n", len(have), missing, target)
	// Default snapshot to social-skills:<this version>.
	_, err = prov.Up(ctx, browser.UpOpts{
		N:     missing,
		Image: "social-skills:" + Version,
	})
	return err
}

// buildProvider dispatches to the right concrete provider
// implementation. Today: only daytona; v2 adds local.
func buildProvider(name string) (browser.Provider, error) {
	switch strings.ToLower(name) {
	case "daytona":
		return dprovider.NewProvider()
	default:
		return nil, fmt.Errorf("unknown provider %q (today: daytona)", name)
	}
}

func pidFilePath() string {
	dir := cacheDir()
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "browser-daemon.pid")
}

func logFilePath() string {
	dir := cacheDir()
	_ = os.MkdirAll(dir, 0o755)
	return filepath.Join(dir, "browser-daemon.log")
}

func cacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "social-browser")
	}
	return os.TempDir()
}

func readPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	// Signal 0 = "is the process there?" — doesn't actually
	// deliver anything, just probes.
	return syscall.Kill(pid, syscall.Signal(0)) == nil
}

// _ = time.Second // keep time imported for future use if/when we add deadlines elsewhere
var _ = time.Second
