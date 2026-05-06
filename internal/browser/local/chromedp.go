package local

// chromedp.go holds the helpers each browser slot needs at launch
// time: stealth init script + the chromedp.ExecAllocator option list.
// Lifted from internal/render/headless/headless.go pre-v0.15.0 — same
// flag set proven to work against LinkedIn / Medium bot detection.

import (
	"os"

	"github.com/chromedp/chromedp"

	"github.com/jedi4ever/social-skills/internal/render/headless"
)

// stealthScript runs at every navigation before any page script —
// masks the standard automation tells. Direct port from the Python
// codebase's _STEALTH_INIT_SCRIPT.
const stealthScript = `
Object.defineProperty(navigator, 'webdriver', { get: () => undefined });
Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
Object.defineProperty(navigator, 'plugins', { get: () => [1, 2, 3, 4, 5] });
window.chrome = { runtime: {} };
`

// buildAllocatorOpts assembles the chromedp.ExecAllocator options
// from a headless.Options struct. Args mirror the Python code's
// browser launch: disable-blink-features=AutomationControlled +
// no-sandbox + disable-dev-shm-usage so we run cleanly in containers
// and don't trip the "Chrome is being controlled by automated test
// software" banner detection.
func buildAllocatorOpts(opts headless.Options) []chromedp.ExecAllocatorOption {
	a := []chromedp.ExecAllocatorOption{
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		// --no-zygote stops Chromium spawning the zygote helper
		// process which needs CAP_SYS_ADMIN to create user
		// namespaces. Without this flag, Chromium starts then
		// immediately dies with "Failed to move to new namespace"
		// in restrictive containers (Daytona sandboxes, K8s pods
		// without privileged: true, etc.). Local Docker tolerates
		// the zygote because it grants full caps; the flag is a
		// harmless no-op there.
		chromedp.Flag("no-zygote", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.UserAgent(opts.UserAgent),
		chromedp.WindowSize(opts.ViewportWidth, opts.ViewportHeight),
	}
	if opts.Headless {
		a = append(a, chromedp.Headless)
	}
	if opts.ExecPath != "" {
		a = append(a, chromedp.ExecPath(opts.ExecPath))
	}
	if opts.Locale != "" {
		a = append(a, chromedp.Flag("lang", opts.Locale))
	}
	if opts.Timezone != "" {
		// chromedp doesn't have a top-level timezone option; pass
		// it via TZ env. The exec allocator inherits process env so
		// we set it on the running process for the spawn.
		// (Per-spawn TZ would be cleaner; deferred until needed.)
		_ = os.Setenv("TZ", opts.Timezone)
	}
	return a
}
