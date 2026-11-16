package main

// MODO DEMONSTRAÇÃO: simuladores de ATEM (UDP), HyperDeck e VideoHub que falam
// o protocolo real. Servem para conhecer o software sem tocar na rede técnica
// e foram usados para validar os drivers.

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ================= ATEM =================

type simAtem struct {
	mu      sync.Mutex
	conn    *net.UDPConn
	clients map[string]*simAtemClient
	product string
	inputs  map[int][2]string
	order   []int
	pgm     []int
	pvw     []int
	aux     []int
	auxNames []string
	ftb     []bool
	dsk     [2]bool
	keys    []bool
	stream  int
	macros  []string
	ccuIris map[int]int
	cams    []int
}

type simAtemClient struct {
	addr    *net.UDPAddr
	session uint16
	id      uint16
	ready   bool
}

func cmd(name string, data []byte) []byte {
	b := make([]byte, 8+len(data))
	binary.BigEndian.PutUint16(b, uint16(len(b)))
	copy(b[4:], name)
	copy(b[8:], data)
	return b
}

func be16(v int) []byte { b := make([]byte, 2); binary.BigEndian.PutUint16(b, uint16(v)); return b }

func fixed(s string, n int) []byte { b := make([]byte, n); copy(b, s); return b }

func runSimAtem(addr, product string, cams []string, stream bool, nME, nAux int, auxNames []string) {
	ua, _ := net.ResolveUDPAddr("udp4", addr)
	conn, err := net.ListenUDP("udp4", ua)
	if err != nil { log.Printf("sim ATEM %s: %v", addr, err); return }
	s := &simAtem{conn: conn, clients: map[string]*simAtemClient{}, product: product, inputs: map[int][2]string{}}
	for i, c := range cams {
		id := i + 1
		s.inputs[id] = [2]string{c, fmt.Sprintf("C%d", id)}
		s.order = append(s.order, id)
		s.cams = append(s.cams, id)
	}
	extra := []struct{ id int; l, sh string }{{0, "Black", "BLK"}, {1000, "Color Bars", "BARS"}, {2001, "Color 1", "COL1"}, {3010, "Media Player 1", "MP1"}, {3020, "Media Player 2", "MP2"}, {6000, "Super Source", "SSRC"}, {10010, "ME 1 PGM", "M1PG"}, {10011, "ME 1 PVW", "M1PV"}, {10020, "ME 2 PGM", "M2PG"}}
	for _, e := range extra { s.inputs[e.id] = [2]string{e.l, e.sh}; s.order = append(s.order, e.id) }
	s.pgm, s.pvw, s.ftb, s.keys = make([]int, nME), make([]int, nME), make([]bool, nME), make([]bool, nME)
	for i := 0; i < nME; i++ { s.pgm[i] = s.cams[(i*2)%len(s.cams)]; s.pvw[i] = s.cams[(i*2+1)%len(s.cams)] }
	s.aux = make([]int, nAux)
	for i := range s.aux {
		switch i % 4 { case 0: s.aux[i] = 10010; case 1: s.aux[i] = s.cams[i%len(s.cams)]; case 2: s.aux[i] = 10020; default: s.aux[i] = 3010 }
		n := fmt.Sprintf("Auxiliary %d", i+1)
		if i < len(auxNames) && auxNames[i] != "" { n = auxNames[i] }
		s.auxNames = append(s.auxNames, n)
		id := 8001 + i
		s.inputs[id] = [2]string{n, fmt.Sprintf("AX%d", i+1)}
		s.order = append(s.order, id)
	}
	s.macros = []string{"ABERTURA", "INTERVALO", "ENCERRAMENTO"}
	if stream { s.stream = 4 } else { s.stream = 0 }
	go s.recv()
	go s.animate()
}

func (s *simAtem) send(c *simAtemClient, cmds ...[]byte) {
	body := []byte{}
	flush := func() {
		c.id = (c.id + 1) & 0x7fff
		p := append(header(flAckRequest, 12+len(body), c.session, 0, c.id), body...)
		s.conn.WriteToUDP(p, c.addr)
		body = []byte{}
	}
	for _, x := range cmds {
		if len(body)+len(x) > 1300 { flush() }
		body = append(body, x...)
	}
	flush()
}

func (s *simAtem) dump() [][]byte {
	out := [][]byte{cmd("_ver", append(be16(2), be16(30)...)), cmd("_pin", fixed(s.product, 44))}
	for _, id := range s.order {
		in := s.inputs[id]
		d := append(be16(id), fixed(in[0], 20)...)
		d = append(d, fixed(in[1], 4)...)
		d = append(d, make([]byte, 10)...)
		out = append(out, cmd("InPr", d))
	}
	for i := 0; i < len(s.pgm); i++ {
		out = append(out, cmd("PrgI", []byte{byte(i), 0, byte(s.pgm[i] >> 8), byte(s.pgm[i])}))
		out = append(out, cmd("PrvI", []byte{byte(i), 0, byte(s.pvw[i] >> 8), byte(s.pvw[i]), 0, 0, 0, 0}))
		out = append(out, cmd("TrPs", []byte{byte(i), 0, 0, 0, 0, 0, 0, 0}))
		out = append(out, cmd("FtbS", []byte{byte(i), b2(s.ftb[i]), 0, 0}))
		out = append(out, cmd("KeOn", []byte{byte(i), 0, b2(s.keys[i]), 0}))
	}
	for i, a := range s.aux { out = append(out, cmd("AuxS", []byte{byte(i), 0, byte(a >> 8), byte(a)})) }
	for i := 0; i < 2; i++ { out = append(out, cmd("DskS", []byte{byte(i), b2(s.dsk[i]), 0, 0, 0, 0, 0, 0})) }
	for i, m := range s.macros {
		d := append(be16(i), 1, 0)
		d = append(d, be16(len(m))...)
		d = append(d, 0, 0)
		d = append(d, []byte(m)...)
		out = append(out, cmd("MPrp", d))
	}
	out = append(out, cmd("MRPr", []byte{0, 0, 0xff, 0xff}))
	if s.stream > 0 { out = append(out, cmd("StRS", append(be16(s.stream), 0, 0))) }
	out = append(out, s.tally())
	for _, c := range s.cams[:min(4, len(s.cams))] { out = append(out, s.ccu(c)...) }
	out = append(out, cmd("InCm", []byte{1, 0, 0, 0}))
	return out
}

// ccdp: formato assumido pelo leitor (entrada, domínio, parâmetro, tipo, … valores a partir do byte 16)
func ccdp(in, dom, feat int, vals ...int) []byte {
	d := make([]byte, 16, 24)
	d[0], d[1], d[2], d[3] = byte(in), byte(dom), byte(feat), 0x80
	for _, v := range vals { d = append(d, byte(v>>8), byte(v)) }
	for len(d) < 24 { d = append(d, 0) }
	return cmd("CCdP", d)
}

func (s *simAtem) ccu(in int) [][]byte {
	if s.ccuIris == nil { s.ccuIris = map[int]int{} }
	if _, ok := s.ccuIris[in]; !ok { s.ccuIris[in] = 900 + in*90 }
	return [][]byte{ccdp(in, 0, 3, s.ccuIris[in]), ccdp(in, 1, 1, 512<<(in%3)), ccdp(in, 1, 2, 5600), ccdp(in, 1, 5, 0, 20000), ccdp(in, 8, 0, 0, 0, 0, 0)}
}

func b2(b bool) byte { if b { return 1 }; return 0 }

func (s *simAtem) tally() []byte {
	d := be16(len(s.order))
	for _, id := range s.order {
		f := 0
		if id == s.pgm[0] { f |= 1 }
		if id == s.pvw[0] { f |= 2 }
		d = append(d, byte(id>>8), byte(id), byte(f))
	}
	return cmd("TlSr", d)
}

func (s *simAtem) broadcast(cmds ...[]byte) {
	for _, c := range s.clients { if c.ready { s.send(c, cmds...) } }
}

func (s *simAtem) recv() {
	buf := make([]byte, 2048)
	for {
		n, addr, err := s.conn.ReadFromUDP(buf)
		if err != nil { return }
		if n < 12 { continue }
		p := buf[:n]
		flags := p[0] >> 3
		s.mu.Lock()
		key := addr.String()
		c := s.clients[key]
		if flags&flHello != 0 {
			c = &simAtemClient{addr: addr, session: binary.BigEndian.Uint16(p[2:])}
			s.clients[key] = c
			reply := append(header(flHello, 20, c.session, 0, 0), 0x02, 0, 0, 0, 0, 0, 0, 0)
			s.conn.WriteToUDP(reply, addr)
			s.mu.Unlock()
			continue
		}
		if c == nil { s.mu.Unlock(); continue }
		if flags&flAck != 0 && !c.ready {
			c.ready = true
			c.session = 0x8000 | uint16(rand.Intn(0x7fff))
			s.send(c, s.dump()...)
		}
		if flags&flAckRequest != 0 {
			id := binary.BigEndian.Uint16(p[10:])
			s.conn.WriteToUDP(header(flAck, 12, c.session, id, 0), addr)
			s.handle(p[12:])
		}
		s.mu.Unlock()
	}
}

func (s *simAtem) handle(body []byte) {
	for off := 0; off+8 <= len(body); {
		l := int(binary.BigEndian.Uint16(body[off:]))
		if l < 8 || off+l > len(body) { return }
		name := string(body[off+4 : off+8])
		d := body[off+8 : off+l]
		off += l
		me := int(d[0]) % len(s.pgm)
		switch name {
		case "DCut", "DAut":
			s.pgm[me], s.pvw[me] = s.pvw[me], s.pgm[me]
			s.pushME(me)
		case "CPgI":
			s.pgm[me] = int(binary.BigEndian.Uint16(d[2:])); s.pushME(me)
		case "CPvI":
			s.pvw[me] = int(binary.BigEndian.Uint16(d[2:])); s.pushME(me)
		case "FtbA":
			s.ftb[me] = !s.ftb[me]
			s.broadcast(cmd("FtbS", []byte{byte(me), b2(s.ftb[me]), 0, 0}))
		case "CAuS":
			a := int(d[1]) % len(s.aux)
			s.aux[a] = int(binary.BigEndian.Uint16(d[2:]))
			s.broadcast(cmd("AuxS", []byte{byte(a), 0, byte(s.aux[a] >> 8), byte(s.aux[a])}))
		case "MAct":
			idx := binary.BigEndian.Uint16(d[0:])
			s.broadcast(cmd("MRPr", []byte{1, 0, byte(idx >> 8), byte(idx)}))
			go func() { time.Sleep(2 * time.Second); s.mu.Lock(); s.broadcast(cmd("MRPr", []byte{0, 0, 0xff, 0xff})); s.mu.Unlock() }()
		}
	}
}

func (s *simAtem) pushME(me int) {
	s.broadcast(cmd("PrgI", []byte{byte(me), 0, byte(s.pgm[me] >> 8), byte(s.pgm[me])}),
		cmd("PrvI", []byte{byte(me), 0, byte(s.pvw[me] >> 8), byte(s.pvw[me]), 0, 0, 0, 0}), s.tally())
}

func (s *simAtem) animate() {
	t := time.NewTicker(time.Second)
	n := 0
	for range t.C {
		n++
		s.mu.Lock()
		// keepalive como a mesa real
		for _, c := range s.clients { if c.ready { s.send(c) } }
		if n%6 == 0 {
			me := 0
			if n%18 == 0 { me = 1 }
			// transição AUTO com barra animada
			for _, pos := range []int{2500, 5000, 7500} {
				s.broadcast(cmd("TrPs", []byte{byte(me), 1, 10, 0, byte(pos >> 8), byte(pos), 0, 0}))
			}
			s.broadcast(cmd("TrPs", []byte{byte(me), 0, 0, 0, 0, 0, 0, 0}))
			s.pgm[me] = s.pvw[me]
			next := s.cams[rand.Intn(len(s.cams))]
			for next == s.pgm[me] { next = s.cams[rand.Intn(len(s.cams))] }
			s.pvw[me] = next
			s.pushME(me)
		}
		if n%9 == 0 && len(s.cams) > 3 && s.ccuIris != nil {
			// operador de CCU girando a íris da câmera 2 (vários pacotes seguidos, como um knob real)
			in := s.cams[1+(n/9)%3]
			for step := 0; step < 5; step++ { s.ccuIris[in] += 25; s.broadcast(ccdp(in, 0, 3, s.ccuIris[in])) }
			if n%27 == 0 { s.broadcast(ccdp(in, 1, 2, 4300+(n%5)*100)) }
		}
		if n%23 == 0 { s.dsk[0] = !s.dsk[0]; s.broadcast(cmd("DskS", []byte{0, b2(s.dsk[0]), 0, 0, 0, 0, 0, 0})) }
		if n%31 == 0 { s.keys[0] = !s.keys[0]; s.broadcast(cmd("KeOn", []byte{0, 0, b2(s.keys[0]), 0})) }
		if s.stream == 4 {
			sec := n
			s.broadcast(cmd("SRST", []byte{byte(sec / 3600), byte(sec / 60 % 60), byte(sec % 60), 0, 0, 0, 0, 0}))
		}
		s.mu.Unlock()
	}
}

// ================= HyperDeck =================

type simDeck struct {
	mu       sync.Mutex
	model    string
	rec      bool
	frames   int
	remain   [2]float64
	slot     int
	clip     int
	fmtVideo string
	codec    string
	conns    map[net.Conn]bool
	fmtSlot  int
	clips    []simClip
}

type simClip struct {
	name       string
	slot, dur  int
}

func runSimDeck(addr, model string, rec bool, remainMin1, remainMin2 float64) *simDeck {
	ln, err := net.Listen("tcp", addr)
	if err != nil { log.Printf("sim HyperDeck %s: %v", addr, err); return nil }
	d := &simDeck{clips: nil, model: model, rec: rec, remain: [2]float64{remainMin1 * 60, remainMin2 * 60}, slot: 1, clip: 3, fmtVideo: "1080p5994", codec: "QuickTimeProResHQ", conns: map[net.Conn]bool{}, frames: rand.Intn(100000)}
	if rec { d.clips = append(d.clips, simClip{name: fmt.Sprintf("Abertura_%04d", d.clip), slot: 1, dur: 3000}) }
	go func() {
		for range time.Tick(100 * time.Millisecond) {
			d.mu.Lock()
			if d.rec {
				d.frames += 6
				if n := len(d.clips); n > 0 { d.clips[n-1].dur += 6 }
				d.remain[d.slot-1] -= 0.1 * 4 // demonstração: consome mídia 4× mais rápido
				if d.remain[d.slot-1] <= 0 {
					d.remain[d.slot-1] = 0
					if d.slot == 1 && d.remain[1] > 0 { d.slot = 2; d.clips = append(d.clips, simClip{name: fmt.Sprintf("Continuacao_%04d", d.clip), slot: 2}) } else { d.rec = false; d.notify() }
				}
			}
			d.mu.Unlock()
		}
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil { return }
			go d.serve(c)
		}
	}()
	return d
}

func (d *simDeck) tc() string {
	f := d.frames
	return fmt.Sprintf("%02d:%02d:%02d:%02d", f/(60*60*60)%24, f/(60*60)%60, f/60%60, f%60)
}

func (d *simDeck) transport(code int) string {
	st := "stopped"
	if d.rec { st = "record" }
	return fmt.Sprintf("%d transport info:\r\nstatus: %s\r\nspeed: 0\r\nslot id: %d\r\nclip id: %d\r\nsingle clip: false\r\ndisplay timecode: %s\r\ntimecode: %s\r\nvideo format: %s\r\nloop: false\r\n\r\n", code, st, d.slot, d.clip, d.tc(), d.tc(), d.fmtVideo)
}

func (d *simDeck) notify() {
	msg := d.transport(508)
	for c := range d.conns { c.Write([]byte(msg)) }
}

func (d *simDeck) serve(c net.Conn) {
	defer c.Close()
	c.Write([]byte("500 connection info:\r\nprotocol version: 1.11\r\nmodel: " + d.model + "\r\n\r\n"))
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil { d.mu.Lock(); delete(d.conns, c); d.mu.Unlock(); return }
		line = strings.TrimSpace(line)
		d.mu.Lock()
		var out string
		switch {
		case line == "device info":
			out = "204 device info:\r\nprotocol version: 1.11\r\nmodel: " + d.model + "\r\nunique id: 7c2e0d0" + strconv.Itoa(len(d.model)) + "\r\nslot count: 2\r\nsoftware version: 8.4\r\n\r\n"
		case line == "transport info":
			out = d.transport(208)
		case strings.HasPrefix(line, "slot info"):
			id := 1
			if i := strings.LastIndex(line, ":"); i > 0 && strings.Contains(line, "slot id") { id, _ = strconv.Atoi(strings.TrimSpace(line[i+1:])) }
			if id < 1 || id > 2 { id = 1 }
			st := "mounted"
			vol := fmt.Sprintf("SSD_%d", id)
			if d.remain[id-1] < 0 { st = "empty"; vol = "" }
			rem := int(d.remain[id-1])
			if rem < 0 { rem = 0 }
			out = fmt.Sprintf("202 slot info:\r\nslot id: %d\r\nstatus: %s\r\nvolume name: %s\r\nrecording time: %d\r\nvideo format: %s\r\n\r\n", id, st, vol, rem, d.fmtVideo)
		case line == "configuration":
			out = "211 configuration:\r\naudio input: embedded\r\nvideo input: SDI\r\nfile format: " + d.codec + "\r\n\r\n"
		case strings.HasPrefix(line, "notify"):
			d.conns[c] = true
			out = "200 ok\r\n"
		case strings.HasPrefix(line, "disk list"):
			id := d.slot
			if i := strings.LastIndex(line, ":"); i > 0 && strings.Contains(line, "slot id") { id, _ = strconv.Atoi(strings.TrimSpace(line[i+1:])) }
			var b strings.Builder
			fmt.Fprintf(&b, "206 disk list:\r\nslot id: %d\r\n", id)
			for i, c := range d.clips {
				if c.slot != id { continue }
				f := c.dur
				fmt.Fprintf(&b, "%d: %s.mov %s %s %02d:%02d:%02d:%02d\r\n", i+1, c.name, d.codec, d.fmtVideo, f/216000, f/3600%60, f/60%60, f%60)
			}
			b.WriteString("\r\n")
			out = b.String()
		case strings.HasPrefix(line, "record"):
			if !d.rec {
				d.rec = true; d.clip++
				nm := fmt.Sprintf("%s_%04d", strings.ReplaceAll(d.model, " ", "_"), d.clip)
				if i := strings.Index(line, "name:"); i >= 0 { nm = strings.TrimSpace(line[i+5:]) + fmt.Sprintf("_%04d", d.clip) }
				d.clips = append(d.clips, simClip{name: nm, slot: d.slot})
			}
			out = "200 ok\r\n"
			d.notify()
		case line == "stop":
			d.rec = false
			out = "200 ok\r\n"
			d.notify()
		case strings.HasPrefix(line, "format:") && strings.Contains(line, "prepare"):
			out = "216 format ready:\r\nFMT7731\r\n\r\n"
			if i := strings.Index(line, "slot id:"); i >= 0 { fmt.Sscanf(line[i+8:], "%d", &d.fmtSlot) } else { d.fmtSlot = d.slot }
		case strings.HasPrefix(line, "format: confirm: FMT7731"):
			if d.rec { out = "105 disk full\r\n" } else {
				s := d.fmtSlot
				if s < 1 || s > 2 { s = 1 }
				d.remain[s-1] = 240 * 60
				out = "200 ok\r\n"
			}
		case line == "ping":
			out = "200 ok\r\n"
		default:
			out = "100 syntax error\r\n"
		}
		d.mu.Unlock()
		c.Write([]byte(out))
	}
}

// ================= VideoHub =================

type simHub struct {
	mu     sync.Mutex
	model  string
	n      int
	ins    []string
	outs   []string
	routes []int
	conns  map[net.Conn]bool
}

func runSimHub(addr, model string, n int, ins, outs []string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil { log.Printf("sim VideoHub %s: %v", addr, err); return }
	h := &simHub{model: model, n: n, conns: map[net.Conn]bool{}}
	for i := 0; i < n; i++ {
		in := fmt.Sprintf("SRC %02d", i+1)
		if i < len(ins) { in = ins[i] }
		out := fmt.Sprintf("DEST %02d", i+1)
		if i < len(outs) { out = outs[i] }
		h.ins = append(h.ins, in); h.outs = append(h.outs, out); h.routes = append(h.routes, i%len(ins))
	}
	go func() {
		for range time.Tick(17 * time.Second) {
			h.mu.Lock()
			o := 4 + rand.Intn(n-4)
			h.routes[o] = rand.Intn(n)
			h.push(fmt.Sprintf("VIDEO OUTPUT ROUTING:\n%d %d\n\n", o, h.routes[o]))
			h.mu.Unlock()
		}
	}()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil { return }
			go h.serve(c)
		}
	}()
}

func (h *simHub) push(s string) { for c := range h.conns { c.Write([]byte(s)) } }

func (h *simHub) serve(c net.Conn) {
	defer c.Close()
	h.mu.Lock()
	var b strings.Builder
	b.WriteString("PROTOCOL PREAMBLE:\nVersion: 2.8\n\n")
	fmt.Fprintf(&b, "VIDEOHUB DEVICE:\nDevice present: true\nModel name: %s\nFriendly name: %s\nUnique ID: 7C2E0D02\nVideo inputs: %d\nVideo processing units: 0\nVideo outputs: %d\nVideo monitoring outputs: 0\nSerial ports: 0\n\n", h.model, h.model, h.n, h.n)
	b.WriteString("INPUT LABELS:\n")
	for i, l := range h.ins { fmt.Fprintf(&b, "%d %s\n", i, l) }
	b.WriteString("\nOUTPUT LABELS:\n")
	for i, l := range h.outs { fmt.Fprintf(&b, "%d %s\n", i, l) }
	b.WriteString("\nVIDEO OUTPUT LOCKS:\n")
	for i := range h.outs { fmt.Fprintf(&b, "%d U\n", i) }
	b.WriteString("\nVIDEO OUTPUT ROUTING:\n")
	for i, r := range h.routes { fmt.Fprintf(&b, "%d %d\n", i, r) }
	b.WriteString("\nEND PRELUDE:\n\n")
	c.Write([]byte(b.String()))
	h.conns[c] = true
	h.mu.Unlock()
	r := bufio.NewReader(c)
	var block []string
	for {
		line, err := r.ReadString('\n')
		if err != nil { h.mu.Lock(); delete(h.conns, c); h.mu.Unlock(); return }
		line = strings.TrimRight(line, "\r\n")
		if line != "" { block = append(block, line); continue }
		if len(block) == 0 { continue }
		h.mu.Lock()
		switch strings.ToUpper(block[0]) {
		case "PING:":
			c.Write([]byte("ACK\n\n"))
		case "VIDEO OUTPUT ROUTING:":
			c.Write([]byte("ACK\n\n"))
			var upd strings.Builder
			upd.WriteString("VIDEO OUTPUT ROUTING:\n")
			for _, l := range block[1:] {
				var o, i int
				if _, err := fmt.Sscanf(l, "%d %d", &o, &i); err == nil && o >= 0 && o < h.n && i >= 0 && i < h.n {
					h.routes[o] = i
					fmt.Fprintf(&upd, "%d %d\n", o, i)
				}
			}
			upd.WriteString("\n")
			h.push(upd.String())
		default:
			c.Write([]byte("NAK\n\n"))
		}
		h.mu.Unlock()
		block = nil
	}
}

// ================= cenário de demonstração =================

func startDemo(c *Config, fresh bool) {
	cams := []string{"CAM 1 PALCO", "CAM 2 PLATEIA", "CAM 3 GRUA", "CAM 4 STEADY", "CAM 5 PTZ", "CAM 6 BACKSTAGE", "VT PLAYOUT", "GC VMIX"}
	// Constellation 8K: 40 entradas, 4 M/E, 24 AUX (nomes de saída como vêm da mesa)
	big := append([]string{}, cams...)
	for i := len(big); i < 40; i++ { big = append(big, fmt.Sprintf("INPUT %d", i+1)) }
	runSimAtem("127.0.0.1:19910", "ATEM Constellation 8K", big, false, 4, 24,
		[]string{"PGM ENCODER", "VIDEO MAN", "TELÃO ESQ", "TELÃO DIR", "RETORNO PALCO", "", "GRAVAÇÃO GRUA", "MON DIRETOR", "", "", "REPLAY IN", "", "", "", "", "", "", "", "", "", "", "", "MV EXTRA", "TESTE"})
	runSimAtem("127.0.0.1:19911", "ATEM Television Studio HD8", []string{"CORTE ATEM 1", "CAM 7 DRONE", "CAM 8 ENTREVISTA", "REPLAY", "TELÃO"}, true, 2, 4, nil)
	ins := []string{"ATEM1 PGM", "ATEM1 CLEAN", "ATEM2 PGM", "CAM 1", "CAM 2", "CAM 3 GRUA", "CAM 4", "CAM 5", "REPLAY", "GC", "VT", "TELÃO", "RETORNO SAT", "MV ATEM1", "MV ATEM2", "BARS"}
	outs := []string{"HD PGM SUJO", "HD SUJO BKP", "HD LIMPO", "HD GRUA", "ENCODER 1", "ENCODER 2", "TELÃO", "MON DIRETOR", "MON DTV", "MON ÁUDIO", "MON REPLAY", "MON GCS"}
	runSimHub("127.0.0.1:19990", "Blackmagic Videohub 40x40 12G", 40, ins, outs)
	runSimHub("127.0.0.1:19991", "Smart Videohub CleanSwitch 12x12", 12, ins[:12], outs)
	runSimDeck("127.0.0.1:19993", "HyperDeck Studio 4K Pro", true, 182, 240)
	runSimDeck("127.0.0.1:19994", "HyperDeck Studio 4K Pro", true, 175, 240)
	runSimDeck("127.0.0.1:19995", "HyperDeck Studio HD Plus", true, 12, -1)
	runSimDeck("127.0.0.1:19996", "HyperDeck Studio HD Plus", false, 300, -1)
	runSimDeck("127.0.0.1:19997", "HyperDeck Studio HD Mini", true, 7, 120)
	if ln, err := net.Listen("tcp", "127.0.0.1:19554"); err == nil {
		go func() { for { c, err := ln.Accept(); if err != nil { return }; c.Close() } }()
	}
	runSimWebPresenter("127.0.0.1:19977")
	runSimCamera("127.0.0.1:19080")
	runSimSNMP("127.0.0.1:19161", "ups")
	runSimSNMP("127.0.0.1:19162", "switch")
	if !fresh { return }
	c.Devices = nil
	add := func(name, kind, host string, port int, group string) string {
		id := uniqueID(c.Devices, name)
		c.Devices = append(c.Devices, Device{ID: id, Name: name, Kind: kind, Host: host, Port: port, Enabled: true, Group: group})
		return id
	}
	add("ATEM 1 - Constellation 8K", "atem", "127.0.0.1", 19910, "Switchers")
	add("ATEM 2 - Studio HD8", "atem", "127.0.0.1", 19911, "Switchers")
	vh := add("VideoHub - Matriz 1 40x40", "videohub", "127.0.0.1", 19990, "Matrizes")
	add("Smart VideoHub CleanSwitch 12x12", "videohub", "127.0.0.1", 19991, "Matrizes")
	h1 := add("HyperDeck - PGM SUJO", "hyperdeck", "127.0.0.1", 19993, "Gravação")
	h2 := add("HyperDeck - SUJO BKP", "hyperdeck", "127.0.0.1", 19994, "Gravação")
	h3 := add("HyperDeck - PRODUZIDO LIMPO", "hyperdeck", "127.0.0.1", 19995, "Gravação")
	add("HyperDeck - PGM LIMPO", "hyperdeck", "127.0.0.1", 19996, "Gravação")
	h5 := add("HyperDeck - GRUA", "hyperdeck", "127.0.0.1", 19997, "Gravação")
	add("Encoder de streaming", "tcp", "127.0.0.1", 19554, "Rede")
	add("Câmera PTZ 9 (desligada)", "tcp", "127.0.0.1", 19555, "Rede")
	add("Web Presenter HD (YouTube)", "webpresenter", "127.0.0.1", 19977, "Streaming")
	cam := add("Câmera URSA Broadcast G2 · CAM 3", "bmcamera", "127.0.0.1", 19080, "Câmeras")
	add("Nobreak do rack", "ups", "127.0.0.1", 19161, "Energia e rede")
	add("Switch do rack", "switch", "127.0.0.1", 19162, "Energia e rede")
	for i := range c.Devices { if c.Devices[i].Kind == "switch" { c.Devices[i].Ports = "1-8" } }
	_ = cam
	add("ATEM 2 M/E Advanced Panel 20", "ping", "127.0.0.2", 0, "Painéis")
	add("ATEM Camera Control Panel", "ping", "127.0.0.3", 0, "Painéis")
	c.Pairs = []RecPair{{Primary: h1, Backup: h2, Label: "PGM SUJO principal/backup"}}
	c.RecWatch = []string{h1, h2, h3, h5}
	c.Protected = []ProtectedRoute{{Device: vh, Output: 0, Expected: 0}, {Device: vh, Output: 1, Expected: 1}}
	c.ControlEnabled = true
	c.TSL = []TSLTarget{{Enabled: true, Label: "Multiviewer / UMD (exemplo)", Host: "127.0.0.1", Port: 19800, Proto: "udp5", Atem: "atem_1_constellation_8k"}}
	c.Schedule = []ScheduleItem{{Time: "20:30", Action: "checklist", Label: "pré-show"}}
	c.StationName = "DEMO · " + c.StationName
}
