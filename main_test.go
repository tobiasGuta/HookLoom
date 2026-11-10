package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func runAdmin(t *testing.T, a *app, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var b *bytes.Reader
	if body == nil {
		b = bytes.NewReader(nil)
	} else {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		b = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, b)
	req.Host = "127.0.0.1:8766"
	req.Header.Set("Authorization", "Bearer test-admin-token")
	result := httptest.NewRecorder()
	a.adminMux().ServeHTTP(result, req)
	return result
}
func makeSession(t *testing.T, a *app, options creationRequest) session {
	t.Helper()
	rec := runAdmin(t, a, "POST", "/api/endpoints", options)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create got %d: %s", rec.Code, rec.Body.String())
	}
	var s session
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	if !tokenRE.MatchString(s.Token) {
		t.Fatalf("bad token %q", s.Token)
	}
	return s
}
func newTestApp() *app { return newApp("http://127.0.0.1:8765", "test-admin-token") }
func TestCreationAndCapture(t *testing.T) {
	a := newTestApp()
	s := makeSession(t, a, creationRequest{Name: "challenge", TTLMinutes: 30, MaxRequests: 3, BodyLimitKiB: 8})
	if len(s.Token) != 32 {
		t.Fatalf("token length %d", len(s.Token))
	}
	req := httptest.NewRequest("POST", "/c/"+s.Token+"/submit?q=one", strings.NewReader("hello CTF"))
	req.Header.Set("X-Lab", "active")
	rr := httptest.NewRecorder()
	a.publicMux().ServeHTTP(rr, req)
	if rr.Code != 200 || strings.TrimSpace(rr.Body.String()) != `{"ok":true}` {
		t.Fatalf("response: %d %s", rr.Code, rr.Body.String())
	}
	got := runAdmin(t, a, "GET", "/api/endpoints/"+s.Token, nil)
	if got.Code != 200 {
		t.Fatal(got.Code)
	}
	var read session
	if err := json.Unmarshal(got.Body.Bytes(), &read); err != nil {
		t.Fatal(err)
	}
	if read.Seen != 1 || len(read.Captures) != 1 {
		t.Fatalf("captures: %d/%d", read.Seen, len(read.Captures))
	}
	c := read.Captures[0]
	if c.Body != "hello CTF" || c.Path != "/submit" || c.Query["q"][0] != "one" || c.Headers["X-Lab"][0] != "active" {
		t.Fatalf("capture: %+v", c)
	}
}
func TestRuleAndConfig(t *testing.T) {
	a := newTestApp()
	s := makeSession(t, a, creationRequest{})
	config := updateRequest{Response: responseSpec{Status: 404, Body: "not found"}, Rules: []rule{{Match: matchSpec{Method: "GET", PathPrefix: "/admin", QueryKey: "role", QueryValue: "admin", HeaderKey: "X-Ctf", HeaderValue: "yes"}, Response: responseSpec{Status: 302, Headers: map[string]string{"Location": "/done", "X-Lab": "yes"}}}}}
	rr := runAdmin(t, a, "PUT", "/api/endpoints/"+s.Token, config)
	if rr.Code != 200 {
		t.Fatalf("PUT: %d %s", rr.Code, rr.Body.String())
	}
	matched := httptest.NewRequest("GET", "/c/"+s.Token+"/admin?role=admin", nil)
	matched.Header.Set("X-Ctf", "yes")
	result := httptest.NewRecorder()
	a.publicMux().ServeHTTP(result, matched)
	if result.Code != 302 || result.Header().Get("Location") != "/done" {
		t.Fatalf("match result %d %s", result.Code, result.Header().Get("Location"))
	}
	miss := httptest.NewRequest("GET", "/c/"+s.Token+"/admin?role=user", nil)
	result2 := httptest.NewRecorder()
	a.publicMux().ServeHTTP(result2, miss)
	if result2.Code != 404 || result2.Body.String() != "not found" {
		t.Fatalf("fallback result %d %s", result2.Code, result2.Body.String())
	}
}
func TestRequestQuotaAndDelete(t *testing.T) {
	a := newTestApp()
	s := makeSession(t, a, creationRequest{MaxRequests: 1})
	for i, expected := range []int{200, 410} {
		rr := httptest.NewRecorder()
		a.publicMux().ServeHTTP(rr, httptest.NewRequest("GET", "/c/"+s.Token, nil))
		if rr.Code != expected {
			t.Fatalf("iteration %d code %d expected %d", i, rr.Code, expected)
		}
	}
	if result := runAdmin(t, a, "DELETE", "/api/endpoints/"+s.Token, nil); result.Code != 200 {
		t.Fatalf("delete: %d", result.Code)
	}
	rr := httptest.NewRecorder()
	a.publicMux().ServeHTTP(rr, httptest.NewRequest("GET", "/c/"+s.Token, nil))
	if rr.Code != 410 {
		t.Fatalf("deleted endpoint: %d", rr.Code)
	}
}
func TestExpiration(t *testing.T) {
	a := newTestApp()
	now := time.Date(2026, 11, 8, 19, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	s := makeSession(t, a, creationRequest{TTLMinutes: 1})
	now = now.Add(61 * time.Second)
	rr := httptest.NewRecorder()
	a.publicMux().ServeHTTP(rr, httptest.NewRequest("GET", "/c/"+s.Token, nil))
	if rr.Code != 410 {
		t.Fatalf("expired got %d", rr.Code)
	}
	if a.usedBytes != 0 {
		t.Fatalf("retained bytes %d", a.usedBytes)
	}
}
func TestBodyTooLarge(t *testing.T) {
	a := newTestApp()
	s := makeSession(t, a, creationRequest{BodyLimitKiB: 1})
	rr := httptest.NewRecorder()
	a.publicMux().ServeHTTP(rr, httptest.NewRequest("POST", "/c/"+s.Token, strings.NewReader(strings.Repeat("A", 1500))))
	if rr.Code != 413 {
		t.Fatalf("oversize got %d", rr.Code)
	}
	get := runAdmin(t, a, "GET", "/api/endpoints/"+s.Token, nil)
	var out session
	_ = json.Unmarshal(get.Body.Bytes(), &out)
	if len(out.Captures) != 1 || !out.Captures[0].Truncated || out.Captures[0].BodyBytes != 1024 {
		t.Fatalf("capture: %+v", out.Captures)
	}
}
func TestAuthenticationAndHeaderValidation(t *testing.T) {
	a := newTestApp()
	req := httptest.NewRequest("GET", "/api/endpoints", nil)
	req.Host = "127.0.0.1:8766"
	rr := httptest.NewRecorder()
	a.adminMux().ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("admin should require token: %d", rr.Code)
	}
	req = httptest.NewRequest("GET", "/api/endpoints", nil)
	req.Host = "127.0.0.1:8766"
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Origin", "https://evil.example")
	rr = httptest.NewRecorder()
	a.adminMux().ServeHTTP(rr, req)
	if rr.Code != 403 {
		t.Fatalf("cross origin should fail: %d", rr.Code)
	}
	s := makeSession(t, a, creationRequest{})
	invalid := updateRequest{Response: responseSpec{Status: 200, Headers: map[string]string{"Connection": "close"}}}
	if res := runAdmin(t, a, "PUT", "/api/endpoints/"+s.Token, invalid); res.Code != 400 {
		t.Fatalf("unsafe header allowed: %d", res.Code)
	}
}
func TestGlobalMemoryBudget(t *testing.T) {
	a := newTestApp()
	now := time.Date(2026, 11, 8, 20, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	s := makeSession(t, a, creationRequest{MaxRequests: 100, BodyLimitKiB: 256})
	for i := 0; i < 60; i++ {
		if i > 0 && i%maxRequestsPerSecond == 0 {
			now = now.Add(time.Second)
		}
		rr := httptest.NewRecorder()
		a.publicMux().ServeHTTP(rr, httptest.NewRequest("POST", "/c/"+s.Token, strings.NewReader(strings.Repeat("A", 200*1024))))
		if rr.Code != 200 {
			t.Fatalf("attempt %d: %d", i, rr.Code)
		}
	}
	if a.usedBytes > maxGlobalBytes {
		t.Fatalf("memory budget exceeded: %d", a.usedBytes)
	}
}
func TestAdminOnlyLoopback(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8766", "192.0.2.1:8766", ":8766"} {
		if isLoopbackAddr(addr) {
			t.Fatalf("nonloopback allowed: %s", addr)
		}
	}
	for _, addr := range []string{"127.0.0.1:8766", "[::1]:8766"} {
		if !isLoopbackAddr(addr) {
			t.Fatalf("loopback disallowed: %s", addr)
		}
	}
}

func TestRateLimitPerEndpoint(t *testing.T) {
	a := newTestApp()
	now := time.Date(2026, 11, 8, 20, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	s := makeSession(t, a, creationRequest{MaxRequests: 100})
	url := "/c/" + s.Token
	for i := 0; i < maxRequestsPerSecond; i++ {
		rr := httptest.NewRecorder()
		a.publicMux().ServeHTTP(rr, httptest.NewRequest("GET", url, nil))
		if rr.Code != 200 {
			t.Fatalf("allowed request %d: %d", i, rr.Code)
		}
	}
	blocked := httptest.NewRecorder()
	a.publicMux().ServeHTTP(blocked, httptest.NewRequest("GET", url, nil))
	if blocked.Code != 429 || blocked.Header().Get("Retry-After") != "1" {
		t.Fatalf("burst: %d, retry-after %s", blocked.Code, blocked.Header().Get("Retry-After"))
	}
	now = now.Add(time.Second)
	allowed := httptest.NewRecorder()
	a.publicMux().ServeHTTP(allowed, httptest.NewRequest("GET", url, nil))
	if allowed.Code != 200 {
		t.Fatalf("next window: %d", allowed.Code)
	}
	result := runAdmin(t, a, "GET", "/api/endpoints/"+s.Token, nil)
	var final session
	if err := json.Unmarshal(result.Body.Bytes(), &final); err != nil {
		t.Fatal(err)
	}
	if final.Seen != maxRequestsPerSecond+1 {
		t.Fatalf("quota consumed by 429: %d", final.Seen)
	}
}

func TestCaptureConcurrencyLimit(t *testing.T) {
	a := newTestApp()
	for i := 0; i < maxConcurrentCaptures; i++ {
		a.captureSlots <- struct{}{}
	}
	rr := httptest.NewRecorder()
	a.publicMux().ServeHTTP(rr, httptest.NewRequest("GET", "/c/nope", nil))
	if rr.Code != 503 || rr.Header().Get("Retry-After") != "1" {
		t.Fatalf("excess concurrency: %d", rr.Code)
	}
	<-a.captureSlots
	recovered := httptest.NewRecorder()
	a.publicMux().ServeHTTP(recovered, httptest.NewRequest("GET", "/c/nope", nil))
	if recovered.Code != 404 {
		t.Fatalf("recovery: %d", recovered.Code)
	}
}

func TestAdminHostAndBearerRequired(t *testing.T) {
	a := newTestApp()
	cases := []struct {
		host, auth string
		want       int
	}{
		{"evil.example:8766", "Bearer test-admin-token", 403},
		{"127.0.0.1:8766", "test-admin-token", 401},
		{"127.0.0.1:8766", "Bearer test-admin-token", 200},
		{"localhost:8766", "Bearer test-admin-token", 200},
	}
	for _, tc := range cases {
		req := httptest.NewRequest("GET", "/api/endpoints", nil)
		req.Host = tc.host
		req.Header.Set("Authorization", tc.auth)
		rr := httptest.NewRecorder()
		a.adminMux().ServeHTTP(rr, req)
		if rr.Code != tc.want {
			t.Fatalf("host %s auth %s: got %d, want %d", tc.host, tc.auth, rr.Code, tc.want)
		}
	}
}

func TestAdminJSONLimitAndObjectShape(t *testing.T) {
	a := newTestApp()
	for _, payload := range []string{
		`null`, `[]`, `{}` + strings.Repeat(" ", maxAdminJSONBytes),
		`{"name":"x"} {"name":"y"}`,
		`{"unknown":"x"}`,
	} {
		req := httptest.NewRequest("POST", "/api/endpoints", strings.NewReader(payload))
		req.Host = "127.0.0.1:8766"
		req.Header.Set("Authorization", "Bearer test-admin-token")
		rr := httptest.NewRecorder()
		a.adminMux().ServeHTTP(rr, req)
		if rr.Code != 400 {
			t.Fatalf("payload (%d bytes): got %d", len(payload), rr.Code)
		}
	}
}

func TestResponseHeaderRejectsControls(t *testing.T) {
	a := newTestApp()
	s := makeSession(t, a, creationRequest{})
	for _, value := range []string{"hello\nworld", "\x01abc", "abc\x7f"} {
		bad := updateRequest{Response: responseSpec{Status: 200, Headers: map[string]string{"X-Test": value}}}
		rr := runAdmin(t, a, "PUT", "/api/endpoints/"+s.Token, bad)
		if rr.Code != 400 {
			t.Fatalf("accepted header value %q", value)
		}
	}
}

func TestStoredTokenRequiresPrivateRegularFile(t *testing.T) {
	t.Setenv("HOOKLOOM_ADMIN_TOKEN", "")
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	token, tokenPath, err := loadAdminToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(token) < 32 {
		t.Fatal("generated token too short")
	}
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if err := os.Chmod(tokenPath, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadAdminToken(); err == nil {
		t.Fatal("world-readable token was accepted")
	}
	if err := os.Remove(tokenPath); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(configHome, "other-file")
	if err := os.WriteFile(target, []byte(token), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, tokenPath); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadAdminToken(); err == nil {
		t.Fatal("token symlink was accepted")
	}
}

func TestQueryValuesCountTowardBudget(t *testing.T) {
	a := newTestApp()
	s := makeSession(t, a, creationRequest{})
	query := strings.Repeat("q", 9000)
	rr := httptest.NewRecorder()
	a.publicMux().ServeHTTP(rr, httptest.NewRequest("GET", "/c/"+s.Token+"/?k="+query, nil))
	if rr.Code != 200 {
		t.Fatalf("request: %d", rr.Code)
	}
	if a.usedBytes < len(query) {
		t.Fatalf("query missing from accounting: %d", a.usedBytes)
	}
}

func TestCaptureBaseURLValidation(t *testing.T) {
	for _, good := range []string{"http://127.0.0.1:8765", "https://hooks.example.test", "https://[::1]:8765"} {
		if !validCaptureBase(good) {
			t.Fatalf("rejected URL: %s", good)
		}
	}
	for _, bad := range []string{"http://", "https://bad.test/path", "https://bad.test?query=1", "http://person:pass@bad.test", "ftp://bad.test", "http://bad.test/#fragment", "http://bad.test:invalid"} {
		if validCaptureBase(bad) {
			t.Fatalf("accepted URL: %s", bad)
		}
	}
}

// TestExactPathMatchesOnlyThatPath verifies that pathExact is a case-sensitive,
// full-path comparison, while legacy pathPrefix retains its original behavior.
func TestExactPathMatchesOnlyThatPath(t *testing.T) {
	a := newTestApp()
	s := makeSession(t, a, creationRequest{})
	configuration := updateRequest{
		Response: responseSpec{Status: 200, Body: "fallback"},
		Rules: []rule{
			{Match: matchSpec{Method: "GET", PathExact: "/callback"}, Response: responseSpec{Status: 302, Headers: map[string]string{"Location": "/done"}}},
			{Match: matchSpec{Method: "GET", PathPrefix: "/api"}, Response: responseSpec{Status: 201, Body: "prefix"}},
		},
	}
	if r := runAdmin(t, a, "PUT", "/api/endpoints/"+s.Token, configuration); r.Code != 200 {
		t.Fatalf("update rules: %d: %s", r.Code, r.Body.String())
	}
	cases := []struct {
		method, path    string
		status, matched int
	}{
		{"GET", "/callback", 302, 0},
		{"GET", "/callback/test", 200, -1},
		{"GET", "/callbackxyz", 200, -1},
		{"GET", "/Callback", 200, -1},
		{"POST", "/callback", 200, -1},
		{"GET", "/api", 201, 1},
		{"GET", "/api/test", 201, 1},
		{"GET", "/apixyz", 201, 1},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			a.publicMux().ServeHTTP(rec, httptest.NewRequest(tc.method, "/c/"+s.Token+tc.path, nil))
			if rec.Code != tc.status {
				t.Fatalf("status=%d, want %d", rec.Code, tc.status)
			}
			if tc.status == 302 && rec.Header().Get("Location") != "/done" {
				t.Fatalf("Location=%q", rec.Header().Get("Location"))
			}
		})
	}
	get := runAdmin(t, a, "GET", "/api/endpoints/"+s.Token, nil)
	if get.Code != http.StatusOK {
		t.Fatalf("get: %d", get.Code)
	}
	var saved session
	if err := json.Unmarshal(get.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved.Rules) != 2 || saved.Rules[0].Match.PathExact != "/callback" || saved.Rules[1].Match.PathPrefix != "/api" {
		t.Fatalf("persisted rules changed: %+v", saved.Rules)
	}
	if len(saved.Captures) != len(cases) {
		t.Fatalf("captured %d, want %d", len(saved.Captures), len(cases))
	}
	for i, tc := range cases {
		if saved.Captures[i].MatchedRule != tc.matched {
			t.Fatalf("%s %s: matchedRule=%d, want %d", tc.method, tc.path, saved.Captures[i].MatchedRule, tc.matched)
		}
	}
}

func TestValidateExactPathRule(t *testing.T) {
	cases := []struct {
		name  string
		match matchSpec
		valid bool
	}{
		{"exact only", matchSpec{PathExact: "/callback"}, true},
		{"prefix only", matchSpec{PathPrefix: "/callback"}, true},
		{"other condition only", matchSpec{Method: "GET"}, true},
		{"empty match is allowed via API", matchSpec{}, true},
		{"exact missing slash", matchSpec{PathExact: "callback"}, false},
		{"exact above 200 bytes", matchSpec{PathExact: "/" + strings.Repeat("x", 200)}, false},
		{"both modes", matchSpec{PathExact: "/callback", PathPrefix: "/callback"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			s := makeSession(t, a, creationRequest{})
			r := runAdmin(t, a, "PUT", "/api/endpoints/"+s.Token, updateRequest{Response: defaultResponse(), Rules: []rule{{Match: tc.match, Response: defaultResponse()}}})
			if tc.valid && r.Code != 200 {
				t.Fatalf("unexpected rejection: %d %s", r.Code, r.Body.String())
			}
			if !tc.valid && r.Code < 400 {
				t.Fatalf("invalid rule accepted: %s", r.Body.String())
			}
		})
	}
}

func TestRuleConflictAnalyzerAsset(t *testing.T) {
	a := newTestApp()
	req := httptest.NewRequest(http.MethodGet, "/rule_conflicts.js", nil)
	req.Host = "127.0.0.1:8766"
	rr := httptest.NewRecorder()
	a.adminMux().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "findShadowedRules") {
		t.Fatalf("rule analyzer unavailable: %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/javascript; charset=utf-8" {
		t.Fatalf("wrong content type: %q", got)
	}
	for _, name := range []string{"Content-Security-Policy", "Cache-Control", "X-Content-Type-Options"} {
		if rr.Header().Get(name) == "" {
			t.Fatalf("security header missing: %s", name)
		}
	}
	req.Host = "attacker.example:8766"
	bad := httptest.NewRecorder()
	a.adminMux().ServeHTTP(bad, req)
	if bad.Code != http.StatusForbidden {
		t.Fatalf("unexpected Host accepted: %d", bad.Code)
	}
}

// Unknown paths and known tokens share one ingress budget. Rejections must
// not consume endpoint request quotas or affect the private management API.
func TestGlobalIngressLimitCoversUnknownAndKnownPaths(t *testing.T) {
	a := newTestApp()
	now := time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC)
	a.now = func() time.Time { return now }
	s := makeSession(t, a, creationRequest{MaxRequests: 100})
	handler := a.publicMux()

	for i := 0; i < maxGlobalRequestsPerSecond-1; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unknown-"+strings.Repeat("x", 8), nil))
		if rec.Code != http.StatusNotFound {
			t.Fatalf("unknown request %d: status %d, want 404", i, rec.Code)
		}
	}
	allowed := httptest.NewRecorder()
	handler.ServeHTTP(allowed, httptest.NewRequest(http.MethodGet, "/c/"+s.Token, nil))
	if allowed.Code != http.StatusOK {
		t.Fatalf("last allowed request: status %d", allowed.Code)
	}
	blocked := httptest.NewRecorder()
	handler.ServeHTTP(blocked, httptest.NewRequest(http.MethodPost, "/c/"+s.Token, nil))
	if blocked.Code != http.StatusTooManyRequests || blocked.Header().Get("Retry-After") != "1" {
		t.Fatalf("ingress overflow: status %d, Retry-After %q", blocked.Code, blocked.Header().Get("Retry-After"))
	}
	if got := blocked.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("blocked Referrer-Policy = %q", got)
	}
	if got := blocked.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("blocked Cache-Control = %q", got)
	}
	inspect := runAdmin(t, a, http.MethodGet, "/api/endpoints/"+s.Token, nil)
	if inspect.Code != http.StatusOK {
		t.Fatalf("private admin rejected by ingest limiter: %d", inspect.Code)
	}
	var stored session
	if err := json.Unmarshal(inspect.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Seen != 1 {
		t.Fatalf("globally rejected request consumed endpoint quota: seen=%d", stored.Seen)
	}
	now = now.Add(time.Second)
	recovered := httptest.NewRecorder()
	handler.ServeHTTP(recovered, httptest.NewRequest(http.MethodGet, "/c/"+s.Token, nil))
	if recovered.Code != http.StatusOK {
		t.Fatalf("new rate window: status %d", recovered.Code)
	}
}

func TestOversizedCaptureRequestTargetRejected(t *testing.T) {
	a := newTestApp()
	s := makeSession(t, a, creationRequest{})
	handler := a.publicMux()
	longTarget := "/c/" + s.Token + "?q=" + strings.Repeat("x", maxPublicRequestTargetBytes)
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, longTarget, nil))
	if r.Code != http.StatusRequestURITooLong {
		t.Fatalf("long URL: status %d, want 414", r.Code)
	}
	if r.Header().Get("Referrer-Policy") != "no-referrer" || r.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("long URL response missing privacy headers")
	}
	inspection := runAdmin(t, a, http.MethodGet, "/api/endpoints/"+s.Token, nil)
	var stored session
	if err := json.Unmarshal(inspection.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.Seen != 0 {
		t.Fatalf("rejected URL consumed capture quota: %d", stored.Seen)
	}
	short := httptest.NewRecorder()
	handler.ServeHTTP(short, httptest.NewRequest(http.MethodGet, "/c/"+s.Token, nil))
	if short.Code != http.StatusOK {
		t.Fatalf("normal request not admitted: %d", short.Code)
	}
}

func TestGlobalIngressLimiterConcurrent(t *testing.T) {
	a := newTestApp()
	fixed := time.Date(2026, 11, 9, 20, 0, 0, 0, time.UTC)
	const attempts = 100
	var wg sync.WaitGroup
	results := make(chan bool, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- a.allowGlobalRate(fixed)
		}()
	}
	wg.Wait()
	close(results)
	admitted := 0
	for allowed := range results {
		if allowed {
			admitted++
		}
	}
	if admitted != maxGlobalRequestsPerSecond {
		t.Fatalf("concurrent admissions = %d, want %d", admitted, maxGlobalRequestsPerSecond)
	}
}

// Security regression: the public capture listener must not disclose one-time
// URL secrets through browser Referrer headers, even when an endpoint is absent.
func TestCaptureReferrerPolicy(t *testing.T) {
	a := newTestApp()
	s := makeSession(t, a, creationRequest{})
	for _, path := range []string{"/c/" + s.Token, "/c/" + s.Token + "/redirect", "/unmatched"} {
		rec := httptest.NewRecorder()
		a.publicMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
			t.Errorf("%s: Referrer-Policy = %q", path, got)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q", path, got)
		}
	}
}

// A programmable response may specify its own headers, but cannot downgrade
// the protections on token-bearing capture URLs, including redirects.
func TestCaptureReferrerPolicyCannotBeOverridden(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{name: "success", status: http.StatusOK},
		{name: "redirect", status: http.StatusFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newTestApp()
			spec := responseSpec{
				Status: tc.status,
				Headers: map[string]string{
					"Referrer-Policy": "unsafe-url",
					"Cache-Control":   "public, max-age=3600",
					"Location":        "https://example.org/",
				},
				Body: "test",
			}
			s := makeSession(t, a, creationRequest{Response: &spec})
			rec := httptest.NewRecorder()
			a.publicMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/c/"+s.Token, nil))
			if rec.Code != tc.status {
				t.Fatalf("response status = %d, want %d", rec.Code, tc.status)
			}
			if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
				t.Errorf("Referrer-Policy = %q, want no-referrer", got)
			}
			if got := rec.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
		})
	}
}

func TestIngestBindingRequiresExplicitOptIn(t *testing.T) {
	cases := []struct {
		addr    string
		optIn   bool
		allowed bool
	}{
		{"127.0.0.1:8765", false, true},
		{"[::1]:8765", false, true},
		{"0.0.0.0:8765", false, false},
		{":8765", false, false},
		{"192.0.2.10:8765", false, false},
		{"localhost:8765", false, false}, // DNS may change; use a numeric loopback address.
		{"0.0.0.0:8765", true, true},
		{":8765", true, true},
		{"192.0.2.10:8765", true, true},
		{"bad-address", true, false},
	}
	for _, tc := range cases {
		err := validateIngestBind(tc.addr, tc.optIn)
		if (err == nil) != tc.allowed {
			t.Errorf("addr=%q optIn=%v: allowed=%v, want %v", tc.addr, tc.optIn, err == nil, tc.allowed)
		}
	}
}
