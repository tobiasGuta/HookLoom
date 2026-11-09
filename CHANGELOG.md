# Changelog

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
