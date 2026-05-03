# Development

## Toolchain

Gramli is written in Go and currently targets Go 1.24+.

```sh
mise exec -- go test ./...
```

Generated data must stay inside `.gramli/` during development.

## Project Layout

```text
cmd/gramli/          CLI entrypoint
internal/cli/        Cobra command wiring
internal/auth/       Session cookie import and auth checks
internal/config/     Config and repo-local path defaults
internal/instagram/  Instagram HTTP client and parsers
internal/posts/      Post/media persistence helpers
internal/storage/    SQLite setup and migrations
internal/exporter/   Metadata exporters
internal/logging/    slog setup
```

## Tests

Run:

```sh
mise exec -- go test ./...
```

Network-dependent behavior should not be required in CI. Use local fixtures and parser tests for Instagram response handling.

When running tests or commands from restricted environments, keep Go caches inside the repo:

```sh
env \
  GOCACHE="$PWD/.gramli/cache/go-build" \
  GOMODCACHE="$PWD/.gramli/cache/gomod" \
  MISE_CACHE_DIR="$PWD/.gramli/cache/mise" \
  mise exec -- go test ./...
```

## Download State Model

Download state is split across:

- `media.download_status`: per-media status used by `download status`
- `media.local_path`: local file path for direct media downloads or reconciled files
- `downloads`: append-only attempt records for direct downloads, yt-dlp downloads, missing media, and failures
- `.gramli/cache/yt-dlp/download-archive.txt`: yt-dlp archive used to skip previously downloaded posts

The `download reconcile` command bridges filesystem state back into SQLite. It scans `.gramli/downloads/<owner>/<shortcode>/`, ignores transient files, prefers completed video files over thumbnails, marks matched media rows as `downloaded`, and marks placeholder URLs as `missing`.

## Local Artifacts

Do not commit:

- `.gramli/`
- `data/`
- cookie JSON files
- logs
- downloaded media
- AI assistant context files

These are covered by `.gitignore`.

## Current Developer Notes

- `RecordDownload` stores `NULL` for post-level downloads without a specific media row.
- `yt-dlp` metadata inspection reads JSON from stdout and warnings from stderr separately.
- `download clean --cache` preserves the yt-dlp archive; use `--archive` to remove it explicitly.
- `download status` reports `downloaded`, `pending`, `failed`, `skipped`, `missing`, and `unsupported`.
