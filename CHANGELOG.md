# Changelog

## v0.3 — Organized workspace redesign (preview)

- Replace the old stacked dashboard with a responsive three-pane workspace: endpoint sidebar, response editor, and independent request inspector.
- Introduce a smaller top bar, endpoint search, a native dialog for creating endpoints, and clear session controls.
- Replace the JSON-only response-header field with removable key/value rows. Advanced conditional-rule JSON editing remains available.
- Add one-click response preset buttons and a save indicator; changes still require explicit saving.
- Add request inspector tabs (overview, headers, query, body, raw); pretty-print JSON request bodies without interpreting captured text as HTML.
- Update captured request activity every five seconds without overwriting unsaved response edits.
- Add neutral light and dark themes; only the visual theme is saved in browser local storage, not the admin token.
- Keep the backend API and its v0.2 hardening unchanged. No new runtime dependencies.

**Compatibility:** No API or token migration required. Restarting the process clears temporary sessions and captures as before. Not deployed to Azure.

## v0.2 — Functionality and security hardening

- Add a per-endpoint fixed-window throttle of 20 requests per second (`429` + `Retry-After`). Throttled requests do not consume the session quota.
- Limit in-flight capture handlers to 24 (`503` + `Retry-After`) to bound simultaneous work on small VMs.
- Reject admin request JSON above 128 KiB, including payloads with trailing whitespace, and reject non-object JSON bodies.
- Validate admin HTTP Host against loopback hostnames/IPs to help resist DNS rebinding.
- Require the explicit `Bearer` scheme on all admin API requests.
- Refuse saved admin tokens with unsafe file modes, symlinks or invalid token characters; validate custom environment tokens.
- Reject disallowed HTTP control characters in custom response headers.
- Account for captured query parameters in the global retained-data budget.
- Validate the advertised capture base URL, including its scheme, host, userinfo, path, query and fragment.
- Add quick response presets and pause dashboard auto-refresh during unsaved edits.
- Expand Go security/regression tests and add JavaScript syntax validation in CI.

**Compatibility:** The existing admin token normally remains valid. Any file readable by other users must first be changed to mode `0600`. The dashboard/admin listener remains loopback-only. The app remains a non-production local prototype; it is not deployed to Azure.

## v0.1 — Initial prototype

- Temporary random HTTP endpoints with in-memory request capture.
- Configurable HTTP responses and conditional matching rules.
- Private dashboard, session expiration, capture export, and bounded retention.
