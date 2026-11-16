package main

// ATEM — protocolo nativo UDP 9910, SEM Blackmagic ATEM SDK / COM.
// É o mesmo caminho usado pelo módulo ATEM do Companion: o software fala
// diretamente com a mesa pela rede, como mais um painel de controle.
//
// Pacote: cabeçalho de 12 bytes
//   [0..1]  flags(5 bits) << 11 | tamanho(11 bits)
//   [2..3]  id de sessão
//   [4..5]  id do pacote remoto confirmado (ACK)
//   [10..11] id do pacote (sequência) — usado quando o flag "AckRequest" está presente
// Corpo: comandos de [tamanho u16][reservado u16][nome 4 bytes][dados].

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	flAckRequest = 0x01
	flHello      = 0x02
	flResend     = 0x04
	flRequestRes = 0x08
	flAck        = 0x10
)

type atemClient struct {
	ccu     ccuLogger
	ccuBase map[string]ccuBase
	h       *Hub
	dev     Device
	stop    chan struct{}
	mu      sync.Mutex
	conn    *net.UDPConn
	session uint16
	localID uint16
	inflight map[uint16]*atemPending
	ready   bool
}

type atemPending struct {
	pkt   []byte
	sent  time.Time
	tries int
}

func startAtem(h *Hub, d Device) func() {
	c := &atemClient{h: h, dev: d, stop: make(chan struct{}), inflight: map[uint16]*atemPending{}}
	go c.loop()
	go c.ccuFlushLoop()
	return func() { close(c.stop); c.mu.Lock(); if c.conn != nil { c.conn.Close() }; c.mu.Unlock() }
}

var atemClients sync.Map // id -> *atemClient (para comandos)

func header(flags byte, length int, session, ack, id uint16) []byte {
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[0:], uint16(flags)<<11|uint16(length&0x07ff))
	binary.BigEndian.PutUint16(b[2:], session)
	binary.BigEndian.PutUint16(b[4:], ack)
	binary.BigEndian.PutUint16(b[10:], id)
	return b
}

func (c *atemClient) loop() {
	atemClients.Store(c.dev.ID, c)
	defer atemClients.Delete(c.dev.ID)
	backoff := time.Second
	for {
		select { case <-c.stop: return; default: }
		err := c.session1()
		select { case <-c.stop: return; default: }
		c.h.SetOnline(c.dev.ID, false, errText(err))
		c.h.Update(c.dev.ID, func(s *DevState) { if s.Atem != nil { s.Atem.Ready = false } })
		select { case <-c.stop: return; case <-time.After(backoff): }
		if backoff < 5*time.Second { backoff += time.Second }
	}
}

func errText(err error) string {
	if err == nil { return "conexão encerrada" }
	s := err.Error()
	if strings.Contains(s, "i/o timeout") { return "sem resposta (timeout)" }
	if strings.Contains(s, "refused") { return "porta recusada" }
	return s
}

func (c *atemClient) session1() error {
	raddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", c.dev.Host, c.dev.Port))
	if err != nil { return err }
	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil { return err }
	c.mu.Lock(); c.conn = conn; c.session = uint16(rand.Intn(0x7fff)); c.localID = 0; c.inflight = map[uint16]*atemPending{}; c.ready = false; c.mu.Unlock()
	defer func() { c.mu.Lock(); c.conn = nil; c.ready = false; c.mu.Unlock(); conn.Close() }()

	hello := append(header(flHello, 20, c.session, 0, 0), 0x01, 0, 0, 0, 0, 0, 0, 0)
	hello[9] = 0x3a
	buf := make([]byte, 2048)
	t0 := time.Now()
	var lastRemote uint16
	gotHello := false
	for tries := 0; tries < 3 && !gotHello; tries++ {
		conn.Write(hello)
		conn.SetReadDeadline(time.Now().Add(1200 * time.Millisecond))
		for {
			n, err := conn.Read(buf)
			if err != nil { break }
			if n < 12 { continue }
			flags := buf[0] >> 3
			if flags&flHello != 0 {
				// byte 12 da resposta: 0x02 = aceito; 0x03 = recusado (mesa sem vaga)
				if n >= 13 && buf[12] == 0x03 {
					return fmt.Errorf("ATEM SEM VAGA: a mesa recusou a conexão — limite de clientes atingido (painéis, ATEM Software Control, Companion, CCU…). Feche um cliente sem uso")
				}
				c.session = binary.BigEndian.Uint16(buf[2:])
				lastRemote = binary.BigEndian.Uint16(buf[10:])
				conn.Write(header(flAck, 12, c.session, lastRemote, 0))
				gotHello = true
				break
			}
		}
	}
	if !gotHello {
		if ok, _ := pingOnce(c.dev.Host, time.Second); ok {
			return fmt.Errorf("ATEM SEM VAGA? A mesa responde ao ping mas não aceitou a conexão de controle — provável limite de clientes atingido (painéis, ATEM Software Control, Companion, CCU…). Feche um cliente sem uso")
		}
		return fmt.Errorf("sem resposta UDP %d (ATEM desligada, IP errado ou rede)", c.dev.Port)
	}
	latency := float64(time.Since(t0).Microseconds()) / 1000
	c.h.Update(c.dev.ID, func(s *DevState) {
		s.Atem = newAtemState(); s.LatencyMs = latency; s.Detail = "handshake ok, recebendo estado da mesa…"
	})

	lastRx := time.Now()
	resend := time.NewTicker(200 * time.Millisecond)
	defer resend.Stop()
	rx := make(chan []byte, 512)
	rerr := make(chan error, 1)
	go func() {
		b := make([]byte, 4096)
		for {
			conn.SetReadDeadline(time.Now().Add(6 * time.Second))
			n, err := conn.Read(b)
			if err != nil { rerr <- err; return }
			p := make([]byte, n); copy(p, b[:n]); rx <- p
		}
	}()
	expect := lastRemote
	for {
		select {
		case <-c.stop: return nil
		case err := <-rerr: return err
		case <-resend.C:
			if time.Since(lastRx) > 5*time.Second {
				c.mu.Lock(); rd := c.ready; c.mu.Unlock()
				if !rd && expect == lastRemote {
					return fmt.Errorf("ATEM SEM VAGA? Aceitou o contato mas não enviou o estado — provável limite de clientes atingido. Feche um cliente sem uso")
				}
				return fmt.Errorf("ATEM parou de responder (5 s sem pacotes)")
			}
			c.mu.Lock()
			for id, p := range c.inflight {
				if time.Since(p.sent) > 400*time.Millisecond {
					if p.tries >= 6 { delete(c.inflight, id); continue }
					p.pkt[0] |= flResend << 3
					conn.Write(p.pkt); p.sent = time.Now(); p.tries++
				}
			}
			c.mu.Unlock()
		case p := <-rx:
			if len(p) < 12 { continue }
			lastRx = time.Now()
			flags := p[0] >> 3
			c.mu.Lock(); c.session = binary.BigEndian.Uint16(p[2:]); c.mu.Unlock()
			remote := binary.BigEndian.Uint16(p[10:])
			if flags&flAck != 0 {
				ackID := binary.BigEndian.Uint16(p[4:])
				c.mu.Lock()
				for id := range c.inflight { if seqLE(id, ackID) { delete(c.inflight, id) } }
				c.mu.Unlock()
			}
			if flags&flHello != 0 { continue }
			if flags&flAckRequest != 0 {
				next := (expect + 1) & 0x7fff
				if remote == next {
					expect = remote
					conn.Write(header(flAck, 12, c.session, remote, 0))
					if len(p) > 12 { c.parse(p[12:]) }
				} else if seqLE(remote, expect) {
					// repetido: confirma de novo para a mesa parar de reenviar
					conn.Write(header(flAck, 12, c.session, remote, 0))
				}
				// lacuna: ignora e espera o reenvio automático da ATEM (mantém a ordem dos comandos)
			}
		}
	}
}

// seqLE: a <= b em sequência de 15 bits com wrap.
func seqLE(a, b uint16) bool { return ((b - a) & 0x7fff) < 0x4000 }

func cstr(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 { b = b[:i] }
	return strings.TrimSpace(strings.ToValidUTF8(string(b), "?"))
}

func u16(b []byte, off int) int { if len(b) < off+2 { return 0 }; return int(binary.BigEndian.Uint16(b[off:])) }
func u8(b []byte, off int) int  { if len(b) < off+1 { return 0 }; return int(b[off]) }

type atemLog struct{ typ, sev, msg string; data map[string]any }

func (c *atemClient) parse(body []byte) {
	var logs []atemLog
	becameReady := false
	logPVW := c.h.cfg.Rules.LogPreview
	ccuOff := c.h.cfg.Rules.CCUOff
	ccuPct := float64(c.h.cfg.Rules.CCUMinPct) / 100
	if ccuPct <= 0 { ccuPct = 0.05 }
	if c.ccuBase == nil { c.ccuBase = map[string]ccuBase{} }
	cfgMu.RLock()
	auxCfg := c.h.cfg.auxMap(c.dev.ID)
	cfgMu.RUnlock()
	c.h.Update(c.dev.ID, func(s *DevState) {
		a := s.Atem
		if a == nil { a = newAtemState(); s.Atem = a }
		a.PacketsIn++
		s.LastSeen = time.Now()
		me := func(i int) *AtemME {
			m, ok := a.MEs[i]
			if !ok { m = &AtemME{Program: -1, Preview: -1, Keys: map[int]bool{}}; a.MEs[i] = m }
			return m
		}
		for off := 0; off+8 <= len(body); {
			l := int(binary.BigEndian.Uint16(body[off:]))
			if l < 8 || off+l > len(body) { break }
			name := string(body[off+4 : off+8])
			d := body[off+8 : off+l]
			off += l
			atemCount(c.dev.ID, name)
			diagWrite(c.dev.ID, "ATEM", name+" "+diagHex(d))
			switch name {
			case "_ver":
				a.Version = fmt.Sprintf("%d.%d", u16(d, 0), u16(d, 2))
			case "_pin":
				a.Product = cstr(d)
			case "InPr":
				if len(d) >= 26 {
					id := u16(d, 0)
					in := a.Inputs[id]
					if in == nil { in = &AtemInput{ID: id}; a.Inputs[id] = in }
					in.Long, in.Short = cstr(d[2:22]), cstr(d[22:26])
					if len(d) >= 33 { in.Port = u8(d, 32) }
				}
			case "PrgI":
				i, src := u8(d, 0), u16(d, 2)
				m := me(i)
				if m.Program != src {
					old := m.Program
					m.Program = src; m.ProgramSince = time.Now()
					if a.Ready {
						logs = append(logs, atemLog{"PGM", "info", fmt.Sprintf("M/E %d: %s → %s", i+1, a.Name(old), a.Name(src)),
							map[string]any{"me": i, "src": src, "name": a.Name(src), "from": old, "fromName": a.Name(old), "tcs": c.h.tcSnapshotLocked()}})
					}
				}
			case "PrvI":
				i, src := u8(d, 0), u16(d, 2)
				m := me(i)
				if m.Preview != src {
					old := m.Preview
					m.Preview = src
					if a.Ready && logPVW {
						logs = append(logs, atemLog{"PVW", "trace", fmt.Sprintf("M/E %d preview: %s → %s", i+1, a.Name(old), a.Name(src)), map[string]any{"me": i, "src": src}})
					}
				}
			case "AuxS":
				i, src := u8(d, 0), u16(d, 2)
				old, had := a.Aux[i]
				if !had || old != src {
					a.Aux[i] = src
					if a.Ready && had && auxCfg[i].Mode != "ignore" {
						sev := "info"
						if auxCfg[i].Mode == "lock" { sev = "warn" }
						logs = append(logs, atemLog{"AUX ALTERADO", sev, fmt.Sprintf("%s: %s → %s", auxTitle(a, i, auxCfg[i]), a.Name(old), a.Name(src)), map[string]any{"aux": i, "src": src}})
					}
				}
			case "TrPs":
				m := me(u8(d, 0))
				m.InTransition = u8(d, 1) == 1
				m.TransPos = u16(d, 4)
			case "FtbS":
				i := u8(d, 0)
				m := me(i)
				black := u8(d, 1) == 1
				m.FTBTrans = u8(d, 2) == 1
				if black != m.FTB && a.Ready {
					st := "FADE TO BLACK ATIVADO"
					if !black { st = "FADE TO BLACK DESATIVADO" }
					logs = append(logs, atemLog{st, "warn", fmt.Sprintf("M/E %d", i+1), nil})
				}
				m.FTB = black
			case "KeOn":
				m := me(u8(d, 0))
				k, on := u8(d, 1), u8(d, 2) == 1
				if m.Keys[k] != on && a.Ready {
					logs = append(logs, atemLog{"KEY", "info", fmt.Sprintf("M/E %d USK %d %s", u8(d, 0)+1, k+1, onOff(on)), nil})
				}
				m.Keys[k] = on
			case "DskS":
				k, on := u8(d, 0), u8(d, 1) == 1
				if a.DSK[k] != on && a.Ready {
					logs = append(logs, atemLog{"DSK", "info", fmt.Sprintf("DSK %d %s", k+1, onOff(on)), nil})
				}
				a.DSK[k] = on
			case "TlSr":
				n := u16(d, 0)
				for i := 0; i < n; i++ {
					o := 2 + i*3
					if o+3 > len(d) { break }
					a.Tally[u16(d, o)] = u8(d, o+2)
				}
			case "StRS":
				st := map[int]string{1: "PARADO", 2: "CONECTANDO", 4: "STREAMING", 32: "PARANDO"}[u16(d, 0)]
				if st == "" { st = fmt.Sprintf("ESTADO %d", u16(d, 0)) }
				if st != a.Stream && a.Ready { logs = append(logs, atemLog{"STREAM", "info", "Streaming: " + st, nil}) }
				a.Stream = st
			case "SRST":
				a.StreamTime = fmt.Sprintf("%02d:%02d:%02d", u8(d, 0), u8(d, 1), u8(d, 2))
			case "RTMS":
				st := map[int]string{0: "PARADO", 1: "GRAVANDO", 128: "PARANDO"}[u16(d, 0)]
				if st == "" { st = fmt.Sprintf("ESTADO %d", u16(d, 0)) }
				if st != a.Rec && a.Ready { logs = append(logs, atemLog{"REC ATEM", "info", "Gravação interna da ATEM: " + st, nil}) }
				a.Rec = st
			case "RTMR":
				a.RecTime = fmt.Sprintf("%02d:%02d:%02d", u8(d, 0), u8(d, 1), u8(d, 2))
			case "MPrp":
				idx, used := u16(d, 0), u8(d, 2) == 1
				nl := u16(d, 4)
				if used && len(d) >= 8+nl { a.Macros[idx] = cstr(d[8 : 8+nl]) } else { delete(a.Macros, idx) }
			case "MRPr":
				running := u8(d, 0)&1 == 1
				idx := u16(d, 2)
				if running && a.MacroRunning != idx && a.Ready {
					logs = append(logs, atemLog{"MACRO", "info", "Macro em execução: " + nz(a.Macros[idx], fmt.Sprintf("#%d", idx+1)), nil})
				}
				if running { a.MacroRunning = idx } else { a.MacroRunning = -1 }
			case "Warn":
				a.Warning = cstr(d)
				logs = append(logs, atemLog{"AVISO ATEM", "warn", a.Warning, nil})
			case "CCdP":
				in, param, val, logIt := ccuDecode(d)
				if param == "" { break }
				cs := a.Cams[in]
				if cs == nil { cs = &CamState{Input: in, Values: map[string]string{}}; a.Cams[in] = cs }
				if cs.Values[param] != val { cs.Values[param] = val; cs.Updated = time.Now() }
				// registra só quando o valor se afasta do ÚLTIMO REGISTRADO além do limiar
				// (oscilação de 0,01 de câmera em automático não vira evento)
				nums, th := ccuNums(d, ccuPct)
				bk := fmt.Sprintf("%d|%s", in, param)
				base, seen := c.ccuBase[bk]
				if !seen || !a.Ready {
					c.ccuBase[bk] = ccuBase{nums, val}
				} else if logIt && !ccuOff && maxDiff(nums, base.nums) >= th {
					c.ccu.note(in, param, base.str, val)
					c.ccuBase[bk] = ccuBase{nums, val}
				}
			case "InCm":
				if !a.Ready { a.Ready = true; becameReady = true }
			}
		}
		if becameReady {
			s.Detail = fmt.Sprintf("%s · firmware protocolo %s · %d M/E · %d AUX · %d entradas", nz(a.Product, "ATEM"), a.Version, len(a.MEs), len(a.Aux), len(a.Inputs))
		}
	})
	if becameReady {
		c.mu.Lock(); c.ready = true; c.mu.Unlock()
		c.h.SetOnline(c.dev.ID, true, "")
		c.h.Update(c.dev.ID, func(s *DevState) {
			s.Detail = fmt.Sprintf("%s · protocolo %s · %d M/E · %d AUX · %d fontes", nz(s.Atem.Product, "ATEM"), s.Atem.Version, len(s.Atem.MEs), len(s.Atem.Aux), len(s.Atem.Inputs))
		})
		// estado inicial de PGM para o relatório
		var init []atemLog
		c.h.Update(c.dev.ID, func(s *DevState) {
			for i, m := range s.Atem.MEs {
				init = append(init, atemLog{"PGM", "info", fmt.Sprintf("M/E %d no ar: %s (ao conectar)", i+1, s.Atem.Name(m.Program)),
					map[string]any{"me": i, "src": m.Program, "name": s.Atem.Name(m.Program), "initial": true, "tcs": c.h.tcSnapshotLocked()}})
			}
		})
		logs = append(init, logs...)
	}
	for _, l := range logs {
		o := ""
		if l.typ == "AUX ALTERADO" {
			o = c.h.originExact(fmt.Sprintf("%s:aux:%v:%v", c.dev.ID, l.data["aux"], l.data["src"]))
		} else if l.typ == "FADE TO BLACK ATIVADO" || l.typ == "FADE TO BLACK DESATIVADO" || l.typ == "MACRO" { o = c.h.originFor(c.dev.ID) }
		c.h.LogO(o, c.dev.ID, c.dev.Name, l.typ, l.sev, l.msg, l.data)
	}
}

func onOff(b bool) string { if b { return "NO AR" }; return "FORA" }

// ---------- comandos (somente com controle liberado e armado) ----------

func (c *atemClient) send(name string, data []byte) error {
	c.mu.Lock(); defer c.mu.Unlock()
	if c.conn == nil || !c.ready { return fmt.Errorf("ATEM não conectada") }
	cmd := make([]byte, 8+len(data))
	binary.BigEndian.PutUint16(cmd[0:], uint16(len(cmd)))
	copy(cmd[4:8], name)
	copy(cmd[8:], data)
	c.localID = (c.localID + 1) & 0x7fff
	pkt := append(header(flAckRequest, 12+len(cmd), c.session, 0, c.localID), cmd...)
	c.inflight[c.localID] = &atemPending{pkt: pkt, sent: time.Now()}
	_, err := c.conn.Write(pkt)
	return err
}

func atemCommand(id, action string, a, b int) error {
	v, ok := atemClients.Load(id)
	if !ok { return fmt.Errorf("ATEM não encontrada") }
	c := v.(*atemClient)
	d := make([]byte, 4)
	switch action {
	case "cut":
		d[0] = byte(a); return c.send("DCut", d)
	case "auto":
		d[0] = byte(a); return c.send("DAut", d)
	case "ftb":
		d[0] = byte(a); return c.send("FtbA", d)
	case "pgm":
		d[0] = byte(a); binary.BigEndian.PutUint16(d[2:], uint16(b)); return c.send("CPgI", d)
	case "pvw":
		d[0] = byte(a); binary.BigEndian.PutUint16(d[2:], uint16(b)); return c.send("CPvI", d)
	case "aux":
		d[0] = 1; d[1] = byte(a); binary.BigEndian.PutUint16(d[2:], uint16(b)); return c.send("CAuS", d)
	case "macro":
		binary.BigEndian.PutUint16(d[0:], uint16(a)); d[2] = 0; return c.send("MAct", d)
	}
	return fmt.Errorf("ação ATEM desconhecida: %s", action)
}

// atemDiscover: envia um hello e verifica se responde como ATEM (para a varredura de rede).
func atemDiscover(ip string, port int, timeout time.Duration) bool {
	raddr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", ip, port))
	if err != nil { return false }
	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil { return false }
	defer conn.Close()
	sess := uint16(rand.Intn(0x7fff))
	hello := append(header(flHello, 20, sess, 0, 0), 0x01, 0, 0, 0, 0, 0, 0, 0)
	conn.Write(hello)
	conn.SetReadDeadline(time.Now().Add(timeout))
	b := make([]byte, 64)
	n, err := conn.Read(b)
	if err != nil || n < 12 || (b[0]>>3)&flHello == 0 { return false }
	// encerra educadamente: nada a fazer, a ATEM expira a sessão sozinha.
	return true
}
