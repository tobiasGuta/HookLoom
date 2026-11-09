# Changelog

## v0.5 — Exact path matching (review)

- Add `pathExact` as a case-sensitive alternative to existing `pathPrefix` in
  conditional rule matching, with validation rejecting ambiguous dual-mode
  matches and paths without a leading slash.
- Provide a **Starts with / Exact match** selector in the visual rule builder.
  Existing prefix rules remain unchanged; Advanced JSON round-trips either
  representation without losing the selected matching behavior.
- Add API-level regression tests for exact-path boundaries, wrong methods,
  saved rules, captured match indexes, legacy prefix matching and validation.
- No new dependencies and no changes to listener bindings, admin security,
  Interactsh or Azure deployment.


## v0.4 — Visual rules and request inspection (review)

- Introduce a visual conditional-rule builder with method/path matching,
  optional query/header/body conditions, status/delay/body settings, and
  per-rule response header rows.
- Provide rule order controls, explicit catch-all matching, validation to
  prevent accidental match-all rules, and a lossless Advanced JSON editor
  toggle for the existing API shape.
- Expand request search across path, query parameters, headers, and bodies.
  Add a matched-rule/default-response filter and a Copy JSON action for
  individual captured requests, with a sensitive-data reminder.
- Maintain the v0.3 layout, neutral visual design, loopback-only admin,
  and unchanged Go backend API; no new runtime dependencies.
- Test the frontend with automated browser interaction checks and validate
  rule matching and default responses against the live Go service.

**Compatibility:** No configuration or token migration needed. Temporary
captures remain in RAM and disappear when the process stops. Not deployed to Azure.


## v0.3 — Organized workspace redesign

- Match the cleaner reference design with a compact workspace summary, more coherent endpoint identity and three-column spacing, separate request activity/details cards, and a less crowded editor.
- Add a line-numbered exact-text response body editor with an opt-in JSON formatter; keep header editing and response presets.
- Make the center Response tab show an actual read-only preview of the default response and keep Requests tab content updated as new captures arrive.
- Verify desktop and mobile views with browser-based frontend interaction tests against a stubbed administration API; backend Go tests and race checks still pass.
- Replace the old stacked dashboard with a responsive three-pane workspace: endpoint sidebar, response editor, and independent request inspector.
- Introduce a smaller top bar, endpoint search, a native dialog for creating endpoints, and clear session controls.
- Replace the JSON-only response-header field with removable key/value rows. Advanced conditional-rule JSON editing remains available.
- Add one-click response preset buttons and a save indicator; changes still require explicit saving.
- Add request inspector tabs (overview, headers, query, body, raw); pretty-print JSON request bodies without interpreting captured text as HTML.
- Update captured request activity every five seconds without overwriting unsaved response edits.
- Add neutral light and dark themes; only the visual theme is saved in browser local storage, not the admin token.
- Refine the v0.3 preview with request search and HTTP method filtering, readable method/path/time/size activity rows, clearer endpoint statistics, and sticky desktop navigation/inspector panels.
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
