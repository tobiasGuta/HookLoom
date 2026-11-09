package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
