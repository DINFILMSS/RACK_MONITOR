package main

// HyperDeck — Ethernet Protocol TCP 9993 com conexão PERSISTENTE.
// A V0.7.8 abria e fechava uma conexão a cada ~7,5 s (risco de "120 connection
// rejected"). Aqui há uma única conexão por gravador, com "notify" ligado: as
// mudanças de transporte/mídia chegam na hora (508/502) e a consulta de rotina é 1 s.

import (
	"regexp"
	"sort"
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type deckClient struct {
	h       *Hub
	dev     Device
	stop    chan struct{}
	mu      sync.Mutex
	conn    net.Conn
	fmtMu   sync.Mutex
	fmtWait chan block
	qWait   chan block
	qCodes  map[int]bool
}

var deckClients sync.Map

func startHyperdeck(h *Hub, d Device) func() {
	c := &deckClient{h: h, dev: d, stop: make(chan struct{})}
	deckClients.Store(d.ID, c)
	go c.loop()
	return func() { close(c.stop); c.mu.Lock(); if c.conn != nil { c.conn.Close() }; c.mu.Unlock(); deckClients.Delete(d.ID) }
}

func (c *deckClient) loop() {
	backoff := time.Second
	for {
		select { case <-c.stop: return; default: }
		t0 := time.Now()
		err := c.session()
		select { case <-c.stop: return; default: }
		msg := errText(err)
		if err != nil && (strings.Contains(err.Error(), "EOF") || strings.Contains(err.Error(), "reset") || strings.Contains(err.Error(), "(120)")) && time.Since(t0) < 3*time.Second {
			msg = "OCUPADO: este HyperDeck aceita 1 conexão e ela está com outro programa (Companion?). Use \"Via Companion\" em CONFIGURAÇÃO."
		}
		c.h.SetOnline(c.dev.ID, false, msg)
		select { case <-c.stop: return; case <-time.After(backoff): }
		if backoff < 5*time.Second { backoff += time.Second }
	}
}

func (c *deckClient) write(cmd string) error {
	c.mu.Lock(); defer c.mu.Unlock()
	if c.conn == nil { return fmt.Errorf("HyperDeck não conectado") }
	c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err := c.conn.Write([]byte(cmd + "\r\n"))
	return err
}

type block struct {
	code  int
	title string
	props map[string]string
	raw   []string
}

func (c *deckClient) session() error {
	t0 := time.Now()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(c.dev.Host, strconv.Itoa(c.dev.Port)), 2*time.Second)
	if err != nil { return err }
	lat := float64(time.Since(t0).Microseconds()) / 1000
	c.mu.Lock(); c.conn = conn; c.mu.Unlock()
	defer func() { c.mu.Lock(); c.conn = nil; c.mu.Unlock(); conn.Close() }()

	blocks := make(chan block, 64)
	rerr := make(chan error, 1)
	go readBlocks(conn, blocks, rerr)

	c.h.Update(c.dev.ID, func(s *DevState) {
		if s.Deck == nil { s.Deck = &DeckState{Slots: map[int]*DeckSlot{}} }
		s.LatencyMs = lat
	})
	c.write("device info")
	c.write("notify: transport: true slot: true configuration: true")
	c.write("configuration")
	c.write("transport info")
	slots := 2
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	n := 0
	lastRx := time.Now()
	online := false
	var pingAt time.Time
	for {
		select {
		case <-c.stop: return nil
		case err := <-rerr: return err
		case <-tick.C:
			if time.Since(lastRx) > 6*time.Second { return fmt.Errorf("HyperDeck parou de responder (6 s)") }
			n++
			pingAt = time.Now()
			c.write("transport info")
			if n%3 == 1 { for i := 1; i <= slots; i++ { c.write(fmt.Sprintf("slot info: slot id: %d", i)) } }
			if n%20 == 0 { c.write("configuration") }
		case b := <-blocks:
			lastRx = time.Now()
			if diagOn.Load() { diagWrite(c.dev.ID, "DECK", fmt.Sprintf("%d %s %s", b.code, b.title, strings.Join(b.raw, " | "))) }
			c.mu.Lock()
			if c.qWait != nil && (c.qCodes[b.code] || (b.code >= 100 && b.code < 200 && b.code != 120)) {
				select { case c.qWait <- b: default: }
			}
			if c.fmtWait != nil && (b.code == 216 || (b.code >= 100 && b.code < 200 && b.code != 120)) {
				select { case c.fmtWait <- b: default: }
			}
			c.mu.Unlock()
			if b.code == 120 { return fmt.Errorf("HyperDeck recusou: limite de conexões (120). Feche outros clientes") }
			if !online && b.code >= 200 && b.code < 600 {
				online = true
				c.h.SetOnline(c.dev.ID, true, "")
			}
			if b.code == 208 && !pingAt.IsZero() {
				ms := float64(time.Since(pingAt).Microseconds()) / 1000
				c.h.Update(c.dev.ID, func(s *DevState) { s.LatencyMs = s.LatencyMs*0.7 + ms*0.3 })
			}
			if b.code == 204 {
				if v, err := strconv.Atoi(b.props["slot count"]); err == nil && v > 0 && v < 8 { slots = v }
			}
			c.apply(b)
		}
	}
}

func readBlocks(conn net.Conn, out chan<- block, rerr chan<- error) {
	r := bufio.NewReader(conn)
	for {
		conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		line, err := r.ReadString('\n')
		if err != nil { rerr <- err; return }
		line = strings.TrimRight(line, "\r\n")
		if line == "" { continue }
		if len(line) < 3 { continue }
		code, err := strconv.Atoi(line[:3])
		if err != nil { continue }
		b := block{code: code, title: strings.TrimSpace(line[3:]), props: map[string]string{}}
		if strings.HasSuffix(line, ":") {
			for {
				conn.SetReadDeadline(time.Now().Add(5 * time.Second))
				l, err := r.ReadString('\n')
				if err != nil { rerr <- err; return }
				l = strings.TrimRight(l, "\r\n")
				if l == "" { break }
				b.raw = append(b.raw, l)
				if i := strings.Index(l, ": "); i > 0 { b.props[strings.ToLower(strings.TrimSpace(l[:i]))] = strings.TrimSpace(l[i+2:]) } else if strings.HasSuffix(l, ":") { b.props[strings.ToLower(strings.TrimSuffix(l, ":"))] = "" }
			}
		}
		out <- b
	}
}

func (c *deckClient) apply(b block) {
	type lg struct{ typ, sev, msg string; data map[string]any }
	var logs []lg
	stall := c.h.cfg.Rules.TCStallSec
	c.h.Update(c.dev.ID, func(s *DevState) {
		d := s.Deck
		if d == nil { d = &DeckState{Slots: map[int]*DeckSlot{}}; s.Deck = d }
		s.LastSeen = time.Now()
		p := b.props
		switch b.code {
		case 500:
			if v := p["model"]; v != "" { d.Model = v }
			d.Protocol = p["protocol version"]
		case 204:
			d.Model = nz(p["model"], d.Model)
			d.Protocol = nz(p["protocol version"], d.Protocol)
			d.Software = p["software version"]
			if v, err := strconv.Atoi(p["slot count"]); err == nil { d.SlotCount = v }
			s.Detail = strings.TrimSpace(d.Model + " · protocolo " + d.Protocol + " · " + d.Software)
		case 208, 508:
			if v, ok := p["status"]; ok {
				v = strings.ToLower(v)
				if v != d.Transport && d.Transport != "" {
					if v == "record" {
						sn := takeSentName(c.dev.ID)
						dd := map[string]any{"slot": d.ActiveSlot}
						if v, err := strconv.Atoi(p["slot id"]); err == nil { dd["slot"] = v }
						if sn != "" { dd["clipName"] = sn }
						logs = append(logs, lg{"REC INICIADO", "info", "Gravação iniciada" + clipTxt(p) + map[bool]string{true: " · nome " + sn, false: ""}[sn != ""], dd})
					} else if d.Transport == "record" {
						logs = append(logs, lg{"REC PARADO", "info", fmt.Sprintf("Gravação parada após %s (transporte: %s)", human(time.Since(d.RecSince)), transportPT(v)), nil})
					} else {
						logs = append(logs, lg{"TRANSPORTE", "trace", transportPT(d.Transport) + " → " + transportPT(v), nil})
					}
				}
				if v == "record" && d.Transport != "record" { d.RecSince = time.Now(); d.lastTCChange = time.Now(); d.RecSinceConn = d.Transport == "" }
				if d.Transport == "" && v == "record" {
					logs = append(logs, lg{"REC INICIADO", "info", "Já estava gravando ao conectar.", map[string]any{"initial": true}})
				}
				d.Transport = v
			}
			if v, ok := p["speed"]; ok { d.Speed = v }
			if v, err := strconv.Atoi(p["slot id"]); err == nil {
				if d.ActiveSlot != 0 && v != d.ActiveSlot {
					sev, txt := "info", "Slot ativo"
					if d.Transport == "record" || strings.ToLower(p["status"]) == "record" { sev, txt = "warn", "Gravação" }
					vol := ""
					if sl := d.Slots[v]; sl != nil && sl.Volume != "" { vol = " (" + sl.Volume + ")" }
					logs = append(logs, lg{"TROCA DE SLOT", sev, fmt.Sprintf("%s passou do slot %d para o slot %d%s · TC %s", txt, d.ActiveSlot, v, vol, nz(p["display timecode"], d.TC)), map[string]any{"from": d.ActiveSlot, "to": v}})
				}
				d.ActiveSlot = v
			}
			if v, ok := p["clip id"]; ok { d.Clip = v }
			if v := nz(p["display timecode"], p["timecode"]); v != "" {
				if v != d.lastTC { d.lastTC = v; d.lastTCChange = time.Now() }
				d.TC = v
				d.tcAt = time.Now()
			}
			if v := p["video format"]; v != "" { d.VideoFormat = v }
			wasStall := d.TCStall
			d.TCStall = d.Transport == "record" && stall > 0 && time.Since(d.lastTCChange) > time.Duration(stall)*time.Second
			if d.TCStall && !wasStall { logs = append(logs, lg{"TIMECODE PARADO", "warn", "Em REC com timecode parado em " + d.TC, nil}) }
		case 202, 502:
			id, err := strconv.Atoi(p["slot id"])
			if err != nil { return }
			sl := d.Slots[id]
			if sl == nil { sl = &DeckSlot{}; d.Slots[id] = sl }
			if v, ok := p["status"]; ok {
				v = strings.ToLower(v)
				if sl.Status != "" && v != sl.Status {
					sev := "info"
					if v == "error" || (v == "empty" && id == d.ActiveSlot) { sev = "warn" }
					logs = append(logs, lg{"MÍDIA", sev, fmt.Sprintf("Slot %d: %s → %s", id, mediaPT(sl.Status), mediaPT(v)), nil})
				}
				sl.Status = v
			}
			if v, ok := p["volume name"]; ok { sl.Volume = v }
			if v, err := strconv.Atoi(p["recording time"]); err == nil { sl.RemainSec = v; sl.RemainKnown = true }
			if sl.Status == "empty" { sl.RemainSec = 0 }
			if v := p["video format"]; v != "" { sl.VideoFormat = v }
			t := 0
			for _, x := range d.Slots { if x.Status == "mounted" { t += x.RemainSec } }
			d.TotalRemain = t
		case 211, 511:
			if v := p["file format"]; v != "" { d.FileFormat = v }
		}
	})
	for _, l := range logs {
		o := ""
		if l.typ == "REC INICIADO" || l.typ == "REC PARADO" { if init, _ := l.data["initial"].(bool); !init { o = c.h.originFor(c.dev.ID) } }
		c.h.LogO(o, c.dev.ID, c.dev.Name, l.typ, l.sev, l.msg, l.data)
	}
}

func clipTxt(p map[string]string) string {
	if v := p["slot id"]; v != "" { return " no slot " + v }
	return ""
}

func mediaPT(s string) string {
	switch s {
	case "mounted": return "MONTADA"
	case "empty": return "SEM MÍDIA"
	case "mounting": return "MONTANDO"
	case "error": return "ERRO"
	}
	return strings.ToUpper(nz(s, "?"))
}

var clipNameRe = strings.NewReplacer("\r", " ", "\n", " ", ":", "-")

func deckCommand(id, action, name string) error {
	v, ok := deckClients.Load(id)
	if !ok { return fmt.Errorf("HyperDeck não encontrado") }
	c := v.(*deckClient)
	switch action {
	case "record":
		if n := strings.TrimSpace(clipNameRe.Replace(name)); n != "" { return c.write("record: name: " + n) }
		return c.write("record")
	case "stop":
		return c.write("stop")
	}
	return fmt.Errorf("ação HyperDeck desconhecida: %s", action)
}

// ---------- formatação de mídia (protocolo HyperDeck: prepare → token → confirm) ----------

var cardNameRe = strings.NewReplacer(" ", "_", ":", "", "/", "", "\\", "", "\r", "", "\n", "")

// cardName: exFAT aceita no máximo 11 caracteres no nome do volume; HFS+ bem mais.
func cardName(name, fs string) string {
	name = safeTok(cardNameRe.Replace(strings.TrimSpace(name)))
	max := 11
	if fs == "HFS+" { max = 27 }
	if len(name) > max { name = strings.TrimRight(name[:max], "_-") }
	return name
}

func deckFormat(id string, slot int, fs, name string) (string, error) {
	v, ok := deckClients.Load(id)
	if !ok { return "", fmt.Errorf("HyperDeck não encontrado") }
	c := v.(*deckClient)
	c.fmtMu.Lock(); defer c.fmtMu.Unlock()
	if fs != "HFS+" { fs = "exFAT" }
	name = cardName(name, fs)
	wait := make(chan block, 4)
	c.mu.Lock(); c.fmtWait = wait; c.mu.Unlock()
	defer func() { c.mu.Lock(); c.fmtWait = nil; c.mu.Unlock() }()
	get := func() (string, error) {
		select {
		case b := <-wait:
			if b.code == 216 {
				for _, l := range b.raw { if t := strings.TrimSpace(l); t != "" { if i := strings.Index(t, ": "); i > 0 { t = strings.TrimSpace(t[i+2:]) }; return t, nil } }
				return "", fmt.Errorf("gravador não devolveu o código de confirmação")
			}
			return "", fmt.Errorf("gravador recusou: %d %s", b.code, b.title)
		case <-time.After(4 * time.Second):
			return "", fmt.Errorf("sem resposta do gravador")
		}
	}
	cmd := fmt.Sprintf("format: slot id: %d prepare: %s", slot, fs)
	if name != "" { cmd += " name: " + name }
	if err := c.write(cmd); err != nil { return "", err }
	tok, err := get()
	if err != nil && name != "" {
		// alguns firmwares recusam o nome (102 invalid value): tenta mantendo o nome atual
		for len(wait) > 0 { <-wait }
		if e := c.write(fmt.Sprintf("format: slot id: %d prepare: %s", slot, fs)); e != nil { return "", e }
		if tok, err = get(); err == nil { name = "" }
	}
	if err != nil {
		// firmwares antigos: seleciona o slot e usa a forma curta
		c.write(fmt.Sprintf("slot select: slot id: %d", slot))
		time.Sleep(300 * time.Millisecond)
		for len(wait) > 0 { <-wait }
		short := "format: prepare: " + fs
		if name != "" { short += " name: " + name }
		if e := c.write(short); e != nil { return "", e }
		if tok, err = get(); err != nil {
			if strings.Contains(err.Error(), "102") { return "", fmt.Errorf("o gravador recusou (102 valor inválido): confira se o slot %d tem cartão montado", slot) }
			return "", fmt.Errorf("formatação não aceita (%v)", err)
		}
	}
	if err := c.write("format: confirm: " + tok); err != nil { return "", err }
	return fmt.Sprintf("slot %d · %s%s", slot, fs, map[bool]string{true: " · nome " + name, false: ""}[name != ""]), nil
}

// ---------- lista de clipes por cartão ("disk list") ----------

type Clip struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Codec    string `json:"codec"`
	Format   string `json:"format"`
	Duration string `json:"duration"`
	Start    string `json:"start,omitempty"`
}

type SlotClips struct {
	Slot   int    `json:"slot"`
	Volume string `json:"volume"`
	Status string `json:"status"`
	Clips  []Clip `json:"clips"`
	Error  string `json:"error,omitempty"`
}

func (c *deckClient) query(cmd string, codes ...int) (block, error) {
	c.fmtMu.Lock(); defer c.fmtMu.Unlock()
	ch := make(chan block, 4)
	set := map[int]bool{}
	for _, x := range codes { set[x] = true }
	c.mu.Lock(); c.qWait, c.qCodes = ch, set; c.mu.Unlock()
	defer func() { c.mu.Lock(); c.qWait, c.qCodes = nil, nil; c.mu.Unlock() }()
	if err := c.write(cmd); err != nil { return block{}, err }
	select {
	case b := <-ch:
		if b.code < 200 { return b, fmt.Errorf("gravador respondeu %d %s", b.code, b.title) }
		return b, nil
	case <-time.After(5 * time.Second):
		return block{}, fmt.Errorf("sem resposta")
	}
}

var tcTok = regexp.MustCompile(`^\d{2}:\d{2}:\d{2}[:;]\d{2}$`)

func parseClipLine(l string) (Clip, bool) {
	i := strings.Index(l, ": ")
	if i <= 0 { return Clip{}, false }
	id, err := strconv.Atoi(strings.TrimSpace(l[:i]))
	if err != nil { return Clip{}, false }
	f := strings.Fields(l[i+2:])
	if len(f) == 0 { return Clip{}, false }
	c := Clip{ID: id}
	// formatos: "nome codec formato duração" (disk list) ou "nome início duração" (clips get)
	n := len(f)
	switch {
	case n >= 4 && tcTok.MatchString(f[n-1]) && !tcTok.MatchString(f[n-2]):
		c.Duration, c.Format, c.Codec = f[n-1], f[n-2], f[n-3]
		c.Name = strings.Join(f[:n-3], " ")
	case n >= 3 && tcTok.MatchString(f[n-1]) && tcTok.MatchString(f[n-2]):
		c.Duration, c.Start = f[n-1], f[n-2]
		c.Name = strings.Join(f[:n-2], " ")
	default:
		c.Name = strings.Join(f, " ")
	}
	return c, true
}

func deckClips(h *Hub, id string) ([]SlotClips, error) {
	v, ok := deckClients.Load(id)
	if !ok { return nil, fmt.Errorf("HyperDeck não encontrado") }
	c := v.(*deckClient)
	h.mu.Lock()
	type si struct{ n int; vol, st string }
	var slots []si
	if s := h.states[id]; s != nil && s.Deck != nil {
		for n, x := range s.Deck.Slots { slots = append(slots, si{n, x.Volume, x.Status}) }
	}
	h.mu.Unlock()
	sort.Slice(slots, func(a, b int) bool { return slots[a].n < slots[b].n })
	out := []SlotClips{}
	for _, s := range slots {
		sc := SlotClips{Slot: s.n, Volume: s.vol, Status: s.st}
		if s.st != "mounted" { out = append(out, sc); continue }
		b, err := c.query(fmt.Sprintf("disk list: slot id: %d", s.n), 206)
		if err != nil { sc.Error = err.Error() } else {
			for _, l := range b.raw { if cl, ok := parseClipLine(l); ok { sc.Clips = append(sc.Clips, cl) } }
		}
		out = append(out, sc)
	}
	return out, nil
}
