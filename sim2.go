package main

// Simuladores do modo demonstração: Web Presenter, câmera Blackmagic (REST),
// nobreak e switch (SNMP).

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

func runSimWebPresenter(addr string) {
	ln, err := net.Listen("tcp", addr)
	if err != nil { log.Printf("sim Web Presenter: %v", err); return }
	start := time.Now()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil { return }
			go func(c net.Conn) {
				defer c.Close()
				c.Write([]byte("PROTOCOL PREAMBLE:\nVersion: 1.2\n\nIDENTITY:\nModel: Blackmagic Web Presenter HD\nLabel: Web Presenter HD\nUnique ID: 7c2e0d0aa1\n\nSTREAM SETTINGS:\nVideo Mode: 1080p59.94\nCurrent Platform: YouTube\nCurrent Server: Primary\nCurrent Quality: Streaming High\n\nSTREAM STATE:\nStatus: Streaming\n\n"))
				r := bufio.NewReader(c)
				for {
					line, err := r.ReadString('\n')
					if err != nil { return }
					if strings.TrimSpace(line) == "STREAM STATE:" {
						el := int(time.Since(start).Seconds())
						cache := 0
						if el%80 > 60 && el%80 < 72 { cache = 64 } // rede ruim de vez em quando
						fmt.Fprintf(c, "STREAM STATE:\nStatus: Streaming\nBitrate: %d\nDuration: %02d:%02d:%02d:00\nCache Used: %d\n\n", 5800000+el%7*30000, el/3600, el/60%60, el%60, cache)
					}
				}
			}(c)
		}
	}()
}

func runSimCamera(addr string) {
	var mu sync.Mutex
	rec, frames, remain := true, 3600*60*2, 47*60
	go func() {
		for range time.Tick(time.Second) { mu.Lock(); if rec { frames += 60; remain -= 4; if remain < 0 { remain = 0 } }; mu.Unlock() }
	}()
	m := http.NewServeMux()
	j := func(w http.ResponseWriter, v any) { w.Header().Set("Content-Type", "application/json"); json.NewEncoder(w).Encode(v) }
	m.HandleFunc("/control/api/v1/transports/0/record", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); defer mu.Unlock()
		if r.Method == "PUT" {
			var b struct{ Recording bool `json:"recording"` }
			json.NewDecoder(r.Body).Decode(&b)
			rec = b.Recording
			w.WriteHeader(204); return
		}
		j(w, map[string]any{"recording": rec})
	})
	m.HandleFunc("/control/api/v1/transports/0/timecode", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); f := frames; mu.Unlock()
		j(w, map[string]any{"timecode": fmt.Sprintf("%02d:%02d:%02d:%02d", f/216000%24, f/3600%60, f/60%60, f%60)})
	})
	m.HandleFunc("/control/api/v1/media/workingset", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock(); rm := remain; mu.Unlock()
		j(w, map[string]any{"workingset": []map[string]any{
			{"index": 0, "activeDisk": true, "volume": "CFAST_CAM3", "deviceName": "cfast1", "remainingRecordTime": rm, "clipCount": 4},
			{"index": 1, "activeDisk": false, "volume": "CFAST_B", "deviceName": "cfast2", "remainingRecordTime": 7200, "clipCount": 0}}})
	})
	m.HandleFunc("/control/api/v1/system/product", func(w http.ResponseWriter, r *http.Request) {
		j(w, map[string]any{"productName": "Blackmagic URSA Broadcast G2", "deviceName": "URSA-CAM3"})
	})
	go http.ListenAndServe(addr, m)
}

func runSimSNMP(addr string, kind string) {
	ua, _ := net.ResolveUDPAddr("udp", addr)
	c, err := net.ListenUDP("udp", ua)
	if err != nil { log.Printf("sim SNMP: %v", err); return }
	start := time.Now()
	vals := func() map[string]any {
		el := int(time.Since(start).Seconds())
		v := map[string]any{"1.3.6.1.2.1.1.1.0": "Simulated " + kind, "1.3.6.1.2.1.1.5.0": map[string]string{"ups": "NOBREAK-RACK", "switch": "SW-RACK-01"}[kind]}
		if kind == "ups" {
			onBat := el > 100 && el < 125
			src, sec, charge := int64(3), int64(0), int64(100)
			if onBat { src, sec, charge = 5, int64(el-100), int64(100-(el-100)) }
			u := "1.3.6.1.2.1.33.1."
			v[u+"1.1.0"], v[u+"1.2.0"] = "SMS", "Sinus Double 3kVA"
			v[u+"4.1.0"], v[u+"2.1.0"], v[u+"2.2.0"], v[u+"2.3.0"], v[u+"2.4.0"], v[u+"4.4.1.5.1"] = src, int64(2), sec, int64(38), charge, int64(46)
		} else {
			names := []string{"", "ATEM 1 CONSTELLATION", "ATEM 2 HD8", "HYPERDECKS", "VIDEOHUBS", "PAINEL 2ME", "CAMERA CONTROL PANEL", "WEB PRESENTER", "UPLINK"}
			for i := 1; i <= 8; i++ {
				s := fmt.Sprint(i)
				st := int64(1)
				if i == 5 && el%90 > 70 && el%90 < 82 { st = 2 }
				v["1.3.6.1.2.1.2.2.1.2."+s] = "GigabitEthernet1/0/" + s
				v["1.3.6.1.2.1.2.2.1.8."+s] = st
				v["1.3.6.1.2.1.2.2.1.14."+s] = int64(map[bool]int{true: el / 30, false: 0}[i == 3])
				v["1.3.6.1.2.1.31.1.1.1.18."+s] = names[i]
				v["1.3.6.1.2.1.31.1.1.1.15."+s] = int64(1000)
			}
		}
		return v
	}
	go func() {
		buf := make([]byte, 65535)
		for {
			n, ra, err := c.ReadFromUDP(buf)
			if err != nil { return }
			cm, _, id, order, _, err := snmpParse(buf[:n])
			if err != nil || cm != "public" { continue }
			v := vals()
			var vbs [][2][]byte
			for _, o := range order {
				e, _ := berOID(o)
				val := []byte{0x81, 0x00}
				switch x := v[o].(type) {
				case int64: val = berInt(x)
				case string: val = tlv(0x04, []byte(x))
				}
				vbs = append(vbs, [2][]byte{e, val})
			}
			c.WriteToUDP(snmpMsg(cm, 0xA2, id, vbs), ra)
		}
	}()
}
