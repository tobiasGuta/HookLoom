# HookLoom

A small, **ephemeral HTTP request catcher and programmable response mocker** for authorized CTF challenges. Unlike a full Webhook.site installation, HookLoom has **no database, no Redis, no Node dependencies**, and no permanent capture storage. Built with the Go standard library for Fedora development and a future ARM64 Ubuntu/Azure deployment.

**Status:** `v0.3` interface preview; **local testing first**. Automated tests run in GitHub Actions on pushes and pull requests. It has not been deployed to the existing Azure Interactsh VM.

## What it does

- Creates unpredictable per-challenge paths such as `http://127.0.0.1:8765/c/<32-character-token>` using 192 bits of cryptographic randomness.
- Captures HTTP methods, path suffixes, query parameters, headers, and request bodies (text or base64 if binary).
- Returns an exact **custom HTTP response**: status code, body, headers (including `Location` for redirects), and 0–2000 ms delay.
- Supports **first-match conditional rules** on method, relative path prefix, query value, header value, or body substring.
- Has a private, organized three-pane dashboard: endpoint navigation, visual response editing, and request inspection. Captured content is rendered as text, not injected as HTML.
- Expiration is automatic; all captured data disappears on expiration or process exit. A deleted/expired endpoint returns **410 Gone** for up to one hour, then **404 Not Found**.
- Limits concurrent active endpoints (8), per-endpoint requests (1–100), TTL (1–240 minutes), request body sizes (1–256 KiB), response sizes, and global retained capture data (~8 MiB accounting budget).
- Includes one-click response presets (JSON, text, HTML, redirects, 404, 500 and 204), editable header rows, and an optional advanced conditional-rule JSON editor. Presets require an explicit Save.

## Quick start on Fedora

Requires **Go 1.23+**. If you don't have it:

```bash
sudo dnf install golang
```

In the project folder:

```bash
go test ./...
go run .
```

The app uses two **loopback-only** endpoints by default:

| Address | Purpose |
| --- | --- |
| `http://127.0.0.1:8766` | Private management dashboard (requires admin token) |
| `http://127.0.0.1:8765/c/<token>` | CTF request receiver (unique secret URL) |

Your first run automatically generates a **persistent admin access token** under `~/.config/hookloom/admin.token` (mode `0600`). This is an authentication secret, **not a database**. Keep it private. To paste it into the dashboard's login box, display it **locally on Fedora**:

```bash
cat ~/.config/hookloom/admin.token
```

Don't post it in GitHub or chat, and don't add it to HTTP requests being captured.

Open `http://127.0.0.1:8766` in your Fedora browser, enter the admin token, and click **Create endpoint**. You will get a random URL. For example, to trigger a harmless local test:

```bash
curl -i -X POST 'http://127.0.0.1:8765/c/PASTE_YOUR_ENDPOINT_TOKEN/submit?test=one' \
  -H 'Content-Type: application/json' \
  -H 'X-CTF: selftest' \
  --data '{"challenge":"local-test"}'
```

Click your endpoint in the left sidebar. The default response is `HTTP 200` with `{"ok":true}`. In the center pane, choose a response preset or edit the status, delay, header rows, and body; click **Save response** to activate changes. Advanced conditional rules remain available in the collapsible JSON editor. The right inspector displays incoming requests, with **Overview**, **Headers**, **Query**, **Body** (formatted JSON where possible), and **Raw** views. The dashboard refreshes request activity every five seconds **without replacing unsaved response edits**. Use **+ New** to open the endpoint-creation modal. The top-bar theme button switches between neutral light and dark layouts; the theme preference is stored locally, but the admin token remains in memory only.

**Do not use a real CTF target yet:** a `127.0.0.1` URL only reaches your own Fedora computer. We will publish the capture port through a separate HTTPS hostname after local testing is complete.

## Example conditional rule

Paste this JSON array into the dashboard's **Conditional response rules** field, then save:

```json
[
  {
    "match": {
      "method": "GET",
      "pathPrefix": "/api",
      "queryKey": "role",
      "queryValue": "admin",
      "headerKey": "X-CTF",
      "headerValue": "yes"
    },
    "response": {
      "status": 302,
      "headers": { "Location": "/complete", "X-Lab": "ctf" },
      "body": "",
      "delayMs": 0
    }
  }
]
```

Then:

```bash
curl -i 'http://127.0.0.1:8765/c/PASTE_YOUR_ENDPOINT_TOKEN/api?role=admin' \
  -H 'X-CTF: yes'
```

You should see `302` and `Location: /complete`. When the rule doesn't match, the endpoint returns its default response. You can configure several rules in priority order. The request record displays the index of the matched rule (starting at 0), or `-1` for the default.

## Security design

**Management and capture are two separate HTTP servers:**

- `-admin` **must** bind to a numeric loopback address (for example `127.0.0.1:8766`); the program refuses public admin addresses and hostname-based listener configuration. The admin server rejects unexpected HTTP `Host` values to mitigate DNS rebinding. The administration API requires a separate, persistent random **Bearer token**. The browser holds it only in memory. Admin responses use no-store, restrictive browser headers and a Content Security Policy. Captured HTML is shown as text, never injected into the dashboard as HTML.
- `-ingest` is loopback by default. For a remote CTF target, use an HTTPS reverse tunnel forwarding to **only the capture listener**. Never tunnel or reverse-proxy the admin listener.
- CTF callback URLs are **capability URLs**. Anyone possessing one can send data to it and consume its quota. Treat the URLs as sensitive while active and delete them when done.
- All request data, including headers/cookies and bodies, is held **only in RAM**, subject to limits. Sessions are gone after application restart or expiration. No access logs containing request data are written by the app. Exports are downloaded locally; protect the downloaded files separately.
- Go server timeouts and header-size limits mitigate basic resource abuse. v0.2 also admits at most **24 simultaneous in-flight capture requests** and **20 requests per second per endpoint**. A rate-limited callback receives `429` (with `Retry-After: 1`) without consuming its lifetime quota; when the concurrency cap is reached, the service returns `503` (with `Retry-After: 1`). These coarse limits are **not full DDoS protection**. This is a **personal prototype**, not a hardened multi-user Internet service. Do not expose the capture listener directly to the Internet without external rate limiting and abuse controls.
- Admin configuration JSON is rejected above **128 KiB**, including trailing whitespace. Response header names and values reject forbidden control characters. The saved admin-token file must be a regular file with restrictive permissions (`0600`); symlinks and world-readable files are rejected. Custom `HOOKLOOM_ADMIN_TOKEN` values must contain **32–128 base64url characters** (`A–Z`, `a–z`, `0–9`, `_`, `-`).
- Avoid capturing real credentials or unauthorized targets. This app is for systems you own or authorized CTF exercises.

### Limits (v0.2)

| Setting | Default | Allowed |
| --- | --- | --- |
| Active endpoints | — | Maximum 8 |
| URL lifetime | 30 minutes | 1–240 minutes |
| Requests per endpoint | 50 | 1–100 |
| Request body retained | 64 KiB | 1–256 KiB |
| Response delay | 0 ms | 0–2000 ms |
| Response body | small JSON | Maximum 64 KiB |
| Total captured data | — | ~8 MiB accounting budget; oldest records evicted |
| Simultaneous capture requests | — | 24 (additional requests return 503) |
| Requests per second per endpoint | — | 20 (additional requests return 429) |
| Administration JSON size | — | 128 KiB |

An oversized body is recorded as **truncated** and receives `413 Request Entity Too Large`, instead of the configured response. The memory budget can evict older captures before the endpoint itself expires. A session that reaches its request limit remains visible for export until its TTL ends, but additional callbacks receive `410`.

## Future Azure integration (not yet deployed)

Your existing Interactsh VM already uses TCP 80, TCP 443, and DNS 53. **Do not modify these listeners or the Azure NSG for the CTF prototype.** Later, we can:

1. Cross-compile for ARM64: `GOOS=linux GOARCH=arm64 go build -o hookloom-linux-arm64 .`
2. Run the catcher as a **separate, unprivileged systemd service**, initially only on `127.0.0.1:8765` and private admin on `127.0.0.1:8766`.
3. Make only the capture listener accessible under a separate Namecheap subdomain, through an appropriate HTTPS tunnel/reverse-proxy arrangement that does not intercept Interactsh traffic.
4. Reach the private dashboard via an SSH local port forward over Tailscale:

   ```bash
   ssh -N -L 8766:127.0.0.1:8766 \
     -i ~/.ssh/azure_interactsh azureuser@<AZURE_TAILSCALE_IP>
   ```

5. Review outbound tunnel trust, memory usage, and Azure spend before enabling it permanently.

Do not publish Interactsh secrets, Azure public IPs, authentication tokens, or CTF capture contents to the repository.

## Working with the code

```bash
go test -race ./...
go vet ./...
node --check web/app.js  # optional: requires Node.js
go build -o hookloom .
```

Stop the local server with `Ctrl+C`. Captures and active endpoints are deliberately lost on shutdown; the admin authentication token remains in `~/.config/hookloom/admin.token` for the next run.

## Upgrade notes for v0.2

Pull and restart the local process after checking for unexported captures:

```bash
git pull --ff-only
go test -race ./...
go run .
```

**Important:** Restarting destroys all temporary endpoints and captured requests by design. Export any lab evidence you wish to preserve first. The admin token is normally reused, so you can log in again with the same private file. If you previously relaxed the token file permissions, repair them locally with `chmod 600 ~/.config/hookloom/admin.token` before starting v0.2.

The new throttling behavior may return HTTP 429/503 during deliberate stress tests; slow down requests or test against a local isolated instance. No changes to Azure, NSG, DNS, or Interactsh are required.

See [CHANGELOG.md](CHANGELOG.md) for the full v0.2 summary.

## Future improvements

- Body bytes response mode for binary payloads (current response body is text).
- Pattern/regex matching with carefully bounded resources.
- Optional response templating, without an arbitrary JavaScript or shell execution engine.
- On-demand rate limiting and request deduplication for public tunnel traffic.
- Additional tests for tunnel behavior and HTTPS callback compatibility.

**Licensing:** No license has been chosen yet. Source is visible, but reuse permissions are reserved until a license is added.
