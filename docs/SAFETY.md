# Safety

Gramli is intended for personal archival of Instagram content visible to your own authenticated account.

## Gramli Does Not

- ask for Instagram passwords
- store plaintext passwords
- bypass 2FA, captchas, login challenges, rate limits, or private account protections
- use stolen, leaked, shared, or third-party sessions
- bypass paywalls, DRM, or access controls
- present itself as an official Instagram or Meta client
- republish or redistribute downloaded media

## Sessions

Authentication is based on a user-supplied browser cookie export. Treat cookie files like passwords.

Do not commit:

- cookie JSON files
- `.gramli/sessions/`
- `.gramli/cache/yt-dlp/cookies.txt`

## Downloading

Only download content that is visible to your authenticated account and that you are allowed to archive for personal use.

Use conservative batch sizes and delays:

```sh
gramli download run --collection saved --limit 25 --strategy yt-dlp --delay 5s
```
