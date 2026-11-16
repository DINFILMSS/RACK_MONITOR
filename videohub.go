package main

// VideoHub — Ethernet Protocol TCP 9990, conexão persistente.
// A matriz envia o estado completo ao conectar e depois EMPURRA cada mudança
// de rota/label na hora: nenhuma troca passa despercebida entre consultas.

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type hubClient struct {
	h    *Hub
	dev  Device
	stop chan struct{}
	mu   sync.Mutex
	conn net.Conn
}

var hubClients sync.Map

func startVideohub(h *Hub, d Device) func() {
	c := &hubClient{h: h, dev: d, stop: make(chan struct{})}
	hubClients.Store(d.ID, c)
	go c.loop()
	return func() { close(c.stop); c.mu.Lock(); if c.conn != nil { c.conn.Close() }; c.mu.Unlock(); hubClients.Delete(d.ID) }
}

func (c *hubClient) loop() {
	backoff := time.Second
	for {
		select { case <-c.stop: return; default: }
		err := c.session()
		select { case <-c.stop: return; default: }
		c.h.SetOnline(c.dev.ID, false, errText(err))
		c.h.Update(c.dev.ID, func(s *DevState) { if s.Hub != nil { s.Hub.Ready = false } })
		select { case <-c.stop: return; case <-time.After(backoff): }
		if backoff < 5*time.Second { backoff += time.Second }
	}
}

func (c *hubClient) write(s string) error {
	c.mu.Lock(); defer c.mu.Unlock()
	if c.conn == nil { return fmt.Errorf("VideoHub não conectado") }
	c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := c.conn.Write([]byte(s))
	return err
}

func (c *hubClient) session() error {
	t0 := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(c.dev.Host, strconv.Itoa(c.dev.Port)), 2*time.Second)
	if err != nil { return err }
	lat := float64(time.Since(t0).Microseconds()) / 1000
	c.mu.Lock(); c.conn = conn; c.mu.Unlock()
	defer func() { c.mu.Lock(); c.conn = nil; c.mu.Unlock(); conn.Close() }()
	c.h.Update(c.dev.ID, func(s *DevState) {
		s.LatencyMs = lat
		s.Hub = &HubState{InLabels: map[int]string{}, OutLabels: map[int]string{}, Routes: map[int]int{}, Locks: map[int]string{}}
	})

	type blk struct{ title string; lines []string }
	blocks := make(chan blk, 64)
	rerr := make(chan error, 1)
	go func() {
		r := bufio.NewReader(conn)
		var cur *blk
		for {
			conn.SetReadDeadline(time.Now().Add(12 * time.Second))
			line, err := r.ReadString('\n')
			if err != nil { rerr <- err; return }
			line = strings.TrimRight(line, "\r\n")
			if cur == nil {
				if line == "" { continue }
				if line == "ACK" || line == "NAK" { blocks <- blk{title: line}; continue }
				if strings.HasSuffix(line, ":") { cur = &blk{title: strings.ToUpper(strings.TrimSuffix(line, ":"))}; continue }
				continue
			}
			if line == "" { blocks <- *cur; cur = nil; continue }
			cur.lines = append(cur.lines, line)
		}
	}()

	ping := time.NewTicker(3 * time.Second)
	defer ping.Stop()
	lastRx := time.Now()
	var pingAt time.Time
	for {
		select {
		case <-c.stop: return nil
		case err := <-rerr: return err
		case <-ping.C:
			if time.Since(lastRx) > 9*time.Second { return fmt.Errorf("VideoHub parou de responder (9 s)") }
			pingAt = time.Now()
			c.write("PING:\n\n")
		case b := <-blocks:
			lastRx = time.Now()
			if diagOn.Load() { diagWrite(c.dev.ID, "VHUB", b.title+" | "+strings.Join(b.lines, " | ")) }
			if b.title == "ACK" && !pingAt.IsZero() {
				ms := float64(time.Since(pingAt).Microseconds()) / 1000
				pingAt = time.Time{}
				c.h.Update(c.dev.ID, func(s *DevState) { s.LatencyMs = s.LatencyMs*0.7 + ms*0.3; s.LastSeen = time.Now() })
				continue
			}
			c.apply(b.title, b.lines)
		}
	}
}

func (c *hubClient) apply(title string, lines []string) {
	type lg struct{ typ, sev, msg string; data map[string]any }
	var logs []lg
	becameReady := false
	var latch []ProtectedRoute
	cfgMu.RLock()
	prot := map[int]bool{}
	for _, p := range c.h.cfg.Protected { if p.Device == c.dev.ID { prot[p.Output] = true } }
	cfgMu.RUnlock()

	c.h.Update(c.dev.ID, func(s *DevState) {
		hb := s.Hub
		if hb == nil { return }
		s.LastSeen = time.Now()
		pairs := func(fn func(id int, val string)) {
			for _, l := range lines {
				i := strings.IndexByte(l, ' ')
				if i <= 0 { continue }
				id, err := strconv.Atoi(l[:i])
				if err != nil { continue }
				fn(id, strings.TrimSpace(l[i+1:]))
			}
		}
		switch title {
		case "VIDEOHUB DEVICE":
			for _, l := range lines {
				k, v, _ := strings.Cut(l, ": ")
				switch strings.ToLower(k) {
				case "model name": hb.Model = v
				case "friendly name": hb.Friendly = v
				case "video inputs": hb.Inputs, _ = strconv.Atoi(v)
				case "video outputs": hb.Outputs, _ = strconv.Atoi(v)
				}
			}
		case "INPUT LABELS", "VIDEO INPUT LABELS":
			pairs(func(id int, v string) {
				if old, ok := hb.InLabels[id]; ok && old != v && hb.Ready { logs = append(logs, lg{"LABEL", "trace", fmt.Sprintf("Entrada %d: %q → %q", id+1, old, v), nil}) }
				hb.InLabels[id] = v
			})
		case "OUTPUT LABELS", "VIDEO OUTPUT LABELS":
			pairs(func(id int, v string) { hb.OutLabels[id] = v })
		case "VIDEO OUTPUT LOCKS":
			pairs(func(id int, v string) { hb.Locks[id] = v })
		case "VIDEO OUTPUT ROUTING":
			pairs(func(out int, v string) {
				in, err := strconv.Atoi(v)
				if err != nil { return }
				old, had := hb.Routes[out]
				hb.Routes[out] = in
				if had && old != in && hb.Ready {
					hb.Changes++
					sev := "info"
					ek, mine := fmt.Sprintf("%s:route:%d:%d", c.dev.ID, out, in), false
					if e, ok := c.h.expect[ek]; ok && time.Since(e.at) < 10*time.Second { mine = true } // h.mu já travado por Update
					if prot[out] && !mine { sev = "crit"; latch = append(latch, ProtectedRoute{Device: c.dev.ID, Output: out, Expected: in}) }
					logs = append(logs, lg{"ROTA ALTERADA", sev, fmt.Sprintf("Saída %d %s: %d %s → %d %s", out+1, q(hb.OutLabels[out]), old+1, q(hb.InLabels[old]), in+1, q(hb.InLabels[in])),
						map[string]any{"out": out, "from": old, "to": in}})
				}
			})
			if !hb.Ready && len(hb.Routes) > 0 { hb.Ready = true; becameReady = true }
		case "END PRELUDE":
			if !hb.Ready { hb.Ready = true; becameReady = true }
		}
		if becameReady {
			s.Detail = fmt.Sprintf("%s · %d×%d · %d rotas lidas", nz(nz(hb.Friendly, hb.Model), "VideoHub"), hb.Inputs, hb.Outputs, len(hb.Routes))
		}
	})
	if becameReady {
		c.h.SetOnline(c.dev.ID, true, "")
		c.h.Update(c.dev.ID, func(s *DevState) {
			if s.Hub != nil { s.Detail = fmt.Sprintf("%s · %d×%d · %d rotas", nz(nz(s.Hub.Friendly, s.Hub.Model), "VideoHub"), s.Hub.Inputs, s.Hub.Outputs, len(s.Hub.Routes)) }
		})
	}
	for _, l := range logs {
		o := ""
		if l.typ == "ROTA ALTERADA" { o = c.h.originExact(fmt.Sprintf("%s:route:%v:%v", c.dev.ID, l.data["out"], l.data["to"])) }
		c.h.LogO(o, c.dev.ID, c.dev.Name, l.typ, l.sev, l.msg, l.data)
	}
	for _, p := range latch {
		c.h.Latch(fmt.Sprintf("routechg:%s:%d", c.dev.ID, p.Output), c.dev.ID, "crit", "ROTA PROTEGIDA ALTERADA",
			fmt.Sprintf("Saída protegida %d foi trocada por outro painel/estação (agora entrada %d).", p.Output+1, p.Expected+1))
	}
}

func q(s string) string { if s == "" { return "" }; return "(" + s + ")" }

func hubRoute(id string, out, in int) error {
	v, ok := hubClients.Load(id)
	if !ok { return fmt.Errorf("VideoHub não encontrado") }
	return v.(*hubClient).write(fmt.Sprintf("VIDEO OUTPUT ROUTING:\n%d %d\n\n", out, in))
}
