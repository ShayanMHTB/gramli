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

Only the posts not yet downloaded (skips the rest of the collection):

```sh
gramli download run --collection saved --pending --metadata --strategy yt-dlp --delay 4s
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

`download run` now reconciles automatically when it finishes, so `download status` and the web UI reflect what's on disk without a manual step. Pass `--no-reconcile` to skip it.

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

## Sessions

```sh
gramli sessions list                # all stored sessions (alias, type, active, file)
gramli logout                       # deactivate the most recent session
gramli logout --all                 # deactivate all sessions
gramli logout --delete-session-files
gramli logout --archive             # move cookie files aside and drop the records
gramli logout --remove              # permanently delete records + cookie files
gramli sessions archive personal    # archive a specific session by alias
gramli sessions remove personal --yes
gramli sessions prune --yes         # drop all inactive (logged-out) sessions
gramli sessions prune --archive --yes
```

Archived cookie files are moved to `.gramli/sessions/archive/<alias>-<timestamp>.cookies.json`.

## Organize (local)

```sh
gramli collections create "Ideas"
gramli collections add-post ideas <shortcode>
gramli collections remove-post ideas <shortcode>
gramli collections delete ideas --yes
gramli posts tag <shortcode> design inspiration
gramli posts untag <shortcode> design
gramli posts delete <shortcode> --yes            # removes DB rows + files everywhere
gramli posts delete <shortcode> --with-files=false --yes
```

## Account & Profile

```sh
gramli account sync                 # fetch the logged-in profile
gramli account sync --username someone   # any visible profile
gramli account show                 # display the stored profile
gramli account switch --account work     # set the active account alias
gramli auth refresh                 # re-validate and refresh session status
```

## Web UI

```sh
gramli web                          # serve http://127.0.0.1:8787 (read-only)
gramli web --open                   # also open the browser
gramli web --port 9000 --no-remote-thumbnails
```

The UI offers full-text search (SQLite FTS5), a filterable gallery (by
collection, creator, type, status, tag), a post lightbox with album carousel
(arrow keys to page media, `j`/`k` between posts, `/` to focus search, `Esc` to
close), per-creator and per-collection stats, and "Export JSON/CSV" of the
current filtered view.

It is also read-write for **local** organization: from a post you can add/remove
tags, create and toggle collection membership, and delete the post everywhere
(DB rows + files). Hashtags in captions are clickable. These mutations stay
local and the server binds to `127.0.0.1`.

## Maintenance

```sh
gramli config set downloads.concurrency 4
gramli collections sync             # best-effort saved-collection sync
gramli posts clean --dry-run        # preview orphaned post removal
gramli posts clean --yes
gramli download retry --failed --missing   # re-queue for the next run
```
