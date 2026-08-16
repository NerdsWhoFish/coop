# Draft privacy data inventory

This inventory describes the current product behavior so privacy counsel can approve the public declarations.
It is not a legal conclusion and does not replace the answers published in App Store Connect.

## Cooper The Cop

| Data | Why Coop uses it | Linked | Tracking | Storage and recipient |
| --- | --- | --- | --- | --- |
| Parent email address | Account authentication and family administration | Yes | No | Self-hosted Coop server and PostgreSQL |
| Parent account identifier | Session ownership, permissions, and policy attribution | Yes | No | Self-hosted Coop server and PostgreSQL |
| Family policy changes | Apply parental controls and preserve an audit history | Yes | No | Self-hosted Coop server and PostgreSQL |
| YouTube API key | Retrieve channels and videos chosen by the family | Yes | No | Encrypted in self-hosted PostgreSQL and sent to Google with API requests |

## Cooper Watch

| Data | Why Coop uses it | Linked | Tracking | Storage and recipient |
| --- | --- | --- | --- | --- |
| Child profile and device identifiers | Pair the device and apply the correct policy | Yes | No | Self-hosted Coop server and PostgreSQL |
| Video views, completion, playback position, and reactions | Continue playback, show history, and rank recommendations | Yes | No | Self-hosted Coop server and PostgreSQL |
| Subscriptions and channel requests | Build the approved feed and request parent decisions | Yes | No | Self-hosted Coop server and PostgreSQL |
| Search terms | Return policy-filtered search results | Yes, while the request is processed | No | Sent to the self-hosted Coop server and Google YouTube Data API, then discarded |
| Embedded playback requests | Play an approved YouTube video | Potentially | No Coop tracking | Sent from the device to YouTube's privacy-enhanced embed host |

## Open questions before publication

- Confirm what identifiers, cookies, or network metadata YouTube receives from `youtube-nocookie.com` during playback.
- Confirm whether crash reporting or diagnostics will be added before release.
- Decide the Cooper Watch Kids Category status and applicable parental-consent process.
- Confirm the final archive's export classification.

## Current retention behavior

- Search text is processed in memory and is not written to PostgreSQL or application logs.
- A per-child daily search count is retained only for the current quota day and is purged by the daily cleanup job.
- Parent sessions expire after 30 days by default, authentication challenges after 5 minutes, pairing codes after 15 minutes, and invitations after 7 days.
- Expired operational rows are removed on startup and daily thereafter.
- Watch activity, reactions, subscriptions, requests, suppression history, policy audit events, children, parents, and revoked device records remain until their child or family is deleted.
- Deleting a family from Cooper The Cop cascades through all tenant-owned PostgreSQL records, including the audit history.
- Application request logs contain the URL path but omit query strings, request bodies, credentials, and search text.
- Reverse-proxy, platform, database-backup, and infrastructure-log retention is controlled by the self-hosting operator and must be disclosed in that operator's privacy policy.
