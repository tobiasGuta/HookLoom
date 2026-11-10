// HookLoom is a small ephemeral HTTP request catcher for authorized CTF exercises.
// It deliberately has separate public-capture and loopback-only admin listeners.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"
)

//go:embed web/*
var uiFiles embed.FS

const (
	defaultTTL            = 30
	defaultMaxRequests    = 50
	defaultBodyLimitKiB   = 64
	maxSessions           = 8
	maxRules              = 12
	maxGlobalBytes        = 8 << 20
	maxAdminJSONBytes     = 128 << 10
	maxResponseBytes      = 64 << 10
	maxConcurrentCaptures = 24
	maxRequestsPerSecond  = 20
)

var headerKeyRE = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")
var tokenRE = regexp.MustCompile(`^[A-Za-z0-9_-]{32}$`)

type responseSpec struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	DelayMs int               `json:"delayMs"`
}

type matchSpec struct {
	Method       string `json:"method,omitempty"`
	PathPrefix   string `json:"pathPrefix,omitempty"`
	PathExact    string `json:"pathExact,omitempty"`
	QueryKey     string `json:"queryKey,omitempty"`
	QueryValue   string `json:"queryValue,omitempty"`
	HeaderKey    string `json:"headerKey,omitempty"`
	HeaderValue  string `json:"headerValue,omitempty"`
	BodyContains string `json:"bodyContains,omitempty"`
}

type rule struct {
	Match    matchSpec    `json:"match"`
	Response responseSpec `json:"response"`
}

type creationRequest struct {
	Name         string        `json:"name"`
	TTLMinutes   int           `json:"ttlMinutes"`
	MaxRequests  int           `json:"maxRequests"`
	BodyLimitKiB int           `json:"bodyLimitKiB"`
	Response     *responseSpec `json:"response,omitempty"`
	Rules        []rule        `json:"rules,omitempty"`
}

type updateRequest struct {
	Response responseSpec `json:"response"`
	Rules    []rule       `json:"rules"`
}

type capture struct {
	ID           string              `json:"id"`
	At           time.Time           `json:"at"`
	Method       string              `json:"method"`
	Path         string              `json:"path"`
	Query        map[string][]string `json:"query"`
	Headers      map[string][]string `json:"headers"`
	Body         string              `json:"body"`
	BodyEncoding string              `json:"bodyEncoding"`
	BodyBytes    int                 `json:"bodyBytes"`
	Truncated    bool                `json:"truncated"`
	MatchedRule  int                 `json:"matchedRule"`
	cost         int
}

type session struct {
	Token        string       `json:"token"`
	Name         string       `json:"name"`
	URL          string       `json:"url"`
	CreatedAt    time.Time    `json:"createdAt"`
	ExpiresAt    time.Time    `json:"expiresAt"`
	MaxRequests  int          `json:"maxRequests"`
	BodyLimitKiB int          `json:"bodyLimitKiB"`
	Seen         int          `json:"seen"`
	Response     responseSpec `json:"response"`
	Rules        []rule       `json:"rules"`
	Captures     []capture    `json:"captures"`
	rateWindow   time.Time
	rateHits     int
}

type app struct {
	mu           sync.Mutex
	sessions     map[string]*session
	tombstones   map[string]time.Time
	baseURL      string
	adminToken   string
	usedBytes    int
	captureSlots chan struct{}
	now          func() time.Time
}

func newApp(baseURL, adminToken string) *app {
	return &app{sessions: make(map[string]*session), tombstones: make(map[string]time.Time), baseURL: strings.TrimRight(baseURL, "/"), adminToken: adminToken, captureSlots: make(chan struct{}, maxConcurrentCaptures), now: time.Now}
}

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func defaultResponse() responseSpec {
	return responseSpec{Status: 200, Headers: map[string]string{"Content-Type": "application/json; charset=utf-8"}, Body: `{"ok":true}`}
}

func validateResponse(r responseSpec) error {
	if r.Status < 200 || r.Status > 599 {
		return errors.New("response status must be 200–599")
	}
	if r.DelayMs < 0 || r.DelayMs > 2000 {
		return errors.New("delayMs must be 0–2000")
	}
	if len(r.Body) > maxResponseBytes {
		return errors.New("response body must be <=64 KiB")
	}
	if len(r.Headers) > 20 {
		return errors.New("at most 20 response headers are allowed")
	}
	prohibited := map[string]bool{"connection": true, "transfer-encoding": true, "content-length": true, "keep-alive": true, "proxy-authenticate": true, "proxy-authorization": true, "te": true, "trailer": true, "upgrade": true, "host": true}
	for k, v := range r.Headers {
		if !headerKeyRE.MatchString(k) || prohibited[strings.ToLower(k)] {
			return fmt.Errorf("unsupported response header name: %q", k)
		}
		if len(v) > 2048 {
			return fmt.Errorf("invalid response header value for %q", k)
		}
		for j := 0; j < len(v); j++ {
			// Forbid CRLF/header injection and all HTTP-disallowed control bytes.
			if v[j] == 127 || (v[j] < 32 && v[j] != '\t') {
				return fmt.Errorf("invalid response header value for %q", k)
			}
		}
	}
	return nil
}

func validateRules(rules []rule) error {
	if len(rules) > maxRules {
		return fmt.Errorf("at most %d rules allowed", maxRules)
	}
	for i, r := range rules {
		if err := validateResponse(r.Response); err != nil {
			return fmt.Errorf("rule %d: %w", i+1, err)
		}
		m := r.Match
		if len(m.PathPrefix) > 200 || len(m.PathExact) > 200 || len(m.BodyContains) > 512 || len(m.QueryKey) > 128 || len(m.QueryValue) > 512 || len(m.HeaderKey) > 128 || len(m.HeaderValue) > 512 {
			return fmt.Errorf("rule %d: match value too long", i+1)
		}
		if m.Method != "" && (!headerKeyRE.MatchString(m.Method) || strings.ToUpper(m.Method) != m.Method) {
			return fmt.Errorf("rule %d: invalid method", i+1)
		}
		if m.PathPrefix != "" && !strings.HasPrefix(m.PathPrefix, "/") {
			return fmt.Errorf("rule %d: pathPrefix must start with /", i+1)
		}
		if m.PathExact != "" && !strings.HasPrefix(m.PathExact, "/") {
			return fmt.Errorf("rule %d: pathExact must start with /", i+1)
		}
		if m.PathPrefix != "" && m.PathExact != "" {
			return fmt.Errorf("rule %d: use either pathPrefix or pathExact, not both", i+1)
		}
		if m.HeaderKey != "" && !headerKeyRE.MatchString(m.HeaderKey) {
			return fmt.Errorf("rule %d: invalid header key", i+1)
		}
	}
	return nil
}

func matchRule(m matchSpec, r *http.Request, relPath string, body []byte) bool {
	if m.Method != "" && r.Method != m.Method {
		return false
	}
	if m.PathPrefix != "" && !strings.HasPrefix(relPath, m.PathPrefix) {
		return false
	}
	if m.PathExact != "" && relPath != m.PathExact {
		return false
	}
	if m.QueryKey != "" {
		vals, ok := r.URL.Query()[m.QueryKey]
		if !ok {
			return false
		}
		if m.QueryValue != "" {
			found := false
			for _, v := range vals {
				if v == m.QueryValue {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	if m.HeaderKey != "" {
		vals, ok := r.Header[http.CanonicalHeaderKey(m.HeaderKey)]
		if !ok {
			return false
		}
		if m.HeaderValue != "" {
			found := false
			for _, v := range vals {
				if v == m.HeaderValue {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
	}
	if m.BodyContains != "" && !strings.Contains(string(body), m.BodyContains) {
		return false
	}
	return true
}

func (a *app) cleanup(now time.Time) {
	for token, s := range a.sessions {
		if !now.Before(s.ExpiresAt) {
			a.deleteLocked(token, now)
		}
	}
	for token, until := range a.tombstones {
		if !now.Before(until) {
			delete(a.tombstones, token)
		}
	}
}
func (a *app) deleteLocked(token string, at time.Time) {
	if s, ok := a.sessions[token]; ok {
		for _, c := range s.Captures {
			a.usedBytes -= c.cost
		}
		delete(a.sessions, token)
		a.tombstones[token] = at.Add(1 * time.Hour)
	}
}

func (a *app) insertLocked(s *session, c capture) {
	s.Captures = append(s.Captures, c)
	a.usedBytes += c.cost
	// Bound total retained content, evicting oldest captures across workspaces.
	for a.usedBytes > maxGlobalBytes {
		var oldest *session
		var at time.Time
		for _, candidate := range a.sessions {
			if len(candidate.Captures) == 0 {
				continue
			}
			if oldest == nil || candidate.Captures[0].At.Before(at) {
				oldest = candidate
				at = candidate.Captures[0].At
			}
		}
		if oldest == nil {
			break
		}
		a.usedBytes -= oldest.Captures[0].cost
		oldest.Captures = oldest.Captures[1:]
	}
}

func (a *app) publicMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/c/", a.receive)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Capture URLs are bearer capabilities. Never leak their paths as browser Referers.
		// This applies to redirects, custom HTML, invalid paths and rate-limit errors.
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		// Shed excess simultaneous requests rather than exhausting small VM resources.
		select {
		case a.captureSlots <- struct{}{}:
			defer func() { <-a.captureSlots }()
			mux.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Capture server busy", http.StatusServiceUnavailable)
		}
	})
}

func (s *session) allowRate(now time.Time) bool {
	if s.rateWindow.IsZero() || now.Before(s.rateWindow) || now.Sub(s.rateWindow) >= time.Second {
		s.rateWindow = now
		s.rateHits = 0
	}
	if s.rateHits >= maxRequestsPerSecond {
		return false
	}
	s.rateHits++
	return true
}

func (a *app) receive(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	path := strings.TrimPrefix(r.URL.Path, "/c/")
	token, rest, found := strings.Cut(path, "/")
	if !found {
		rest = ""
	} else {
		rest = "/" + rest
	}
	if !tokenRE.MatchString(token) {
		http.NotFound(w, r)
		return
	}
	a.mu.Lock()
	now := a.now()
	a.cleanup(now)
	s, ok := a.sessions[token]
	if !ok {
		_, expired := a.tombstones[token]
		a.mu.Unlock()
		if expired {
			http.Error(w, "Endpoint expired", http.StatusGone)
		} else {
			http.NotFound(w, r)
		}
		return
	}
	if s.Seen >= s.MaxRequests {
		a.mu.Unlock()
		http.Error(w, "Request limit reached", http.StatusGone)
		return
	}
	if !s.allowRate(now) {
		a.mu.Unlock()
		w.Header().Set("Retry-After", "1")
		http.Error(w, "Endpoint rate limit reached", http.StatusTooManyRequests)
		return
	}
	maxBody := s.BodyLimitKiB * 1024
	a.mu.Unlock()

	data, err := io.ReadAll(io.LimitReader(r.Body, int64(maxBody+1)))
	if err != nil {
		http.Error(w, "Could not read request", http.StatusBadRequest)
		return
	}
	oversized := len(data) > maxBody
	if oversized {
		data = data[:maxBody]
	}
	encoded := "utf8"
	body := string(data)
	if !utf8.Valid(data) {
		body = base64.StdEncoding.EncodeToString(data)
		encoded = "base64"
	}
	id, err := randomToken(9)
	if err != nil {
		http.Error(w, "Internal error", 500)
		return
	}
	h := r.Header.Clone()
	event := capture{ID: id, At: a.now(), Method: r.Method, Path: rest, Query: r.URL.Query(), Headers: h, Body: body, BodyEncoding: encoded, BodyBytes: len(data), Truncated: oversized, MatchedRule: -1}
	// Account for retained query strings and header metadata as well as body bytes.
	// This is a bounded accounting budget, not an exact Go heap measurement.
	cost := len(body) + len(rest) + len(r.Method) + len(id) + 1536
	for k, vs := range event.Query {
		cost += len(k) + 64
		for _, v := range vs {
			cost += len(v) + 32
		}
	}
	for k, vs := range h {
		cost += len(k)
		for _, v := range vs {
			cost += len(v)
		}
	}
	event.cost = cost

	a.mu.Lock()
	now = a.now()
	a.cleanup(now)
	s, ok = a.sessions[token]
	if !ok || s.Seen >= s.MaxRequests {
		a.mu.Unlock()
		http.Error(w, "Endpoint no longer available", http.StatusGone)
		return
	}
	spec := s.Response
	for i, rule := range s.Rules {
		if matchRule(rule.Match, r, rest, data) {
			spec = rule.Response
			event.MatchedRule = i
			break
		}
	}
	s.Seen++
	a.insertLocked(s, event)
	a.mu.Unlock()
	if oversized {
		http.Error(w, "Request body exceeds endpoint limit", http.StatusRequestEntityTooLarge)
		return
	}
	if spec.DelayMs > 0 {
		select {
		case <-time.After(time.Duration(spec.DelayMs) * time.Millisecond):
		case <-r.Context().Done():
			return
		}
	}
	for k, v := range spec.Headers {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	// Capability paths must not leak via Referer, even if a mock response
	// supplies a less restrictive Referrer-Policy header.
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(spec.Status)
	if spec.Status != 204 && spec.Status != 304 && r.Method != http.MethodHead {
		_, _ = io.WriteString(w, spec.Body)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func jsonError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
func decodeJSON(r *http.Request, out any) error {
	data, err := io.ReadAll(io.LimitReader(r.Body, maxAdminJSONBytes+1))
	if err != nil {
		return errors.New("could not read JSON payload")
	}
	if len(data) > maxAdminJSONBytes {
		return errors.New("JSON payload exceeds 128 KiB")
	}
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("expected a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return errors.New("only one JSON object is allowed")
	}
	return nil
}

func (a *app) adminMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		serveUI(w, r, "web/index.html", "text/html; charset=utf-8")
	})
	mux.HandleFunc("/app.js", func(w http.ResponseWriter, r *http.Request) {
		serveUI(w, r, "web/app.js", "application/javascript; charset=utf-8")
	})
	mux.HandleFunc("/rule_conflicts.js", func(w http.ResponseWriter, r *http.Request) {
		serveUI(w, r, "web/rule_conflicts.js", "application/javascript; charset=utf-8")
	})
	mux.HandleFunc("/style.css", func(w http.ResponseWriter, r *http.Request) {
		serveUI(w, r, "web/style.css", "text/css; charset=utf-8")
	})
	mux.Handle("/api/", a.authorize(http.HandlerFunc(a.adminAPI)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent DNS rebinding from making the dashboard available at an
		// attacker-controlled hostname that resolves to loopback.
		if !isAllowedAdminHost(r.Host) {
			http.Error(w, "Invalid dashboard Host", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func isAllowedAdminHost(hostport string) bool {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil || port == "" {
		return false
	}
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}
func serveUI(w http.ResponseWriter, r *http.Request, path, ctype string) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", 405)
		return
	}
	if path == "web/index.html" && r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	b, err := uiFiles.ReadFile(path)
	if err != nil {
		http.Error(w, "UI unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; form-action 'none'; base-uri 'none'; frame-ancestors 'none'")
	_, _ = w.Write(b)
}
func (a *app) authorize(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			jsonError(w, 401, "Admin token required")
			return
		}
		provided := strings.TrimPrefix(auth, "Bearer ")
		if len(provided) != len(a.adminToken) || subtle.ConstantTimeCompare([]byte(provided), []byte(a.adminToken)) != 1 {
			jsonError(w, 401, "Admin token required")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			// Browser sessions must be same-origin. No CORS for outside websites.
			expected := "http://" + r.Host
			if origin != expected {
				jsonError(w, 403, "Unexpected Origin")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (a *app) adminAPI(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/api/endpoints":
		switch r.Method {
		case "GET":
			a.list(w)
		case "POST":
			a.create(w, r)
		default:
			jsonError(w, 405, "Method not allowed")
		}
	case strings.HasPrefix(r.URL.Path, "/api/endpoints/"):
		rest := strings.TrimPrefix(r.URL.Path, "/api/endpoints/")
		token, tail, hasTail := strings.Cut(rest, "/")
		if !tokenRE.MatchString(token) {
			jsonError(w, 404, "Endpoint not found")
			return
		}
		if hasTail && tail == "export" && r.Method == "GET" {
			a.export(w, token)
			return
		}
		if hasTail {
			jsonError(w, 404, "Not found")
			return
		}
		switch r.Method {
		case "GET":
			a.get(w, token)
		case "PUT":
			a.update(w, r, token)
		case "DELETE":
			a.remove(w, token)
		default:
			jsonError(w, 405, "Method not allowed")
		}
	default:
		jsonError(w, 404, "Not found")
	}
}
func cloneSession(s *session) session {
	copy := *s
	copy.Rules = append([]rule{}, s.Rules...)
	copy.Captures = append([]capture{}, s.Captures...)
	return copy
}
func (a *app) list(w http.ResponseWriter) {
	a.mu.Lock()
	a.cleanup(a.now())
	out := make([]map[string]any, 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, map[string]any{"token": s.Token, "name": s.Name, "url": s.URL, "createdAt": s.CreatedAt, "expiresAt": s.ExpiresAt, "seen": s.Seen, "saved": len(s.Captures), "maxRequests": s.MaxRequests})
	}
	a.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i]["createdAt"].(time.Time).After(out[j]["createdAt"].(time.Time)) })
	writeJSON(w, 200, out)
}
func (a *app) create(w http.ResponseWriter, r *http.Request) {
	var input creationRequest
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, 400, "Invalid JSON: "+err.Error())
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		input.Name = "CTF session"
	}
	if len(input.Name) > 60 {
		jsonError(w, 400, "name must be <=60 bytes")
		return
	}
	if input.TTLMinutes == 0 {
		input.TTLMinutes = defaultTTL
	}
	if input.MaxRequests == 0 {
		input.MaxRequests = defaultMaxRequests
	}
	if input.BodyLimitKiB == 0 {
		input.BodyLimitKiB = defaultBodyLimitKiB
	}
	if input.TTLMinutes < 1 || input.TTLMinutes > 240 || input.MaxRequests < 1 || input.MaxRequests > 100 || input.BodyLimitKiB < 1 || input.BodyLimitKiB > 256 {
		jsonError(w, 400, "limits: ttlMinutes 1–240, maxRequests 1–100, bodyLimitKiB 1–256")
		return
	}
	response := defaultResponse()
	if input.Response != nil {
		response = *input.Response
	}
	if err := validateResponse(response); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if err := validateRules(input.Rules); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	token, err := randomToken(24)
	if err != nil {
		jsonError(w, 500, "Random generator error")
		return
	}
	now := a.now()
	s := &session{Token: token, Name: input.Name, URL: a.baseURL + "/c/" + token, CreatedAt: now, ExpiresAt: now.Add(time.Duration(input.TTLMinutes) * time.Minute), MaxRequests: input.MaxRequests, BodyLimitKiB: input.BodyLimitKiB, Response: response, Rules: input.Rules, Captures: []capture{}}
	a.mu.Lock()
	a.cleanup(now)
	if len(a.sessions) >= maxSessions {
		a.mu.Unlock()
		jsonError(w, 429, "Maximum active sessions reached (8)")
		return
	}
	a.sessions[token] = s
	a.mu.Unlock()
	writeJSON(w, 201, s)
}
func (a *app) get(w http.ResponseWriter, token string) {
	a.mu.Lock()
	a.cleanup(a.now())
	s, ok := a.sessions[token]
	if !ok {
		a.mu.Unlock()
		jsonError(w, 404, "Endpoint not found or expired")
		return
	}
	out := cloneSession(s)
	a.mu.Unlock()
	writeJSON(w, 200, out)
}
func (a *app) update(w http.ResponseWriter, r *http.Request, token string) {
	var input updateRequest
	if err := decodeJSON(r, &input); err != nil {
		jsonError(w, 400, "Invalid JSON: "+err.Error())
		return
	}
	if err := validateResponse(input.Response); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	if err := validateRules(input.Rules); err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	a.mu.Lock()
	a.cleanup(a.now())
	s, ok := a.sessions[token]
	if !ok {
		a.mu.Unlock()
		jsonError(w, 404, "Endpoint not found or expired")
		return
	}
	s.Response = input.Response
	s.Rules = input.Rules
	out := cloneSession(s)
	a.mu.Unlock()
	writeJSON(w, 200, out)
}
func (a *app) remove(w http.ResponseWriter, token string) {
	a.mu.Lock()
	a.cleanup(a.now())
	_, ok := a.sessions[token]
	if ok {
		a.deleteLocked(token, a.now())
	}
	a.mu.Unlock()
	if !ok {
		jsonError(w, 404, "Endpoint not found")
		return
	}
	writeJSON(w, 200, map[string]string{"result": "deleted"})
}
func (a *app) export(w http.ResponseWriter, token string) {
	a.mu.Lock()
	a.cleanup(a.now())
	s, ok := a.sessions[token]
	if !ok {
		a.mu.Unlock()
		jsonError(w, 404, "Endpoint not found")
		return
	}
	out := cloneSession(s)
	a.mu.Unlock()
	w.Header().Set("Content-Disposition", "attachment; filename=hookloom-"+token[:8]+".json")
	writeJSON(w, 200, out)
}
func validCaptureBase(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	return u.Hostname() != "" && u.User == nil && u.Path == "" && u.RawQuery == "" && u.Fragment == "" && u.Opaque == ""
}
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// require an explicit operator opt-in before listening on public or host-wide
// interfaces. HTTPS reverse proxies should forward only to a loopback listener.
func validateIngestBind(addr string, allowNonLoopback bool) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return errors.New("invalid -ingest: expected host:port")
	}
	if !isLoopbackAddr(addr) && !allowNonLoopback {
		return errors.New("refusing non-loopback capture listener; use a reverse proxy into 127.0.0.1 or explicitly set -allow-nonloopback-ingest")
	}
	return nil
}

var adminTokenRE = regexp.MustCompile(`^[A-Za-z0-9_-]{32,128}$`)

func loadAdminToken() (string, string, error) {
	if token := os.Getenv("HOOKLOOM_ADMIN_TOKEN"); token != "" {
		if !adminTokenRE.MatchString(token) {
			return "", "", errors.New("HOOKLOOM_ADMIN_TOKEN must be 32-128 base64url characters")
		}
		return token, "environment", nil
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", "", err
	}
	dir := filepath.Join(configDir, "hookloom")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	path := filepath.Join(dir, "admin.token")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return "", "", errors.New("saved admin token must be a regular file with mode 0600")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", "", err
		}
		token := strings.TrimSpace(string(b))
		if !adminTokenRE.MatchString(token) {
			return "", "", errors.New("saved admin token must be 32-128 base64url characters")
		}
		return token, path, nil
	} else if !os.IsNotExist(err) {
		return "", "", err
	}
	token, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", "", err
	}
	if _, err = f.WriteString(token + "\n"); err != nil {
		f.Close()
		return "", "", err
	}
	if err = f.Close(); err != nil {
		return "", "", err
	}
	return token, path, nil
}
func main() {
	ingest := flag.String("ingest", "127.0.0.1:8765", "HTTP capture listen address (loopback by default; use a reverse proxy for HTTPS)")
	allowNonLoopbackIngest := flag.Bool("allow-nonloopback-ingest", false, "explicitly allow binding capture traffic to non-loopback addresses (requires separate security review)")
	admin := flag.String("admin", "127.0.0.1:8766", "Private dashboard listen address (loopback only)")
	publicBase := flag.String("public-base", "http://127.0.0.1:8765", "Public capture base URL shown in dashboard")
	flag.Parse()
	if !isLoopbackAddr(*admin) {
		log.Fatal("Refusing to expose admin listener: -admin must use a loopback address")
	}
	if err := validateIngestBind(*ingest, *allowNonLoopbackIngest); err != nil {
		log.Fatal(err)
	}
	if !isLoopbackAddr(*ingest) {
		log.Print("WARNING: non-loopback capture interface enabled. Add independent firewall and edge abuse protection before accepting Internet traffic.")
	}
	if !validCaptureBase(*publicBase) {
		log.Fatal("-public-base must be a complete http(s) URL without a path, query, fragment or credentials")
	}
	token, path, err := loadAdminToken()
	if err != nil {
		log.Fatal("Admin token: ", err)
	}
	a := newApp(*publicBase, token)
	captureServer := &http.Server{Addr: *ingest, Handler: a.publicMux(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	adminServer := &http.Server{Addr: *admin, Handler: a.adminMux(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	// Bind both sockets before accepting traffic, to fail cleanly on port conflicts.
	captureLn, err := net.Listen("tcp", *ingest)
	if err != nil {
		log.Fatal("Capture listener: ", err)
	}
	adminLn, err := net.Listen("tcp", *admin)
	if err != nil {
		captureLn.Close()
		log.Fatal("Admin listener: ", err)
	}
	log.Printf("Capture listener: %s", *ingest)
	log.Printf("Private dashboard: http://%s", *admin)
	log.Printf("Admin token location: %s (never post its contents)", path)
	log.Print("No database. All sessions and captures disappear when the process stops.")
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := captureServer.Serve(captureLn); err != nil && err != http.ErrServerClosed {
			log.Print("capture listener: ", err)
			cancel()
		}
	}()
	go func() {
		defer wg.Done()
		if err := adminServer.Serve(adminLn); err != nil && err != http.ErrServerClosed {
			log.Print("admin listener: ", err)
			cancel()
		}
	}()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-ticker.C:
			a.mu.Lock()
			a.cleanup(a.now())
			a.mu.Unlock()
		}
	}
	shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_ = captureServer.Shutdown(shutdownCtx)
	_ = adminServer.Shutdown(shutdownCtx)
	wg.Wait()
	log.Print("Stopped. Ephemeral capture data discarded.")
}
