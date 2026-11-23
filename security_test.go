package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAllowedHost(t *testing.T) {
	ok := []string{"localhost:8787", "127.0.0.1:8787", "192.168.10.30:8787", "[::1]:8787", "[fe80::1%eth0]:8787", "rack-pc:8787", "rack-pc.local:8787", "monitor.empresa.com.br"}
	for _, h := range ok {
		if !allowedHost(h, []string{"monitor.empresa.com.br"}) { t.Errorf("%s deveria ser aceito", h) }
	}
	bad := []string{"evil.example.com:8787", "rebind.attacker.net", ""}
	for _, h := range bad {
		if allowedHost(h, nil) { t.Errorf("%s deveria ser recusado", h) }
	}
}

func testHub() *Hub {
	cfg := defaultConfig()
	cfg.Devices = nil
	cfg.AdminPIN = "4321"
	return &Hub{cfg: cfg}
}

func req(method, target, host, remote string, hdr map[string]string, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Host = host
	r.RemoteAddr = remote
	for k, v := range hdr { r.Header.Set(k, v) }
	return r
}

func TestGuard(t *testing.T) {
	h := testHub()
	reached := false
	g := h.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }))
	local, lan := "127.0.0.1:5000", "192.168.10.40:5000"
	json := map[string]string{"Content-Type": "application/json"}
	cases := []struct {
		name string
		r    *http.Request
		pass bool
	}{
		{"painel local", req("GET", "/api/state", "localhost:8787", local, nil, ""), true},
		{"tablet pelo IP", req("GET", "/api/state", "192.168.10.30:8787", lan, nil, ""), true},
		{"DNS rebinding", req("GET", "/api/config", "evil.example.com:8787", local, nil, ""), false},
		{"site de fora (Sec-Fetch-Site)", req("POST", "/api/control", "localhost:8787", local, map[string]string{"Sec-Fetch-Site": "cross-site", "Content-Type": "text/plain"}, "{}"), false},
		{"site de fora (Origin)", req("POST", "/api/control/arm", "localhost:8787", local, map[string]string{"Origin": "https://evil.example.com"}, ""), false},
		{"form text/plain", req("POST", "/api/control", "localhost:8787", local, map[string]string{"Content-Type": "text/plain"}, "{}"), false},
		{"img GET de outro site", req("GET", "/api/rundown/next", "localhost:8787", local, map[string]string{"Sec-Fetch-Site": "cross-site"}, ""), false},
		{"mesmo site", req("POST", "/api/control", "192.168.10.30:8787", lan, map[string]string{"Origin": "http://192.168.10.30:8787", "Sec-Fetch-Site": "same-origin", "Content-Type": "application/json"}, "{}"), true},
		{"Companion GET", req("GET", "/api/rundown/next", "192.168.10.30:8787", lan, nil, ""), true},
		{"instalador encerrando", req("POST", "/api/system/shutdown", "127.0.0.1:8787", local, json, ""), true},
		{"config local sem senha", req("PUT", "/api/config", "localhost:8787", local, json, "{}"), true},
		{"config pela rede sem senha", req("PUT", "/api/config", "192.168.10.30:8787", lan, json, "{}"), false},
		{"config pela rede com senha", req("PUT", "/api/config", "192.168.10.30:8787", lan, map[string]string{"Content-Type": "application/json", "X-Admin-Pin": "4321"}, "{}"), true},
		{"perfil pela rede sem senha", req("POST", "/api/profiles/load", "192.168.10.30:8787", lan, json, "{}"), false},
		{"apagar perfil pela rede", req("DELETE", "/api/profiles/x", "192.168.10.30:8787", lan, json, ""), false},
	}
	for _, c := range cases {
		reached = false
		w := httptest.NewRecorder()
		g.ServeHTTP(w, c.r)
		if reached != c.pass { t.Errorf("%s: passou=%v, quero %v (HTTP %d %s)", c.name, reached, c.pass, w.Code, w.Body.String()) }
	}
}

func TestAdminLockout(t *testing.T) {
	h := testHub()
	g := h.guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	hdr := map[string]string{"Content-Type": "application/json", "X-Admin-Pin": "0000"}
	for i := 0; i < maxFails; i++ {
		w := httptest.NewRecorder()
		g.ServeHTTP(w, req("PUT", "/api/config", "10.0.0.5:8787", "10.0.0.99:1", hdr, "{}"))
		if w.Code != 401 { t.Fatalf("tentativa %d: HTTP %d", i, w.Code) }
	}
	// travado: nem a senha certa entra
	w := httptest.NewRecorder()
	g.ServeHTTP(w, req("PUT", "/api/config", "10.0.0.5:8787", "10.0.0.99:1", map[string]string{"Content-Type": "application/json", "X-Admin-Pin": "4321"}, "{}"))
	if w.Code == 200 { t.Fatal("senha aceita durante o bloqueio") }
	// outro aparelho não é afetado
	w = httptest.NewRecorder()
	g.ServeHTTP(w, req("PUT", "/api/config", "10.0.0.5:8787", "10.0.0.98:1", map[string]string{"Content-Type": "application/json", "X-Admin-Pin": "4321"}, "{}"))
	if w.Code != 200 { t.Fatalf("outro aparelho bloqueado: HTTP %d", w.Code) }
}
