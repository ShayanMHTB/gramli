# Commands

## Workspace

```sh
gramli init
gramli doctor
gramli db status
```

## Authentication

```sh
gramli login --cookie-file ./cookies.json --account personal
gramli auth status
gramli auth status --check-remote
gramli logout
```

Gramli never asks for an Instagram password. Cookie files are copied into `.gramli/sessions/` with restrictive permissions.

## Saved Posts

```sh
gramli posts sync --saved --collection saved --limit 100 --delay 2s
gramli posts sync --saved --collection saved --limit 100 --delay 4s
gramli posts list --collection saved --limit 20
gramli posts show <shortcode>
gramli posts search <query>
```

Use `--limit` while testing. Increase gradually before attempting a full account archive.

## Downloads

Status:

```sh
gramli download status
```

The status output includes:

- `Downloaded`: media rows with completed local files
- `Pending`: media rows still open
- `Failed`: media rows marked failed
- `Skipped`: media rows skipped by a previous command
- `Missing`: media rows where Instagram returned a placeholder or unavailable media URL
- `Unsupported`: media rows intentionally not handled by the current downloader

Single post:

```sh
gramli download run --post <shortcode-or-url> --metadata --strategy yt-dlp
```

Batch by collection:

```sh
gramli download run --collection saved --limit 25 --metadata --strategy yt-dlp --delay 5s
```

All matching posts:

```sh
gramli download run --collection saved --all --metadata --strategy yt-dlp --delay 5s
```

Larger conservative batch:

```sh
gramli download run --collection saved --limit 500 --metadata --strategy yt-dlp --delay 7s
```

## Reconcile

Reconcile scans `.gramli/downloads/<owner>/<shortcode>/`, compares files on disk to database rows, and updates local status when `--apply` is used.

Preview:

```sh
gramli download reconcile
```

Apply:

```sh
gramli download reconcile --apply
gramli download status
```

Use reconcile after large downloads, interrupted runs, manual cleanup, or any case where the file tree and SQLite status may have drifted.

## Cleanup

Preview cleanup:

```sh
gramli download clean --cache --response-cache --empty-dirs --dry-run
```

Run cleanup:

```sh
gramli download clean --cache --response-cache --empty-dirs --yes
```

Remove all downloaded media and reset local status rows:

```sh
gramli download clean --all --reset-db --yes
```

The `--cache` flag preserves the `yt-dlp` download archive. Use `--archive` only when you intentionally want future yt-dlp runs to ignore the previous archive.

## Export

```sh
gramli export --format json --stdout --pretty
gramli export --format csv --collection saved --output ./.gramli/exports/saved.csv --overwrite
gramli export --format markdown --stdout
```
