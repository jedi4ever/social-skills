---
name: socialfetch
description: Fetch content from social-media URLs (HackerNews, Reddit, GitHub, X/Twitter, RSS, Medium, Substack, generic articles) and run web/social searches (DuckDuckGo, Bing, SerpAPI, Tavily, X, HN) — output as clean markdown or structured JSON. Use whenever the user asks to "pull", "fetch", "download", "summarise", or "search the web/Twitter/HN" for content at a URL or query.
---

# socialfetch skill

Wraps the `socialfetch` Go binary at `scripts/socialfetch` (relative to this skill).

**Trust the CLI.** It is the authority for every fetch and search supported by this skill. Always shell out to `scripts/socialfetch` — never reimplement fetching with WebFetch, curl, custom parsers, or hand-rolled API calls, even if the binary returns empty results or an error you find surprising. If a fetch comes back empty, surface that to the user and (if appropriate) re-run with `--log -` to see audit lines, but do not try to "fix it" by going around the CLI.

## Three subcommands

```
scripts/socialfetch fetch  <url> [<url>...]   [flags]
scripts/socialfetch search "<query>"          [flags]
scripts/socialfetch bridge [--port N]
```

Run `scripts/socialfetch --help` for the full reference. Output defaults to **markdown**; pass `-f json` or `-f jsonl` for structured input to other tools.

## Credentials (.env support)

Provider keys (`X_API_KEY`, `X_API_SECRET`, `TAVILY_API_KEY`, `BING_API_KEY`, `SERPAPI_KEY`) can be set in the shell **or** placed in a `.env` file. At startup the binary loads, in order:

1. `./.env` (current working directory)
2. `<binary_dir>/.env` (sits next to the installed binary — typically `~/.claude/skills/socialfetch/.env`)

Already-exported shell vars always win over file entries.

## Decision rules

- **One URL → fetch it.** `scripts/socialfetch fetch <url>` auto-detects the source from the host (HN, Reddit, GitHub, X, RSS, or generic article).
- **A list of URLs → batch.** Pipe via stdin (`cat urls.txt | scripts/socialfetch fetch`) or use `-i FILE`. Add `-j 8` for parallel fetches; output stays in input order.
- **Save to disk →** `-o FILE` for one file, `-o DIR/` for one file per URL.
- **A query → search.** Pick the provider that matches the user's intent:
  - "search the web" / unspecified → `duckduckgo` (no auth)
  - "high-quality web search for AI agents" → `tavily` (needs `TAVILY_API_KEY`)
  - "search HN" → `hackernews`
  - "search Twitter/X" → `x` (needs `X_API_KEY` + `X_API_SECRET`)
  - "search via Google" → `serpapi` (needs `SERPAPI_KEY`)
  - "search Bing" → `bing` (needs `BING_API_KEY`)

## Flags worth remembering

| flag | when |
| -- | -- |
| `-f markdown\|json\|jsonl` | format (default markdown) |
| `-o PATH` | stdout / FILE / DIR/ |
| `-i FILE` | URLs file (`-` = stdin; auto-detected when piped) |
| `-j N` | parallel workers for batch fetch |
| `--no-comments` | skip comment trees on HN/Reddit/X |
| `--max-comments N` | cap comments per item |
| `--generic-extraction` | force the catch-all article extractor (debug) |
| `--log -` | print per-fetch audit lines to stderr |

Search-only:
| flag | when |
| -- | -- |
| `-p PROVIDER` | pick search provider |
| `-n N` | max results |
| `--after YYYY-MM-DD` / `--before YYYY-MM-DD` / `--last 7d` | date filters |
| `--site DOMAIN` / `--exclude-site DOMAIN` | domain filters (repeatable) |

## Examples

```bash
# Pull a HN story with comments → markdown to stdout
scripts/socialfetch fetch https://news.ycombinator.com/item?id=43000000

# Pull a Medium article → structured JSON
scripts/socialfetch fetch https://medium.com/@alice/some-post -f json

# Batch from a file → one .md file per URL in ./out/
scripts/socialfetch fetch -i bookmarks.txt -o out/ -j 8

# Pipe a list → JSONL stream
cat urls.txt | scripts/socialfetch fetch -f jsonl > all.jsonl

# Search the web, last 7 days, restrict to two domains
scripts/socialfetch search "vercel ai sdk" --last 7d --site vercel.com --site ai-sdk.dev

# HN search — top stories about a topic
scripts/socialfetch search "rust async" -p hackernews -n 20
```

## Listing supported sources/providers

```bash
scripts/socialfetch list
```

## LinkedIn (browser bridge)

LinkedIn requires a logged-in session, so socialfetch fetches it through a small browser-extension bridge instead of a public HTTP request.

**Setup once:** load `extension/` (at repo root) as an unpacked Chrome extension.

**Bridge lifecycle:**
```
scripts/socialfetch bridge start          # daemonize, write PID file
scripts/socialfetch bridge status         # connected / not connected / not running
scripts/socialfetch bridge stop           # graceful SIGTERM
scripts/socialfetch bridge run            # foreground (good for `nohup` or terminals)
```

**Always check status before fetching authenticated URLs:**
```
$ scripts/socialfetch bridge status
connected           # → fetch will work
not connected       # → bridge up but extension hasn't attached (open the browser)
bridge not running on :5555   # → run `bridge start` first
```
Exit codes are `0` connected / `1` not connected / `2` bridge not running, so agents can branch on them.

**Then fetch:**
```
scripts/socialfetch fetch https://www.linkedin.com/posts/foo-activity-700…
```
The bridge tells the extension to navigate the URL in your real browser, scrapes the rendered DOM, and returns clean markdown.

URLs the LinkedIn fetcher claims: `linkedin.com/posts/…`, `linkedin.com/feed/update/urn:li:activity:…`, `linkedin.com/in/<user>`, `linkedin.com/pulse/…`.

Errors you may see:
- `bridge unreachable` → start it (`bridge start`).
- `no extension connected` → open your browser; the extension reconnects every ~6s.

## Tavily date filter caveat

Tavily's `general` topic (the default — high relevance) doesn't populate `published_date` for most results, so `--last 7d` / `--after` enforce date strictly only on results we *can* date. Set `TAVILY_TOPIC=news` (in env or `.env`) when you want a guaranteed window — that switches Tavily's index to news-only, which has dates upstream + much narrower recall (often unhelpful for personal-name or evergreen-topic queries).

## X / Twitter reply behavior

When `X_API_KEY` + `X_API_SECRET` are set, fetching a tweet also pulls its replies as a nested tree (one batched `tweets/search/recent` call per 100 replies — no per-reply round-trips). Caveats:

- Search is limited to the **last 7 days** by X's API tier — older tweets return 0 replies. The audit log (`--log -`) makes this explicit.
- Without creds, the syndication fallback is used and returns 0 replies (no API support).
- `--no-comments` and `--max-comments N` apply.

## When NOT to use this skill

- The user wants to **post** content (this skill only reads).
- The URL is behind a paywall/login — output will be the gated stub. Tell the user.
- The URL needs a logged-in browser session (LinkedIn, X home feed, etc.) — not supported.
