# App Review demo family

The reviewer environment is a real isolated Coop deployment populated only with fictional people and intentionally selected public channels.
It is not a hard-coded demo mode and it never uses credentials committed to this repository.

## Build the fixture

1. Deploy the exact release candidate to a dedicated hostname and database.
2. Create a dedicated Google Cloud project and YouTube API key for the review environment.
3. In Cooper The Cop, create a fictional admin and enroll a dedicated TOTP seed.
4. Create at least two fictional children with different settings and channel scopes.
5. Approve a small set of public channels whose current content has been manually reviewed.
6. Add one negative keyword that visibly suppresses a known video, then leave the suppression un-overridden.
7. Create a pending channel request from Cooper Watch.
8. Add a second scoped parent so App Review can verify that one adult cannot see another child's records.
9. Exercise likes, dislikes, subscriptions, and watch progress so recommendation explanations have data.
10. Pair a dedicated review device and verify playback through `youtube-nocookie.com`.

Channel suitability changes over time, so the release owner must re-review the selected channels before every submission.
That is why the fixture deliberately contains no repository-owned channel IDs.

## Hand off to App Review

Put the current server URL, admin email, password, TOTP seed, pairing instructions, and the exact workflow to test in App Store Connect review notes.
Use credentials unique to that submission and rotate them after review.
Do not place secrets in screenshots, git, CI logs, release assets, or public support documentation.

Before submission, rehearse login, TOTP, pending approval, playback, scoped-parent visibility, account deletion, and a fresh reinstall of both apps.
Keep the environment running and monitored for the entire review window.
