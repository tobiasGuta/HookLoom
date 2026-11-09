# HookLoom

A small, **ephemeral HTTP request catcher and programmable response mocker** for authorized CTF challenges. Unlike a full Webhook.site installation, HookLoom has **no database, no Redis, no Node dependencies**, and no permanent capture storage. Built with the Go standard library for Fedora development and a future ARM64 Ubuntu/Azure deployment.

**Status:** `v0.1` working prototype; **local testing first**. Automated tests run in GitHub Actions on pushes and pull requests. It has not been deployed to the existing Azure Interactsh VM.

## What it does

- Creates unpredictable per-challenge paths such as `http://127.0.0.1:8765/c/<32-character-token>` using 192 bits of cryptographic randomness.
- Captures HTTP methods, path suffixes, query parameters, headers, and request bodies (text or base64 if binary).
- Returns an exact **custom HTTP response**: status code, body, headers (including `Location` for redirects), and 0–2000 ms delay.
- Supports **first-match conditional rules** on method, relative path prefix, query value, header value, or body substring.
- Has a private browser dashboard for creating endpoints, inspecting requests, editing rules, exporting JSON, and destroying endpoints.
- Expiration is automatic; all captured data disappears on expiration or process exit. A deleted/expired endpoint returns **410 Gone** for up to one hour, then **404 Not Found**.
- Limits concurrent active endpoints (8), per-endpoint requests (1–100), TTL (1–240 minutes), request body sizes (1–256 KiB), response sizes, and global retained capture data (~8 MiB accounting budget).

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

Click your endpoint in the dashboard to view the request. The default response is `HTTP 200` with `{"ok":true}`. Test custom response bodies and headers using the response configuration form.

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

- `-admin` **must** bind to loopback; the program refuses public admin addresses. The administration API requires a separate, persistent random **Bearer token**. The browser holds it only in memory. Admin responses use no-store, restrictive browser headers and a Content Security Policy. Captured HTML is shown as text, never injected into the dashboard as HTML.
- `-ingest` is loopback by default. For a remote CTF target, use an HTTPS reverse tunnel forwarding to **only the capture listener**. Never tunnel or reverse-proxy the admin listener.
- CTF callback URLs are **capability URLs**. Anyone possessing one can send data to it and consume its quota. Treat the URLs as sensitive while active and delete them when done.
- All request data, including headers/cookies and bodies, is held **only in RAM**, subject to limits. Sessions are gone after application restart or expiration. No access logs containing request data are written by the app. Exports are downloaded locally; protect the downloaded files separately.
- Go server timeouts and header-size limits mitigate basic resource abuse. This is a **personal prototype**, not a hardened multi-user Internet service. Do not expose the capture listener directly to the Internet without additional rate limiting and abuse controls.
- Avoid capturing real credentials or unauthorized targets. This app is for systems you own or authorized CTF exercises.

### Limits (v0.1)

| Setting | Default | Allowed |
| --- | --- | --- |
| Active endpoints | — | Maximum 8 |
| URL lifetime | 30 minutes | 1–240 minutes |
| Requests per endpoint | 50 | 1–100 |
| Request body retained | 64 KiB | 1–256 KiB |
| Response delay | 0 ms | 0–2000 ms |
| Response body | small JSON | Maximum 64 KiB |
| Total captured data | — | ~8 MiB accounting budget; oldest records evicted |

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
go build -o hookloom .
```

Stop the local server with `Ctrl+C`. Captures and active endpoints are deliberately lost on shutdown; the admin authentication token remains in `~/.config/hookloom/admin.token` for the next run.

## Future improvements

- Body bytes response mode for binary payloads (current response body is text).
- Pattern/regex matching with carefully bounded resources.
- Optional response templating, without an arbitrary JavaScript or shell execution engine.
- On-demand rate limiting and request deduplication for public tunnel traffic.
- Additional tests for tunnel behavior and HTTPS callback compatibility.

**Licensing:** No license has been chosen yet. Source is visible, but reuse permissions are reserved until a license is added.
