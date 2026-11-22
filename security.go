package main

// Proteções da API do painel.
//
//  1. Host: só atende nomes que não podem ser de um site de fora (IP, localhost,
//     nome do computador, *.local, ou o que estiver em "allowedHosts" no config).
//     Bloqueia DNS rebinding.
//  2. Sites de fora: um site aberto no navegador do rack não pode mandar comandos
//     para http://localhost:8787 (Sec-Fetch-Site / Origin / Content-Type).
//  3. Configuração alterada por outro aparelho da rede exige a SENHA DE ADMINISTRADOR.
//  4. Tentativas erradas de PIN/senha travam o aparelho por um minuto.

import (
	"crypto/subtle"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// allowedHost: o cabeçalho Host da requisição aponta para este monitor?
func allowedHost(host string, extra []string) bool {
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil { h = hh }
	h = strings.ToLower(strings.Trim(h, "[]"))
	if i := strings.IndexByte(h, '%'); i > 0 { h = h[:i] } // IPv6 com zona (fe80::1%eth0)
	if h == "" { return false }
	if net.ParseIP(h) != nil { return true }
	if h == "localhost" || strings.HasSuffix(h, ".localhost") { return true }
	if strings.HasSuffix(h, ".local") { return true } // mDNS / Bonjour
	if !strings.Contains(h, ".") { return true }       // nome curto da rede local (NetBIOS)
	me := strings.ToLower(hostName())
	if h == me { return true }
	for _, e := range extra {
		if strings.EqualFold(strings.TrimSpace(e), h) { return true }
	}
	return false
}

// crossSite: a requisição veio de uma página de outro site?
func crossSite(r *http.Request) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
		return true
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil || !strings.EqualFold(u.Host, r.Host) { return true }
	}
	return false
}

func isWrite(r *http.Request) bool {
	return r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
}

// needsAdmin: rotas que mudam a configuração do monitor.
func needsAdmin(r *http.Request) bool {
	p := r.URL.Path
	switch r.Method {
	case http.MethodPut:
		return p == "/api/config"
	case http.MethodDelete:
		return strings.HasPrefix(p, "/api/profiles/")
	case http.MethodPost:
		switch p {
		case "/api/protect", "/api/auxcfg", "/api/auxcfg/bulk", "/api/profiles", "/api/profiles/load", "/api/profiles/import":
			return true
		}
	}
	return false
}

// ---- limite de tentativas erradas (PIN de comandos e senha de administrador) ----

type failInfo struct {
	n     int
	until time.Time
}

var (
	failMu sync.Mutex
	fails  = map[string]*failInfo{}
)

const maxFails = 5
const failLock = time.Minute

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil { return r.RemoteAddr }
	return h
}

// pinLocked: este aparelho está travado por excesso de tentativas?
func pinLocked(r *http.Request) (bool, int) {
	failMu.Lock(); defer failMu.Unlock()
	f := fails[clientIP(r)]
	if f == nil || time.Now().After(f.until) { return false, 0 }
	return true, int(time.Until(f.until).Seconds()) + 1
}

func pinFailed(r *http.Request) {
	failMu.Lock(); defer failMu.Unlock()
	ip := clientIP(r)
	f := fails[ip]
	if f == nil { f = &failInfo{}; fails[ip] = f }
	f.n++
	if f.n >= maxFails { f.until = time.Now().Add(failLock); f.n = 0 }
}

func pinOK(r *http.Request) { failMu.Lock(); delete(fails, clientIP(r)); failMu.Unlock() }

// checkSecret compara em tempo constante, respeitando o limite de tentativas.
// Devolve "" se confere, senão a mensagem de erro.
func checkSecret(r *http.Request, want, got, wrongMsg string) string {
	if locked, s := pinLocked(r); locked {
		return fmt.Sprintf("Muitas tentativas erradas. Tente de novo em %d s.", s)
	}
	if subtle.ConstantTimeCompare([]byte(want), []byte(got)) != 1 {
		pinFailed(r)
		return wrongMsg
	}
	pinOK(r)
	return ""
}

// guard envolve todas as rotas.
func (h *Hub) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfgMu.RLock()
		extra := append([]string(nil), h.cfg.AllowedHosts...)
		admin := h.cfg.AdminPIN
		cfgMu.RUnlock()

		if !allowedHost(r.Host, extra) {
			name := r.Host
			if hh, _, err := net.SplitHostPort(name); err == nil { name = hh }
			httpErr(w, 403, "Endereço não autorizado: acesse pelo IP ou pelo nome do computador do rack (ou inclua \""+name+"\" em allowedHosts no config.json).")
			return
		}
		api := strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/sse"
		if api && crossSite(r) {
			httpErr(w, 403, "Pedido vindo de outro site recusado.")
			return
		}
		if api && isWrite(r) {
			if ct := r.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
				httpErr(w, 415, "Envie JSON (Content-Type: application/json).")
				return
			}
		}
		if needsAdmin(r) && !isLocal(r) {
			if admin == "" {
				httpErr(w, 403, "Para alterar a configuração por outro aparelho, defina antes uma SENHA DE ADMINISTRADOR no computador do rack (CONFIGURAÇÃO → COMANDOS FÍSICOS).")
				return
			}
			got := r.Header.Get("X-Admin-Pin")
			msg := "Digite a senha de administrador."
			if got != "" { msg = checkSecret(r, admin, got, "Senha de administrador incorreta.") }
			if msg != "" {
				if got != "" { h.LogO(originOf(r), "", "RACK MONITOR", "SENHA DE ADMINISTRADOR", "warn", msg, nil) }
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(401)
				fmt.Fprintf(w, `{"ok":false,"needAdminPin":true,"error":%q}`, msg)
				return
			}
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
