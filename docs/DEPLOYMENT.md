# Deploying Coop

Coop is a household service, not a public multi-tenant SaaS.
Run one backend per household behind HTTPS, keep PostgreSQL private, and expose only the reverse proxy.

## 1. Create the Google API key

1. Create a dedicated project in [Google Cloud Console](https://console.cloud.google.com/).
2. Open **APIs and Services**, then **Library**, and enable **YouTube Data API v3**.
3. Open **Credentials**, create an API key, and restrict its API scope to **YouTube Data API v3**.
4. If the Coop server has a stable public egress address, also add an IP-address application restriction for that address.
5. Do not add an iOS bundle restriction because the key is used by the server, not either app.
6. Paste the key into the parent app after the first admin signs in.

Every installation needs its own key.
Never bake a key into an image, Helm value, mobile app, screenshot, or demo fixture.

The default YouTube quota is finite and cannot be purchased as an ordinary paid upgrade.
Coop budgets feed, search, and backfill work separately so a search burst cannot starve normal ingestion.

## 2. Choose a deployment

Use [Docker Compose](../deploy/compose/README.md) for one conventional Linux host.
Use the [Helm chart](../deploy/helm/coop/README.md) when PostgreSQL, ingress, secrets, certificates, monitoring, and backups are already cluster services.

In both cases:

- Use an immutable released image digest.
- Store the PostgreSQL DSN and 32-byte authentication encryption key in secret storage.
- Back up the database and encryption key separately.
- Route public traffic through HTTPS only.
- Complete initial setup before exposing the hostname beyond the administrator's network.

## 3. Connect the apps

Install Cooper The Cop on the administrator's device and enter the public HTTPS origin, such as `https://coop.example.net`.
The app appends the API path itself.
Create the family, enroll TOTP, save the recovery code in the authenticator, and configure the YouTube key.

Create each child profile and generate a pairing code from the parent app.
Install Cooper Watch on the child's device, enter the same server origin, and redeem that code.
Pairing codes are single-use and short-lived.

## 4. Restrict the child device

Coop controls what its own app serves, but it cannot stop a child from opening YouTube somewhere else.
Use both device restrictions and network filtering.

On iPhone or iPad, use Screen Time to prevent installing or deleting apps, restrict account changes, and remove or constrain browsers as appropriate for the household.
Do not rely on Screen Time alone because another browser or an already-installed YouTube app can bypass Coop.

Apply these DNS rules to the child's device or network policy group:

| Host | Rule | Reason |
| --- | --- | --- |
| `www.youtube.com` | Block | YouTube web client |
| `m.youtube.com` | Block | YouTube mobile web client |
| `youtubei.googleapis.com` | Block | Native YouTube application API |
| `www.youtube-nocookie.com` | Allow | Official player embedded by Coop |
| `*.googlevideo.com` | Allow | Video media used by the embedded player |
| `i.ytimg.com` | Block or omit | Coop proxies thumbnails |

Apply the rules per device when possible so they do not break YouTube for adults on the same network.
Test the native YouTube app, Safari, Coop playback, and playback after moving the child device between Wi-Fi and cellular.
A home-only DNS policy does nothing on cellular or another Wi-Fi network, so use a device-level DNS or VPN profile if the device leaves home.

## 5. Operate and recover

Use `/livez` for process liveness and `/readyz` for traffic readiness.
Readiness includes PostgreSQL and should be the load balancer or orchestrator health check.

The server runs migrations before accepting traffic.
Read release notes before rolling back across a schema change.

If a parent loses their authenticator, run this on the Coop host:

```console
coopd auth-reset-totp --email parent@example.com
```

That command clears the enrollment, invalidates in-flight challenges, and revokes every session for that parent.
The next password login starts a new TOTP enrollment.

If an administrator confirms that an email-specific login lockout is accidental, run:

```console
coopd auth-unlock --email parent@example.com
```

This preserves source-address throttles so unlocking one account does not unblock a hostile address.
Neither recovery operation is exposed over HTTP.

The parent app can permanently delete the family and all tenant data from **Family desk**.
Database backups and external server logs are outside that transaction and must follow the operator's published retention policy.

## 6. Back up and restore

Back up a PostgreSQL custom-format dump and the exact authentication encryption key.
Without the matching key, restored YouTube credentials and TOTP enrollments cannot be decrypted.

Restore into a fresh database with the Coop server stopped, restore the matching key, run `coopd migrate`, and check `/readyz` before returning traffic.
Perform a restoration drill before treating the backup as real.

## 7. Prepare an App Review demo

Follow the fictional-data procedure in [deploy/demo/README.md](../deploy/demo/README.md).
Do not publish a universal demo password, TOTP secret, API key, or pairing code in this repository.
