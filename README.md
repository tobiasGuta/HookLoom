# HookLoom

A small, **ephemeral HTTP request catcher and programmable response mocker** for authorized CTF challenges. Unlike a full Webhook.site installation, HookLoom has **no database, no Redis, no Node dependencies**, and no permanent capture storage. Built with the Go standard library for Fedora development and a future ARM64 Ubuntu/Azure deployment.

**Status:** `v0.7` pre-deployment security hardening review (draft); **local testing first**. Automated tests run in GitHub Actions on pushes and pull requests. It has not been deployed to the existing Azure Interactsh VM.

## What it does

- Creates unpredictable per-challenge paths such as `http://127.0.0.1:8765/c/<32-character-token>` using 192 bits of cryptographic randomness.
- Captures HTTP methods, path suffixes, query parameters, headers, and request bodies (text or base64 if binary).
- Returns an exact **custom HTTP response**: status code, body, headers (including `Location` for redirects), and 0–2000 ms delay.
- Supports **first-match conditional rules** on method, relative path prefix **or exact path**, query value, header value, or body substring.
- Has a private, organized three-pane dashboard: endpoint navigation, visual response editing, and request inspection. Captured content is rendered as text, not injected as HTML.
- The request inspector supports text search (path, query, and method), HTTP method filters, and quick readable request summaries.
- Expiration is automatic; all captured data disappears on expiration or process exit. A deleted/expired endpoint returns **410 Gone** for up to one hour, then **404 Not Found**.
- Limits concurrent active endpoints (8), per-endpoint requests (1–100), TTL (1–240 minutes), request body sizes (1–256 KiB), response sizes, and global retained capture data (~8 MiB accounting budget).
- Includes one-click response presets (JSON, text, HTML, redirects, 404, 500 and 204), editable header rows, and an optional advanced conditional-rule JSON editor. Presets require an explicit Save.
- The interface uses the calm three-column visual design: a selected-endpoint summary bar, endpoint statistics, separate request activity/details cards, a lined response body editor with a JSON formatter, a response preview tab, and an always-visible save action.

## Workspace layout

The **Overview** tab holds the editable default response: quick presets, status, delay, response headers, and an exact-text body editor. **Response** shows a preview of the configured HTTP status, headers, and body without sending a request; actual server-added headers can differ. **Rules** provides a visual conditional-rule editor and an advanced JSON switch, while **Requests** offers quick navigation to retained captures. The right pane provides search, HTTP method filters, and request-detail tabs.

Only explicit **Save changes** updates the endpoint. Changes to response headers and body remain drafts during normal auto-refresh, and the admin token is never saved in browser storage. The small top summary displays the **selected endpoint's capture URL**, not an arbitrary public listener URL.

## v0.6 — Rule-conflict warnings (review)

The **Rules** tab now detects when a prior rule **fully shadows** a later
rule, making that later rule unreachable. For example, a `GET` rule that
**starts with** `/callback` placed above an **exact** `/callback/admin` rule
will always win. The warning identifies both rule numbers and recommends
moving the more specific rule up or narrowing the earlier one.

- Warnings update as you edit, reorder, remove or add visual rules.
- The **Advanced JSON** editor displays the same warnings for valid JSON.
- The checker understands method, exact/prefix paths, query parameters,
  header names/values, body substrings and catch-all rules.
- This is an **advisory**, not an automatic change or a save blocker. Some
  partial overlaps are intentionally not flagged to avoid false positives.
- Matching, response behavior, listener bindings and capture retention are
  unchanged. The warning calculations run locally in the dashboard, without
  sending request data or admin tokens elsewhere.

For regression tests, run `node --test tests/rule_conflicts.test.cjs` alongside
`go test ./...`. GitHub Actions runs both automatically.

## v0.5 — Exact path matching

In **Rules → Visual builder**, each conditional rule's **Path** field now includes
**Starts with** (the existing default) and **Exact match**. Choose **Exact match**
and enter `/callback` to match only the suffix `/callback`, not `/callback/test`
or `/callbackxyz`. Matching is case-sensitive, and uses the path *after* your
endpoint token; query parameters do not affect the path comparison. Leaving a
path empty in exact mode is blocked by the editor. Other method/query/header/body
conditions still combine with the path restriction.

The same functionality is available through **Rules → Advanced JSON** with
`"match":{"pathExact":"/callback","method":"GET"}`. Older rules using
`"pathPrefix"` still work unchanged, and both keys cannot be set together in
one rule. Switching between editor modes preserves the chosen path operator.

For a quick local test, configure **GET + Exact match + `/callback`** to return
`302` and add the header `Location: /done`. Keep the default response at `200`.
Then check `/callback` (302), `/callback/test` (200), and `/callbackxyz` (200)
using your temporary capture URL. Select captured requests to see `Rule 1`
versus `Default` in their details.

## v0.4 — Visual conditional rules and improved request inspection

The **Rules** tab now opens a visual builder instead of requiring JSON. Add a rule,
choose a method and optional path prefix, expand **More match conditions** to
match query parameters, headers, or a body substring, and configure a custom
response status, delay, body, and header rows. Rules are evaluated **top to bottom**:
move them with the arrow controls, and use **Match all requests** only for an
intentional catch-all (normally placed last). You can still use **Advanced JSON**
to edit the exact existing rules array; switching views validates changes first.

Changes remain drafts until **Save changes**. The API and existing rule format are
unchanged. To exercise a new rule, select your endpoint's capture URL and send
requests with `curl`. For example, create a POST rule that matches query key
`role` with value `admin`, sets status `302` and a `Location: /done` response
header; then test with `curl -i -X POST 'YOUR_CAPTURE_URL?role=admin'`.

The **Request activity** panel now searches paths, query parameters, captured
headers, and bodies, and can filter on method and whether a conditional rule
matched. The selected request includes **Copy JSON** for troubleshooting; be
careful because captured request data may include credentials or other secrets.

The dashboard remains accessible only through the loopback admin listener, and
this version is **not** deployed to Azure. Restarting HookLoom still clears
in-memory endpoints and request history.

## Quick start on Fedora

Requires a **security-supported Go toolchain (currently 1.26.x or 1.27.x)**; `go.mod` retains 1.23 as the minimum language compatibility target. If you don't have it:

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

Click your endpoint in the left sidebar. The default response is `HTTP 200` with `{"ok":true}`. In the center pane, choose a response preset or edit the status, delay, header rows, and body; click **Save changes** to activate them. The **Rules** tab offers the visual builder and an Advanced JSON option. The right inspector displays incoming requests, with **Overview**, **Headers**, **Query**, **Body** (formatted JSON where possible), and **Raw** views. The dashboard refreshes request activity every five seconds **without replacing unsaved response edits**. Use **+ New** to open the endpoint-creation modal. The top-bar theme button switches between neutral light and dark layouts; the theme preference is stored locally, but the admin token remains in memory only.

**Do not use a real CTF target yet:** a `127.0.0.1` URL only reaches your own Fedora computer. We will publish the capture port through a separate HTTPS hostname after local testing is complete.

## Example conditional rule

To enter the equivalent rule as JSON, open **Rules → Advanced JSON**, paste the array below, then click **Save changes**:

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
- `-ingest` is loopback-only by default and **rejects non-loopback bindings** unless the operator explicitly opts in with `-allow-nonloopback-ingest`. For a remote CTF target, use an HTTPS reverse tunnel forwarding to **only the capture listener**. Never tunnel or reverse-proxy the admin listener. An opt-in alone does not make public exposure safe.
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

**Important:** Restarting destroys all temporary endpoints and captured requests by design. Export any lab evidence you wish to preserve first. The admin token is normally reused, so you can log in again with the same private file. If you previously relaxed the token file permissions, repair them locally with `chmod 600 ~/.config/hookloom/admin.token` before starting HookLoom.

The new throttling behavior may return HTTP 429/503 during deliberate stress tests; slow down requests or test against a local isolated instance. No changes to Azure, NSG, DNS, or Interactsh are required.

See [CHANGELOG.md](CHANGELOG.md) for the full v0.2 summary.

## Future improvements

- Body bytes response mode for binary payloads (current response body is text).
- Pattern/regex matching with carefully bounded resources.
- Optional response templating, without an arbitrary JavaScript or shell execution engine.
- On-demand rate limiting and request deduplication for public tunnel traffic.
- Additional tests for tunnel behavior and HTTPS callback compatibility.

**Licensing:** No license has been chosen yet. Source is visible, but reuse permissions are reserved until a license is added.
