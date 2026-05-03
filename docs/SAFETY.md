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

## Automation Boundary

Gramli may eventually support automation around local organization, metadata review, export, tagging, deduplication, and user-confirmed archival workflows.

Gramli should not automate abusive platform behavior. Future automation must not:

- bypass login challenges, 2FA, captchas, rate limits, or access controls
- mass-follow, mass-like, mass-comment, mass-DM, or spam users
- impersonate an official Instagram or Meta client
- hide what requests are being made
- use third-party or stolen sessions
- republish downloaded media without permission

Good automation targets are local-first and user-controlled: classifying your own archive, finding duplicates, generating summaries, planning retries, validating files, producing exports, and helping the user decide what to keep.
