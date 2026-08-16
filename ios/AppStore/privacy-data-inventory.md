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
| Search terms | Return policy-filtered search results | Pending retention audit | No | Sent to the self-hosted Coop server and Google YouTube Data API |
| Embedded playback requests | Play an approved YouTube video | Potentially | No Coop tracking | Sent from the device to YouTube's privacy-enhanced embed host |

## Open questions before publication

- Confirm whether search terms are persisted anywhere or only processed in flight.
- Confirm what identifiers, cookies, or network metadata YouTube receives from `youtube-nocookie.com` during playback.
- Define retention and deletion periods for activity, policy audit, sessions, revoked devices, and server logs.
- Confirm whether crash reporting or diagnostics will be added before release.
- Decide the Cooper Watch Kids Category status and applicable parental-consent process.
- Confirm the final archive's export classification.
