# Gramli

Gramli is a local-first Go CLI for managing your Instagram account from the terminal — after authentication via your own browser session. The long-term vision covers downloading saved posts, archiving your own posts and reels, updating your profile, and scheduling new posts without ever touching the web UI.

Meta has locked down the Instagram API heavily. The currently working surface is the authenticated saved-post endpoint. The rest of the vision lives in [`docs/FUTURE_FEATURES.md`](docs/FUTURE_FEATURES.md).

## What Works Today

- Local workspace initialization and SQLite-backed storage
- Browser cookie import for authenticated sessions
- Remote auth verification (lightweight request against the live API)
- Saved-post sync from the authenticated Instagram feed API with pagination
- Manual URL import from a file or stdin
- Local listing, search, and collection management
- Per-post and batch media downloads:
  - Images and carousel albums via direct authenticated HTTP from API media URLs
  - Videos and reels via `yt-dlp` + `ffmpeg` with H.264/AAC compatibility pass
- Download reconciliation (scan disk → update SQLite status)
- Metadata sidecar files (`post.json`) alongside downloaded media
- Export to JSON, CSV, and Markdown
- Download cleanup and cache cleanup
- Missing and unavailable media tracking

## Requirements

- Go 1.24+
- `yt-dlp`
- `ffmpeg`

On macOS:

```sh
brew install yt-dlp ffmpeg
```

This repo includes `mise.toml`, so development can use:

```sh
mise exec -- go test ./...
```

## Quick Start

Initialize the local workspace:

```sh
mise exec -- go run ./cmd/gramli init
```

Import your Instagram browser cookies (export them from your browser first):

```sh
mise exec -- go run ./cmd/gramli login --cookie-file ./path/to/cookies.json --account personal
```

Verify authentication:

```sh
mise exec -- go run ./cmd/gramli auth status --check-remote
```

Sync saved posts:

```sh
mise exec -- go run ./cmd/gramli posts sync --saved --collection saved --limit 100 --delay 2s
```

List synced posts:

```sh
mise exec -- go run ./cmd/gramli posts list --collection saved --limit 20
```

Download a controlled batch:

```sh
mise exec -- go run ./cmd/gramli download run \
  --collection saved \
  --limit 25 \
  --metadata \
  --strategy yt-dlp \
  --delay 5s
```

Reconcile downloaded files back into the database:

```sh
mise exec -- go run ./cmd/gramli download reconcile --apply
mise exec -- go run ./cmd/gramli download status
```

For larger runs, use small sync → download → reconcile cycles:

```sh
mise exec -- go run ./cmd/gramli posts sync --saved --collection saved --limit 100 --delay 4s
mise exec -- go run ./cmd/gramli download run --collection saved --limit 100 --metadata --strategy yt-dlp --delay 7s
mise exec -- go run ./cmd/gramli download reconcile --apply
```

## Storage Layout

All runtime data lives under `.gramli/` (git-ignored):

```text
.gramli/
  config.yaml           — active YAML config
  gramli.db             — SQLite database
  sessions/             — imported cookie files (mode 0600)
  cache/
    saved/              — raw saved-post API JSON responses
    posts/              — raw post HTML responses
    yt-dlp/             — ephemeral Netscape cookie file + download archive
  logs/gramli.log
  exports/
  downloads/
    <owner>/
      <shortcode>/      — media files + optional post.json sidecar
```

Downloaded files are named with a zero-padded media index prefix:

```text
downloads/
  natgeo/
    ABC123xyz/
      01_image.jpg
      post.json
  someuser/
    XYZ789abc/
      01_video.mp4
      post.json
```

## Global Flags

| Flag | Default | Description |
|---|---|---|
| `--data-dir` | `./.gramli` | Gramli workspace directory |
| `--db` | `<data-dir>/gramli.db` | SQLite database path |
| `--log-level` | `info` | `debug`, `info`, `warn`, `error` |
| `--log-file` | `<data-dir>/logs/gramli.log` | Log file path |
| `--quiet` | false | Suppress non-essential output |
| `--verbose` | false | Enable verbose output |
| `--json` | false | Machine-readable JSON output |
| `--no-color` | false | Disable colored output |
| `--dry-run` | false | Show what would happen without making changes |
| `--yes` | false | Auto-confirm destructive prompts |

## Safety Boundaries

Gramli does not ask for your Instagram password and does not store plaintext credentials. It does not attempt to bypass 2FA, captchas, rate limits, login challenges, private account protections, DRM, or anti-bot protections.

Use it only for content visible to your own authenticated account and only for personal archival. Do not use Gramli to mass-download, redistribute, or republish content you do not have rights to archive.

## Documentation

- [COMMANDS](docs/COMMANDS.md) — full command reference
- [DEVELOPMENT](docs/DEVELOPMENT.md) — contributor notes
- [FUTURE_FEATURES](docs/FUTURE_FEATURES.md) — roadmap and planned commands
- [SAVED_POSTS](docs/SAVED_POSTS.md) — saved-post sync internals
- [SAFETY](docs/SAFETY.md) — safety policy
