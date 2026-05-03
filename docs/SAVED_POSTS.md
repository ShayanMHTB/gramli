# Saved Posts

Gramli can sync saved posts from the authenticated endpoint observed in the Instagram web app:

```text
https://www.instagram.com/api/v1/feed/saved/posts/
```

Pagination uses `next_max_id` / `max_id`.

## Recommended Flow

Check auth:

```sh
gramli auth status --check-remote
```

Sync a small batch:

```sh
gramli posts sync --saved --collection saved --limit 25 --delay 2s
```

Inspect:

```sh
gramli posts list --collection saved --limit 25
gramli db status
```

Increase the sync size gradually:

```sh
gramli posts sync --saved --collection saved --limit 100 --delay 2s
```

To fetch all saved posts, omit `--limit`. Use this only after smaller syncs work reliably.

## End-of-Day Archive Flow

This is the safest practical loop for large accounts:

```sh
gramli auth status --check-remote
gramli posts sync --saved --collection saved --limit 100 --delay 4s
gramli download run --collection saved --limit 100 --metadata --strategy yt-dlp --delay 7s
gramli download reconcile --apply
gramli download status
```

Repeat the loop with another `--limit 100` or increase gradually. Reconcile after every batch so SQLite reflects files already present on disk.

## Downloads

For mixed saved posts, use `yt-dlp` strategy:

```sh
gramli download run --collection saved --limit 25 --metadata --strategy yt-dlp --delay 5s
```

Images and albums use direct media URLs when available from the saved-post API. Videos and reels use `yt-dlp` and `ffmpeg` to merge audio/video streams into compatible MP4 files.

## Reconciliation

Saved-post downloads can involve direct CDN URLs, yt-dlp output, interrupted runs, skipped existing files, and manually deleted folders. This can make the filesystem and SQLite disagree.

Use:

```sh
gramli download reconcile
gramli download reconcile --apply
```

Reconcile scans downloaded post folders, matches them by shortcode, updates media rows to `downloaded`, and records post-level downloads where there is no direct media row. Placeholder Instagram media URLs such as `null.jpg` are marked `missing` instead of remaining `pending`.

## Rate Limits

Avoid aggressive runs. Use `--delay`, start with small limits, and resume later if Instagram throttles requests.

Practical defaults:

```sh
gramli posts sync --saved --collection saved --limit 100 --delay 4s
gramli download run --collection saved --limit 100 --metadata --strategy yt-dlp --delay 7s
```
