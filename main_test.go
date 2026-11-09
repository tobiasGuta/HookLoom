package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	rr := httptest.NewRecorder()
	a.adminMux().ServeHTTP(rr, req)
	if rr.Code != 401 {
		t.Fatalf("admin should require token: %d", rr.Code)
	}
	req = httptest.NewRequest("GET", "/api/endpoints", nil)
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
	s := makeSession(t, a, creationRequest{MaxRequests: 100, BodyLimitKiB: 256})
	for i := 0; i < 60; i++ {
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
