package main

// CÂMERAS BLACKMAGIC via Camera Control REST API (http://IP/control/api/v1/).
// Compatíveis: Cinema Camera 6K, URSA Broadcast G2, Micro Studio Camera 4K G2,
// Studio Camera 4K Plus/Pro e mais novas (com o REST habilitado no Camera Setup).
// O estado é colocado no mesmo formato de um gravador: assim a vigilância de
// REC, mídia acabando, timecode parado, troca de slot e relatórios valem também
// para a gravação interna das câmeras.

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

var camHTTP = &http.Client{Timeout: 2500 * time.Millisecond, Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}

type camClient struct {
	h    *Hub
	dev  Device
	base string
	mu   sync.Mutex
}

var camClients sync.Map

func (c *camClient) req(method, path string, body any, out any) error {
	var rd io.Reader
	if body != nil { b, _ := json.Marshal(body); rd = bytes.NewReader(b) }
	bases := []string{c.base}
	if c.base == "" { bases = []string{"http://" + hostPort(c.dev), "https://" + c.dev.Host} }
	var last error
	for _, b := range bases {
		r, _ := http.NewRequest(method, b+"/control/api/v1"+path, rd)
		if body != nil { r.Header.Set("Content-Type", "application/json") }
		resp, err := camHTTP.Do(r)
		if err != nil { last = err; continue }
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode == 404 { return fmt.Errorf("não suportado por esta câmera (%s)", path) }
		if resp.StatusCode >= 300 { return fmt.Errorf("HTTP %d em %s", resp.StatusCode, path) }
		c.mu.Lock(); c.base = b; c.mu.Unlock()
		if out != nil && len(data) > 0 { return json.Unmarshal(data, out) }
		return nil
	}
	return last
}

func hostPort(d Device) string {
	if d.Port == 0 || d.Port == 80 { return d.Host }
	return fmt.Sprintf("%s:%d", d.Host, d.Port)
}

// bcdTC: a API pode devolver o TC como texto ou como número BCD (0xHHMMSSFF).
func bcdTC(v any) string {
	switch x := v.(type) {
	case string: return x
	case float64:
		n := uint32(x)
		return fmt.Sprintf("%02x:%02x:%02x:%02x", (n>>24)&0x3f, (n>>16)&0xff, (n>>8)&0xff, n&0xff)
	}
	return ""
}

func startBMCamera(h *Hub, d Device) func() {
	c := &camClient{h: h, dev: d}
	camClients.Store(d.ID, c)
	stop := make(chan struct{})
	go func() {
		fails := 0
		info := false
		for {
			t0 := time.Now()
			var rec struct{ Recording bool `json:"recording"` }
			err := c.req("GET", "/transports/0/record", nil, &rec)
			if err != nil {
				fails++
				if fails >= 2 { h.SetOnline(d.ID, false, "REST da câmera não responde: "+errText(err)) }
			} else {
				fails = 0
				lat := float64(time.Since(t0).Microseconds()) / 1000
				var tc map[string]any
				_ = c.req("GET", "/transports/0/timecode", nil, &tc)
				var ws struct {
					Workingset []struct {
						Index               int    `json:"index"`
						ActiveDisk          bool   `json:"activeDisk"`
						Volume              string `json:"volume"`
						DeviceName          string `json:"deviceName"`
						RemainingRecordTime int    `json:"remainingRecordTime"`
						ClipCount           int    `json:"clipCount"`
					} `json:"workingset"`
				}
				_ = c.req("GET", "/media/workingset", nil, &ws)
				var prod map[string]any
				if !info { _ = c.req("GET", "/system/product", nil, &prod); info = true }
				deck := &DeckState{}
				h.Update(d.ID, func(s *DevState) {
					if s.Deck == nil { s.Deck = &DeckState{Slots: map[int]*DeckSlot{}, Model: "Câmera Blackmagic"} }
					deck = s.Deck
					s.LatencyMs = lat
					if prod != nil { if n, _ := prod["productName"].(string); n != "" { deck.Model = n } }
				})
				st := "stopped"
				if rec.Recording { st = "record" }
				blk := block{code: 508, props: map[string]string{"status": st}}
				if tc != nil { if t := bcdTC(tc["timecode"]); t != "" { blk.props["display timecode"] = t } }
				for _, w := range ws.Workingset {
					if w.ActiveDisk { blk.props["slot id"] = fmt.Sprint(w.Index + 1) }
				}
				dc := &deckClient{h: h, dev: d}
				h.SetOnline(d.ID, true, "")
				dc.apply(blk)
				for _, w := range ws.Workingset {
					status := "mounted"
					if w.Volume == "" && w.DeviceName == "" { status = "empty" }
					dc.apply(block{code: 502, props: map[string]string{"slot id": fmt.Sprint(w.Index + 1), "status": status, "volume name": nz(w.Volume, w.DeviceName), "recording time": fmt.Sprint(w.RemainingRecordTime)}})
				}
				_ = deck
			}
			select { case <-stop: return; case <-time.After(2 * time.Second): }
		}
	}()
	return func() { close(stop); camClients.Delete(d.ID) }
}

func camCommand(id, action, name string) error {
	v, ok := camClients.Load(id)
	if !ok { return fmt.Errorf("câmera não encontrada") }
	c := v.(*camClient)
	body := map[string]any{"recording": action == "record"}
	if action == "record" && strings.TrimSpace(name) != "" { body["clipName"] = strings.TrimSpace(name) }
	return c.req("PUT", "/transports/0/record", body, nil)
}
