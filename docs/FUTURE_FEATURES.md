# Future Features

This document tracks missing and aspirational features for turning Gramli into a complete, developer-friendly Instagram archival CLI.

The guiding constraint remains the same: Gramli should be local-first, transparent, user-controlled, and limited to content visible to the authenticated user. Automation should help organize, inspect, export, retry, and archive. It should not bypass Instagram security controls or automate abusive social actions.

## Near-Term Reliability

- Implement `download retry --failed`.
- Implement `download retry --pending`.
- Add `download retry --missing` for cases where refreshed metadata may replace placeholder URLs.
- Add `download failures list` with shortcode, owner, status, last error, and destination path.
- Add `download pending list` for the current open queue.
- Add `download verify` to check that local files exist, are non-empty, and have expected media extensions.
- Add checksum calculation for downloaded media.
- Add `download repair` for rows whose `local_path` points to missing files.
- Add `download reconcile --orphans` to list folders not present in SQLite.
- Add `download reconcile --json` examples and tests.
- Add progress summaries that persist after interrupted runs.
- Add retry backoff for transient network failures.
- Add better terminal summaries after batch runs: downloaded, skipped, failed, missing, already archived, and elapsed time.

## Metadata Completeness

- Store richer post metadata from saved-post API responses.
- Preserve raw saved-post API JSON in `.gramli/cache/saved/`.
- Add metadata migrations for location, product tags, music/audio title, coauthors, accessibility captions, hashtags, mentions, and permalink type.
- Detect unavailable/deleted posts and mark them explicitly.
- Track saved collection membership changes over time.
- Add `posts refresh <shortcode>` to refetch one post.
- Add `posts refresh --collection saved --limit N`.
- Add `posts stale` to show records not seen in the latest sync.
- Add `posts missing-media` to show posts whose media rows are missing or placeholder-only.
- Add `posts owners` to list creators with counts.
- Add `posts hashtags` to extract and list hashtags from captions.
- Add `posts mentions` to extract and list mentioned accounts.

## Collections

- Improve saved collection sync where technically feasible.
- Add collection-level download status.
- Add `collections stats`.
- Add `collections export`.
- Add local tags independent of Instagram collections.
- Add `collections merge-local`.
- Add `collections split-local`.
- Add `collections add-post`.
- Add `collections remove-post`.
- Add support for user-defined smart collections, for example by owner, media type, caption text, or hashtag.

## Downloading

- Add a downloader queue table.
- Add resumable jobs with stable job IDs.
- Add `download run --resume-job <id>`.
- Add `download run --pending`.
- Add `download run --failed`.
- Add `download run --missing`.
- Add concurrency controls that are conservative by default.
- Add total-size limits with `--max-bytes`.
- Add per-owner and per-post folder naming templates for yt-dlp downloads.
- Add filename collision handling.
- Add content-aware thumbnail cleanup.
- Add sidecar Markdown generation for every downloaded post.
- Add media probe using `ffprobe` to record codec, duration, dimensions, and audio presence.
- Add `download verify --audio` for video files.
- Add `download transcode` to normalize media into H.264/AAC MP4 when requested.
- Add `download keep-originals` and `download remove-transients`.
- Add dry-run planning that estimates number of posts and media files before download.

## Exporting

- Add complete JSON export with posts, media, collections, accounts, and download attempts.
- Add JSON Lines export for large archives.
- Add richer CSV exports for posts, media, collections, and failures.
- Add Markdown archive index pages.
- Add static HTML gallery export.
- Add SQLite copy export.
- Add `export --since` and `export --until`.
- Add `export --missing`, `export --failed`, and `export --pending`.
- Add stable schema versioning in exported files.
- Add export manifests with file checksums.

## Search And Local Indexing

- Add full-text search with SQLite FTS5.
- Search captions, owner usernames, hashtags, mentions, collection names, and local notes.
- Add `posts note <shortcode>`.
- Add `posts tag <shortcode>`.
- Add duplicate detection by shortcode, media URL, checksum, and perceptual hash.
- Add local favorites.
- Add saved query support.
- Add `search --json` for scripting.
- Add owner-level and collection-level statistics.

## AI-Assisted Local Workflows

These features should operate on local metadata and downloaded files unless the user explicitly asks for a network action.

- Generate local summaries of saved collections.
- Classify posts into local topics.
- Suggest local tags from captions, owner names, and visual metadata.
- Build natural-language queries over the local archive.
- Generate Markdown catalogs.
- Detect likely recipes, workouts, travel ideas, design references, products, memes, tutorials, and reading material.
- Produce local todo lists from saved tutorial posts.
- Generate embeddings for captions and local notes.
- Add semantic search over captions and generated summaries.
- Add `gramli ask` for questions over the local archive.
- Add review queues, for example "show posts likely worth downloading first."
- Add AI-generated folder organization proposals that require user confirmation before changes.
- Add AI-generated retry plans based on previous errors.

## Developer And Scripting Experience

- Stabilize JSON output for all read commands.
- Add `--json` support to every status/list/show command.
- Add shell completion documentation.
- Add manpage generation.
- Add examples for piping to `jq`.
- Add a public Go package for archive queries.
- Add machine-readable error codes everywhere.
- Add structured logs for sync, download, reconcile, export, and auth checks.
- Add `gramli inspect db` helpers for debugging.
- Add a `--profile` or `--account` flag across commands.
- Add Makefile targets.
- Add GitHub Actions.
- Add release builds with goreleaser.
- Add installation instructions for Homebrew, `go install`, and direct binaries.

## Browser And Authentication

- Add documented browser-assisted login flow.
- Add cookie import validation with clearer messages.
- Add session expiry detection.
- Add `auth cookies --show-path`.
- Add `auth cookies --validate`.
- Add `auth refresh --web` skeleton.
- Add multi-account switching.
- Add session file permission checks in `doctor`.
- Redact sensitive headers in all logs.

## Database And Migrations

- Add explicit migration files or embedded migration assets.
- Add `db backup`.
- Add `db restore`.
- Add `db integrity`.
- Add `db vacuum --analyze`.
- Add migration tests.
- Add indexes for common filters.
- Add jobs table for sync and download jobs.
- Add audit tables for status transitions.
- Add archive health views.

## Terminal UX

- Add compact and detailed table modes.
- Add progress bars for long sync and download runs.
- Add colorized status output with `--no-color` support.
- Add confirmation prompts for large downloads.
- Add `--yes` behavior consistently.
- Add quiet mode for scripts.
- Add verbose mode with request and retry summaries.
- Add better final summaries for long runs.

## Packaging

- Add release workflow.
- Add signed checksums.
- Add Homebrew tap.
- Add Docker image for metadata-only workflows.
- Add reproducible build notes.
- Add version injection for commit and build date.
- Add upgrade notes and changelog.

## Compliance And Product Boundaries

- Keep all data local by default.
- Keep `.gramli/` ignored.
- Do not store passwords.
- Do not print cookies.
- Do not bypass challenges, captchas, private account protections, or rate limits.
- Do not provide mass-like, mass-follow, mass-comment, mass-DM, or spam features.
- Keep automation inspectable and user-confirmed.
- Prefer metadata-only defaults.
- Make destructive commands require `--yes`.
- Keep documentation clear that Gramli is not an official Instagram or Meta client.
