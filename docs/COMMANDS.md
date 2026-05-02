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
gramli posts list --collection saved --limit 20
gramli posts show <shortcode>
gramli posts search <query>
```

Use `--limit` while testing. Increase gradually before attempting a full account archive.

## Downloads

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

## Cleanup

Preview cleanup:

```sh
gramli download clean --all --cache --empty-dirs --reset-db --dry-run
```

Run cleanup:

```sh
gramli download clean --all --cache --empty-dirs --reset-db --yes
```

## Export

```sh
gramli export --format json --stdout --pretty
gramli export --format csv --collection saved --output ./.gramli/exports/saved.csv --overwrite
gramli export --format markdown --stdout
```
