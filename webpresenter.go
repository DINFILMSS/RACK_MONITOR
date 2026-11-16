package main

// ENCODER DE STREAMING — Blackmagic Web Presenter / Streaming Encoder
// Web Presenter Ethernet Protocol (TCP 9977, texto em blocos). O status é
// enviado pelo aparelho quando muda; bitrate, duração e cache precisam ser
// consultados (o monitor pede "STREAM STATE:" a cada 2 s).

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

type StreamState struct {
	Model     string `json:"model"`
	Status    string `json:"status"`
	Bitrate   int    `json:"bitrate"`
	Duration  string `json:"duration"`
	CacheUsed int    `json:"cacheUsed"`
	Platform  string `json:"platform"`
	Video     string `json:"video"`
	Since     time.Time `json:"since"`
}

func startWebPresenter(h *Hub, d Device) func() {
	stop := make(chan struct{})
	var conn net.Conn
	go func() {
		backoff := time.Second
		for {
			select { case <-stop: return; default: }
			err := func() error {
				t0 := time.Now()
				c, err := net.DialTimeout("tcp", net.JoinHostPort(d.Host, strconv.Itoa(d.Port)), 2*time.Second)
				if err != nil { return err }
				conn = c
				defer c.Close()
				lat := float64(time.Since(t0).Microseconds()) / 1000
				h.Update(d.ID, func(s *DevState) { s.LatencyMs = lat; if s.Stream == nil { s.Stream = &StreamState{} } })
				type blk struct{ title string; kv map[string]string }
				ch := make(chan blk, 32)
				rerr := make(chan error, 1)
				go func() {
					r := bufio.NewReader(c)
					var cur *blk
					for {
						c.SetReadDeadline(time.Now().Add(10 * time.Second))
						line, err := r.ReadString('\n')
						if err != nil { rerr <- err; return }
						line = strings.TrimRight(line, "\r\n")
						if cur == nil {
							if line == "" { continue }
							if line == "ACK" || line == "NAK" { ch <- blk{title: line}; continue }
							if strings.HasSuffix(line, ":") { cur = &blk{title: strings.ToUpper(strings.TrimSuffix(line, ":")), kv: map[string]string{}} }
							continue
						}
						if line == "" { ch <- *cur; cur = nil; continue }
						if k, v, ok := strings.Cut(line, ": "); ok { cur.kv[strings.ToLower(k)] = v }
					}
				}()
				tick := time.NewTicker(2 * time.Second)
				defer tick.Stop()
				lastRx := time.Now()
				online := false
				for {
					select {
					case <-stop: return nil
					case err := <-rerr: return err
					case <-tick.C:
						if time.Since(lastRx) > 8*time.Second { return fmt.Errorf("encoder parou de responder") }
						c.SetWriteDeadline(time.Now().Add(2 * time.Second))
						c.Write([]byte("STREAM STATE:\n\n"))
					case b := <-ch:
						lastRx = time.Now()
						if diagOn.Load() { diagWrite(d.ID, "WEBP", fmt.Sprintf("%s %v", b.title, b.kv)) }
						if !online { online = true; h.SetOnline(d.ID, true, "") }
						var logs [][3]string
						h.Update(d.ID, func(s *DevState) {
							st := s.Stream
							if st == nil { st = &StreamState{}; s.Stream = st }
							s.LastSeen = time.Now()
							switch b.title {
							case "IDENTITY":
								st.Model = nz(b.kv["model"], b.kv["label"])
								s.Detail = st.Model
							case "STREAM SETTINGS":
								if v := b.kv["current platform"]; v != "" { st.Platform = v }
								if v := b.kv["video mode"]; v != "" { st.Video = v }
							case "STREAM STATE":
								if v, ok := b.kv["status"]; ok && v != st.Status {
									if st.Status != "" { logs = append(logs, [3]string{"STREAM", "info", "Encoder: " + st.Status + " → " + v}) }
									st.Status = v; st.Since = time.Now()
								}
								if v, err := strconv.Atoi(b.kv["bitrate"]); err == nil { st.Bitrate = v }
								if v, ok := b.kv["duration"]; ok { st.Duration = v }
								if v, err := strconv.Atoi(strings.TrimSuffix(b.kv["cache used"], "%")); err == nil { st.CacheUsed = v }
							}
						})
						for _, l := range logs { h.LogO("externo", d.ID, d.Name, l[0], l[1], l[2], nil) }
					}
				}
			}()
			select { case <-stop: return; default: }
			h.SetOnline(d.ID, false, errText(err))
			select { case <-stop: return; case <-time.After(backoff): }
			if backoff < 5*time.Second { backoff += time.Second }
		}
	}()
	return func() { close(stop); if conn != nil { conn.Close() } }
}
