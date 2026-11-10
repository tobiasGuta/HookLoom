# HookLoom security review — pre-Azure deployment

**Status:** Review in progress. Passing automated tests is not proof that a
public-facing service is safe. No Azure / Interactsh infrastructure changes
are performed by this review.

## Trust boundaries

| Boundary | Allowed traffic | Guardrail |
| --- | --- | --- |
| Internet → capture | Only the separate HTTPS capture hostname | Reverse proxy / tunnel to **127.0.0.1:8765 only**; edge rate limits and body caps |
| Tailscale workstation → administration | Private SSH tunnel to 127.0.0.1:8766 | Existing OpenSSH key + tailnet controls + HookLoom bearer token |
| Capture → admin | **No routing** | Two listeners, separate origins, no proxy rule to `/api`, no public 8766 |
| HookLoom → Interactsh | **No shared listening ports or runtime credentials** | Separate unprivileged service / directories and strict memory/CPU budgets |

HookLoom intentionally records supplied headers, cookies, authorization values,
query parameters and request bodies. Anyone with the random capture URL can
send data to it and exhaust the endpoint quota. **Keep capture URLs private**
while active and delete them promptly. Do not use the tool to receive real
customer secrets without a separate authorization, retention, and disclosure
review. Captures are RAM-only; JSON exports are not, and must be protected.

## Source-code controls already present

- Separate public capture and loopback-only admin listeners; admin API uses a
  random bearer token, constant-time comparison, same-origin checks, and Host
  validation. UI uses text APIs rather than injecting untrusted HTML.
- Admin browser responses include CSP, clickjacking, no-sniff and no-store
  protections. No admin token is stored in browser local storage.
- Session limits: 8 active sessions, short TTL, at most 100 requests each;
  at most 256 KiB per captured body; ~8 MiB retained capture accounting.
- HTTP timeouts, maximum header sizes, up to 24 concurrent in-flight capture
  requests and 20 requests/second per **known endpoint**.
- Custom response headers reject prohibited hop-by-hop headers and control
  characters. User-defined HTML responses are intentional **mock behavior**.
- Default capture listener is on loopback. In v0.7, public/interface-wide
  capture binds require explicit `-allow-nonloopback-ingest` opt-in. This is not
  permission to bypass the planned HTTPS ingress review.
- v0.7 also defaults capture responses to `Referrer-Policy: no-referrer`,
  reducing disclosure of random path tokens when a browser follows a link.

## Must resolve before any public Azure deployment

1. **Build with supported Go.** Go 1.23 no longer receives security fixes;
   CI uses Go 1.27.3. Confirm the *actual deployed binary* is compiled with a
   supported, patched Go release. Use `go version -m ./hookloom` locally on the
   built binary. Check official Go release notes for newer patch versions.
2. **Do not repurpose Interactsh's 53/80/443 listeners.** Confirm exactly how
   a *distinct public HTTPS hostname* can forward **only** to HookLoom's
   private capture port without modifying Interactsh's sockets, DNS
   delegation, or current NSG rules. Deploying another web server on TCP 443
   without a routing plan may break existing callbacks.
3. **Never expose the admin listener.** Bind it to `127.0.0.1:8766`, and
   access it via SSH port forwarding over Tailscale. Do not expose or proxy
   this port publicly, including through wildcard proxy routes. A tailnet
   device with access to the host still must pass SSH and HookLoom auth.
4. **Provide independent edge protection.** An attacker does not need a
   valid URL to flood the public listener. Per-endpoint rate limiting and
   per-process concurrency limits are not volumetric DDoS protection.
   Apply ingress-wide/IP-aware throttles, request-line and header/body caps,
   and transport timeouts at the proxy/tunnel edge. Test 429/413/503 behavior.
5. **Run with least privilege and resource isolation.** Use a dedicated
   unprivileged account, limited file permissions, non-root systemd unit,
   `NoNewPrivileges`, a read-only filesystem where practical, bounded memory
   and CPU, and no access to Interactsh secrets, service directories or
   private SSH keys. Validate sandboxing on Ubuntu ARM64 before enabling it.
6. **Keep sensitive traffic off logs.** Do not log full URLs containing
   capability tokens, request headers, cookies or request bodies in an edge
   proxy, tunnel provider, application logs, monitoring tool or GitHub issue.
   Review any upstream service's defaults before directing CTF traffic to it.
7. **Check external limitations.** Confirm DNS ownership and certificate
   management, deployment rollback path, Azure credit/egress costs, tailnet
   grants/ACLs, and monitoring for memory exhaustion/restarts. Document the
   exact ingress path before adding an Azure NSG allow rule.

## Release checklist

- [ ] CI on supported Go is green; manual `go test -race ./...` and
      `go vet ./...` are green.
- [ ] Add and run `govulncheck ./...` using a current vulnerability database.
- [ ] Verify invalid `-ingest 0.0.0.0:8765` fails unless explicitly opted in.
- [ ] Test auth failures, hostile Origin/Host, large bodies and headers,
      expired tokens, unknown tokens, redirect responses, and HTTP timeouts.
- [ ] Verify no public route or firewall rule reaches admin 8766.
- [ ] Verify ingress is a separate HTTPS host and does not touch Interactsh.
- [ ] Validate service memory/CPU caps and ensure Interactsh remains healthy.
- [ ] Confirm rollback is possible without altering the existing VM access.

## References

- [Go releases and support policy](https://go.dev/doc/devel/release)
- [OWASP Webhook Security Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Webhook_Security_Cheat_Sheet.html)
- [Azure network security groups](https://learn.microsoft.com/en-us/azure/virtual-network/network-security-groups-overview)
