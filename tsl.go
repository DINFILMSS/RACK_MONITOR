package main

// SAÍDA TSL (tally + nomes) para multiviewers, UMDs e luzes de tally.
// TSL UMD 3.1 (UDP) e TSL 5.0 (UDP ou TCP). Tally 1/vermelho = PROGRAM,
// tally 2/verde = PREVIEW, texto = nome da fonte vindo da ATEM.

import (
	"encoding/binary"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

type TSLTarget struct {
	Enabled bool   `json:"enabled"`
	Label   string `json:"label"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Proto   string `json:"proto"` // udp31 | udp5 | tcp5
	Atem    string `json:"atem"`
	ME      int    `json:"me"`
	Offset  int    `json:"offset"` // endereço/índice = número da fonte + offset
	Screen  int    `json:"screen"`
	Short   bool   `json:"short"`
}

type tslItem struct {
	idx   int
	text  string
	pgm   bool
	pvw   bool
}

var accentRep = strings.NewReplacer("á", "a", "à", "a", "ã", "a", "â", "a", "ä", "a", "é", "e", "ê", "e", "è", "e", "í", "i", "ì", "i", "ó", "o", "ô", "o", "õ", "o", "ò", "o", "ö", "o", "ú", "u", "ù", "u", "ü", "u", "ç", "c", "ñ", "n",
	"Á", "A", "À", "A", "Ã", "A", "Â", "A", "Ä", "A", "É", "E", "Ê", "E", "È", "E", "Í", "I", "Ó", "O", "Ô", "O", "Õ", "O", "Ö", "O", "Ú", "U", "Ü", "U", "Ç", "C", "Ñ", "N")

func asciiOnly(s string) string {
	var b strings.Builder
	for _, r := range accentRep.Replace(s) {
		if r < 32 || r > 126 { r = '?' }
		b.WriteRune(r)
	}
	return b.String()
}

func tsl31(it tslItem) []byte {
	p := make([]byte, 18)
	p[0] = 0x80 + byte(it.idx&0x7f)
	c := byte(0x30)
	if it.pgm { c |= 1 }
	if it.pvw { c |= 2 }
	p[1] = c
	txt := asciiOnly(it.text)
	if len(txt) > 16 { txt = txt[:16] }
	copy(p[2:], fmt.Sprintf("%-16s", txt))
	return p
}

func tsl5(screen int, items []tslItem) []byte {
	body := []byte{0, 0} // VER, FLAGS
	body = binary.LittleEndian.AppendUint16(body, uint16(screen))
	for _, it := range items {
		t := 0
		if it.pvw { t = 2 }
		if it.pgm { t = 1 }
		ctrl := uint16(t | t<<2 | t<<4 | 3<<6)
		txt := asciiOnly(it.text)
		body = binary.LittleEndian.AppendUint16(body, uint16(it.idx))
		body = binary.LittleEndian.AppendUint16(body, ctrl)
		body = binary.LittleEndian.AppendUint16(body, uint16(len(txt)))
		body = append(body, txt...)
	}
	return append(binary.LittleEndian.AppendUint16(nil, uint16(len(body))), body...)
}

// TSL 5.0 sobre TCP: DLE/STX + "byte stuffing" do DLE.
func tsl5tcp(pkt []byte) []byte {
	out := []byte{0xFE, 0x02}
	for _, b := range pkt { out = append(out, b); if b == 0xFE { out = append(out, 0xFE) } }
	return out
}

func (h *Hub) tslItems(t TSLTarget) []tslItem {
	h.mu.Lock(); defer h.mu.Unlock()
	s := h.states[t.Atem]
	if s == nil || s.Atem == nil || !s.Online { return nil }
	a := s.Atem
	m := a.MEs[t.ME]
	var ids []int
	for id := range a.Inputs { if id > 0 && id < 1000 { ids = append(ids, id) } }
	sort.Ints(ids)
	var out []tslItem
	for _, id := range ids {
		in := a.Inputs[id]
		txt := in.Long
		if t.Short { txt = in.Short }
		it := tslItem{idx: id + t.Offset, text: txt}
		if m != nil { it.pgm, it.pvw = m.Program == id, m.Preview == id }
		if f, ok := a.Tally[id]; ok && t.ME == 0 { it.pgm, it.pvw = it.pgm || f&1 != 0, it.pvw || f&2 != 0 }
		if it.idx >= 0 { out = append(out, it) }
	}
	return out
}

func (h *Hub) tslLoop() {
	type conn struct{ c net.Conn; key string }
	conns := map[int]*conn{}
	last := map[int]string{}
	lastFull := map[int]time.Time{}
	for range time.Tick(150 * time.Millisecond) {
		cfgMu.RLock()
		targets := append([]TSLTarget(nil), h.cfg.TSL...)
		cfgMu.RUnlock()
		for i, t := range targets {
			if !t.Enabled || t.Host == "" || t.Port == 0 { if c := conns[i]; c != nil { c.c.Close(); delete(conns, i) }; continue }
			items := h.tslItems(t)
			if items == nil { continue }
			sig := fmt.Sprint(items)
			if sig == last[i] && time.Since(lastFull[i]) < 2*time.Second { continue }
			last[i], lastFull[i] = sig, time.Now()
			network := "udp"
			if t.Proto == "tcp5" { network = "tcp" }
			key := network + "|" + net.JoinHostPort(t.Host, strconv.Itoa(t.Port))
			c := conns[i]
			if c == nil || c.key != key {
				if c != nil { c.c.Close() }
				nc, err := net.DialTimeout(network, net.JoinHostPort(t.Host, strconv.Itoa(t.Port)), time.Second)
				if err != nil { delete(conns, i); continue }
				c = &conn{nc, key}; conns[i] = c
			}
			c.c.SetWriteDeadline(time.Now().Add(time.Second))
			var err error
			switch t.Proto {
			case "udp31":
				for _, it := range items { if it.idx <= 126 { if _, err = c.c.Write(tsl31(it)); err != nil { break } } }
			default:
				for k := 0; k < len(items); k += 40 { // pacotes até ~2 KB
					p := tsl5(t.Screen, items[k:min(k+40, len(items))])
					if t.Proto == "tcp5" { p = tsl5tcp(p) }
					if _, err = c.c.Write(p); err != nil { break }
				}
			}
			if err != nil { c.c.Close(); delete(conns, i) }
		}
	}
}
