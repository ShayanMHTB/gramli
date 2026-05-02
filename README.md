# Gramli

Gramli is a local-first Go CLI for archiving Instagram saved-post metadata and media from your own authenticated browser session.

It is built for personal archival and programming-training use. It stores data in a repo-local `.gramli/` directory by default, keeps credentials out of the CLI, and uses explicit commands for sync, export, cleanup, and download.

## Status

Gramli is early-stage software. The current build supports:

- Repo-local workspace initialization
- SQLite storage and migrations
- Browser cookie session import
- Remote auth status check
- Manual post URL import
- Saved-post sync through the authenticated Instagram saved-post endpoint
- Local listing, search, collections, and export
- Single-post and batch downloads
- Mixed media downloads:
  - images and albums through saved-post API media URLs where available
  - videos and reels through `yt-dlp` plus `ffmpeg`
- Download cleanup and cache cleanup

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

Import your own Instagram browser cookies:

```sh
mise exec -- go run ./cmd/gramli login --cookie-file ./path/to/cookies.json --account personal
```

Check authentication:

```sh
mise exec -- go run ./cmd/gramli auth status --check-remote
```

Sync saved posts:

```sh
mise exec -- go run ./cmd/gramli posts sync --saved --collection saved --limit 100 --delay 2s
```

List synced saved posts:

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

## Storage

Development data is stored under:

```text
.gramli/
```

This includes:

- config
- SQLite database
- session cookie copies
- logs
- cache
- exports
- downloads

`.gramli/` is ignored by git.

## Safety Boundaries

Gramli does not ask for your Instagram password and does not store plaintext credentials. It does not attempt to bypass 2FA, captchas, rate limits, login challenges, private account protections, DRM, or anti-bot protections.

Use it only for content visible to your own authenticated account and only for personal archival. Do not use Gramli to mass-download, redistribute, or republish content you do not have permission to archive.

## Documentation

See:

- [COMMANDS](docs/COMMANDS.md)
- [DEVELOPMENT](docs/DEVELOPMENT.md)
- [SAVED_POSTS](docs/SAVED_POSTS.md)
- [SAFETY](docs/SAFETY.md)
