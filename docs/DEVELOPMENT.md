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

## Local Artifacts

Do not commit:

- `.gramli/`
- `data/`
- cookie JSON files
- logs
- downloaded media
- AI assistant context files

These are covered by `.gitignore`.
