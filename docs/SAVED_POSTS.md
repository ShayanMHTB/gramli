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

## Downloads

For mixed saved posts, use `yt-dlp` strategy:

```sh
gramli download run --collection saved --limit 25 --metadata --strategy yt-dlp --delay 5s
```

Images and albums use direct media URLs when available from the saved-post API. Videos and reels use `yt-dlp` and `ffmpeg` to merge audio/video streams into compatible MP4 files.

## Rate Limits

Avoid aggressive runs. Use `--delay`, start with small limits, and resume later if Instagram throttles requests.
