package main

// API HTTP + SSE. Todas as telas (PC do rack, tablet, celular de tally, TV do
// rack) recebem o mesmo estado ao vivo por /sse.

import (
	"strconv"
	"os"
	"path/filepath"
	"sort"
	"context"
	"sync"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"time"
)

func (h *Hub) routes(static fs.FS) *http.ServeMux {
	m := http.NewServeMux()
	m.HandleFunc("GET /sse", h.sse)
	m.HandleFunc("GET /api/state", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(h.Snapshot())
	})
	m.HandleFunc("GET /api/whoami", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"local": isLocal(r), "canControl": h.canControl(r) == nil, "addr": r.RemoteAddr, "version": AppVersion})
	})
	m.HandleFunc("GET /api/events", func(w http.ResponseWriter, r *http.Request) {
		min := r.URL.Query().Get("min")
		if min == "" { min = "trace" }
		writeJSON(w, h.RecentEvents(1500, min))
	})
	m.HandleFunc("GET /api/companion", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, h.companionVars()) })
	m.HandleFunc("GET /api/companion/test", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, compTest(r.URL.Query().Get("label"))) })
	m.HandleFunc("GET /api/companion.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		vars := h.companionVars()
		keys := sortedKeys(vars)
		for _, k := range keys { fmt.Fprintf(w, "%s\t%s\n", k, vars[k]) }
	})
	m.HandleFunc("POST /api/alerts/ack", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Key string `json:"key"` }
		if !readJSON(w, r, &b) { return }
		if b.Key == "" { b.Key = "*" }
		h.Ack(b.Key)
		h.LogO(originOf(r), "", "RACK MONITOR", "ALERTA RECONHECIDO", "info", "Operador reconheceu "+map[bool]string{true: "todos os alertas", false: b.Key}[b.Key == "*"], nil)
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/recwatch", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Armed bool `json:"armed"` }
		if !readJSON(w, r, &b) { return }
		cfgMu.RLock(); n := len(h.cfg.RecWatch); cfgMu.RUnlock()
		if b.Armed && n == 0 { httpErr(w, 400, "Selecione em CONFIGURAÇÃO quais gravadores devem estar em REC."); return }
		h.mu.Lock(); h.recArmed = b.Armed; h.recArmedAt = time.Now(); h.dirty = true; h.mu.Unlock()
		if b.Armed {
			h.LogO(originOf(r), "", "RACK MONITOR", "VIGILÂNCIA ARMADA", "warn", fmt.Sprintf("%d gravadores devem permanecer em REC.", n), nil)
		} else {
			h.LogO(originOf(r), "", "RACK MONITOR", "VIGILÂNCIA DESARMADA", "info", "Vigilância de gravação desligada.", nil)
		}
		writeJSON(w, ok())
	})
	// ---- configuração ----
	m.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) {
		cfgMu.RLock(); c := *h.cfg; cfgMu.RUnlock()
		// senhas e tokens nunca saem do computador do rack
		pin, adm := c.ControlPIN != "", c.AdminPIN != ""
		tg, em := c.Telegram.Token != "", c.Email.Pass != ""
		c.ControlPIN, c.AdminPIN, c.Telegram.Token, c.Email.Pass = "", "", "", ""
		writeJSON(w, map[string]any{"config": c, "pinSet": pin, "adminPinSet": adm, "telegramTokenSet": tg, "emailPassSet": em, "local": isLocal(r), "dataDir": h.paths.Root})
	})
	m.HandleFunc("PUT /api/config", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Config
			NewPIN      *string `json:"newPin"`
			NewAdminPIN *string `json:"newAdminPin"`
		}
		if !readJSON(w, r, &in) { return }
		cfgMu.Lock()
		c := h.cfg
		c.StationName = nz(in.StationName, c.StationName)
		c.Devices = in.Devices
		c.Rules = in.Rules
		c.RecWatch = in.RecWatch
		c.Pairs = in.Pairs
		c.Protected = in.Protected
		c.AuxCfg = in.AuxCfg
		c.Companion = in.Companion
		c.WebhookURL = strings.TrimSpace(in.WebhookURL)
		// token/senha em branco = manter o que já está salvo (o painel não recebe esses valores)
		tgTok, emPass := c.Telegram.Token, c.Email.Pass
		c.Telegram = in.Telegram
		if strings.TrimSpace(c.Telegram.Token) == "" { c.Telegram.Token = tgTok }
		c.Schedule = in.Schedule
		c.MarkerPresets = in.MarkerPresets
		c.TSL = in.TSL
		c.Email = in.Email
		if c.Email.Pass == "" { c.Email.Pass = emPass }
		c.ReportOnClose = in.ReportOnClose
		c.ClipTemplate = in.ClipTemplate
		c.ClipNameOff = in.ClipNameOff
		if isLocal(r) {
			c.ControlEnabled = in.ControlEnabled
			c.AllowRemoteControl = in.AllowRemoteControl
			if in.NewPIN != nil { c.ControlPIN = strings.TrimSpace(*in.NewPIN) }
			if in.NewAdminPIN != nil { c.AdminPIN = strings.TrimSpace(*in.NewAdminPIN) }
			c.UpdateRepo = strings.TrimSpace(in.UpdateRepo)
			c.UpdateCheckOff = in.UpdateCheckOff
			c.UpdateAuto = in.UpdateAuto
			if c.UpdateBeta != in.UpdateBeta { c.UpdateBadVersion = "" }
			c.UpdateBeta = in.UpdateBeta
		}
		normalize(c)
		err := saveConfig(h.paths, c)
		cfgMu.Unlock()
		if err != nil { httpErr(w, 500, err.Error()); return }
		h.applyDevices()
		h.mu.Lock(); h.dirty = true; h.mu.Unlock()
		h.LogO(originOf(r), "", "RACK MONITOR", "CONFIGURAÇÃO SALVA", "info", fmt.Sprintf("%d equipamentos", len(in.Devices)), nil)
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/protect", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Device  string `json:"device"`
			Outputs []int  `json:"outputs"`
			Mode    string `json:"mode"` // lock | watch | remove
		}
		if !readJSON(w, r, &b) { return }
		h.mu.Lock()
		routes := map[int]int{}
		if s := h.states[b.Device]; s != nil && s.Hub != nil { for k, v := range s.Hub.Routes { routes[k] = v } }
		h.mu.Unlock()
		cfgMu.Lock()
		set := map[int]bool{}
		for _, o := range b.Outputs { set[o] = true }
		var keep []ProtectedRoute
		for _, p := range h.cfg.Protected { if !(p.Device == b.Device && set[p.Output]) { keep = append(keep, p) } }
		if b.Mode != "remove" {
			for _, o := range b.Outputs {
				exp := -1
				if b.Mode == "lock" { if v, ok := routes[o]; ok { exp = v } }
				keep = append(keep, ProtectedRoute{Device: b.Device, Output: o, Expected: exp})
			}
		}
		h.cfg.Protected = keep
		_ = saveConfig(h.paths, h.cfg)
		cfgMu.Unlock()
		h.mu.Lock(); h.dirty = true; h.mu.Unlock()
		h.LogO(originOf(r), b.Device, "", "ROTAS PROTEGIDAS", "info", fmt.Sprintf("%d saídas → %s", len(b.Outputs), map[string]string{"lock": "travadas no roteamento atual", "watch": "monitorar qualquer troca", "remove": "proteção removida"}[b.Mode]), nil)
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/auxcfg/bulk", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Device string       `json:"device"`
			Items  []AuxSetting `json:"items"`
		}
		if !readJSON(w, r, &b) { return }
		h.mu.Lock()
		cur := map[int]int{}
		if s := h.states[b.Device]; s != nil && s.Atem != nil { for k, v := range s.Atem.Aux { cur[k] = v } }
		h.mu.Unlock()
		cfgMu.Lock()
		set := map[int]AuxSetting{}
		for _, it := range b.Items {
			it.Device = b.Device
			it.Expected = -1
			if it.Mode == "lock" { if v, ok := cur[it.Aux]; ok { it.Expected = v } else { it.Mode = "monitor" } }
			set[it.Aux] = it
		}
		var keep []AuxSetting
		for _, a := range h.cfg.AuxCfg { if _, ch := set[a.Aux]; !(a.Device == b.Device && ch) { keep = append(keep, a) } }
		for _, it := range set { keep = append(keep, it) }
		h.cfg.AuxCfg = keep
		normalize(h.cfg)
		_ = saveConfig(h.paths, h.cfg)
		out := append([]AuxSetting(nil), h.cfg.AuxCfg...)
		cfgMu.Unlock()
		h.mu.Lock(); h.dirty = true; h.mu.Unlock()
		cnt := map[string]int{}
		for _, it := range set { cnt[it.Mode]++ }
		h.LogO(originOf(r), b.Device, "", "AUX CONFIGURADO", "info", fmt.Sprintf("%d AUX alterados · monitorar %d · ignorar %d · travar %d", len(set), cnt["monitor"], cnt["ignore"], cnt["lock"]), nil)
		writeJSON(w, map[string]any{"ok": true, "auxCfg": out, "changed": len(set)})
	})
	m.HandleFunc("POST /api/auxcfg", func(w http.ResponseWriter, r *http.Request) {
		var b AuxSetting
		if !readJSON(w, r, &b) { return }
		h.mu.Lock()
		cur, has := -1, false
		if s := h.states[b.Device]; s != nil && s.Atem != nil { cur, has = s.Atem.Aux[b.Aux] }
		h.mu.Unlock()
		if b.Mode == "lock" {
			if !has { httpErr(w, 400, "AUX sem valor lido ainda — aguarde a ATEM conectar."); return }
			b.Expected = cur
		} else { b.Expected = -1 }
		cfgMu.Lock()
		var keep []AuxSetting
		for _, a := range h.cfg.AuxCfg { if !(a.Device == b.Device && a.Aux == b.Aux) { keep = append(keep, a) } }
		h.cfg.AuxCfg = append(keep, b)
		normalize(h.cfg)
		_ = saveConfig(h.paths, h.cfg)
		cfgMu.Unlock()
		h.mu.Lock(); h.dirty = true; h.mu.Unlock()
		modo := map[string]string{"monitor": "monitorado", "ignore": "ignorado", "lock": "travado na fonte atual"}[b.Mode]
		if modo == "" { modo = "monitorado" }
		h.LogO(originOf(r), b.Device, "", "AUX CONFIGURADO", "info", fmt.Sprintf("%s → %s", auxLabel(b.Aux, b), modo), nil)
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/scan", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Prefix string `json:"prefix"` }
		if !readJSON(w, r, &b) { return }
		res := scanNetwork(b.Prefix)
		if res == nil { res = []Found{} }
		writeJSON(w, res)
	})
	m.HandleFunc("GET /api/netinfo", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, map[string]any{"ips": localIPs()}) })
	// ---- jobs ----
	m.HandleFunc("GET /api/jobs", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, h.ListJobs()) })
	m.HandleFunc("POST /api/jobs", func(w http.ResponseWriter, r *http.Request) {
		var j Job
		if !readJSON(w, r, &j) { return }
		writeJSON(w, h.NewJob(j, false))
	})
	m.HandleFunc("PUT /api/job", func(w http.ResponseWriter, r *http.Request) {
		var j Job
		if !readJSON(w, r, &j) { return }
		if err := h.UpdateJobMeta(j); err != nil { httpErr(w, 400, err.Error()); return }
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/jobs/close", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock(); id := ""; if h.job != nil { id = h.job.ID }; h.mu.Unlock()
		h.CloseJob()
		writeJSON(w, map[string]any{"ok": true, "closed": id})
	})
	m.HandleFunc("GET /api/jobs/{id}/report", func(w http.ResponseWriter, r *http.Request) {
		rep, err := h.BuildReport(r.PathValue("id"))
		if err != nil { httpErr(w, 404, err.Error()); return }
		writeJSON(w, rep)
	})
	m.HandleFunc("GET /api/jobs/{id}/resumo.csv", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+id+"_resumo.csv\"")
		if err := h.WriteSummaryCSV(id, w); err != nil { httpErr(w, 404, err.Error()) }
	})
	m.HandleFunc("GET /api/jobs/{id}/csv", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+id+".csv\"")
		if err := h.WriteCSV(id, w); err != nil { httpErr(w, 404, err.Error()) }
	})
	// ---- controle (protegido) ----
	m.HandleFunc("POST /api/control/arm", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ PIN string `json:"pin"`; Seconds int `json:"seconds"` }
		if !readJSON(w, r, &b) { return }
		if err := h.canControl(r); err != nil { httpErr(w, 403, err.Error()); return }
		cfgMu.RLock(); pin := h.cfg.ControlPIN; cfgMu.RUnlock()
		if pin != "" {
			if msg := checkSecret(r, pin, b.PIN, "PIN incorreto."); msg != "" { h.LogO(originOf(r), "", "RACK MONITOR", "PIN INCORRETO", "warn", "Tentativa de liberar comandos: "+msg, nil); httpErr(w, 403, msg); return }
		}
		if b.Seconds <= 0 || b.Seconds > 600 { b.Seconds = 120 }
		h.mu.Lock(); h.ctrlUntil = time.Now().Add(time.Duration(b.Seconds) * time.Second); h.dirty = true; h.mu.Unlock()
		h.LogO(originOf(r), "", "RACK MONITOR", "COMANDOS LIBERADOS", "warn", fmt.Sprintf("Comandos físicos liberados por %d s.", b.Seconds), nil)
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/control/disarm", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock(); h.ctrlUntil = time.Time{}; h.dirty = true; h.mu.Unlock()
		h.LogO(originOf(r), "", "RACK MONITOR", "COMANDOS TRAVADOS", "info", "Comandos físicos travados.", nil)
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/control", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Device string `json:"device"`
			Action string `json:"action"`
			A      int    `json:"a"`
			B      int    `json:"b"`
			Name   string `json:"name"`
			Media  string `json:"media"` // formatação pela REST API (nome interno da mídia)
			PIN    string `json:"pin"`   // obrigatório para formatar
		}
		if !readJSON(w, r, &b) { return }
		if err := h.canControl(r); err != nil { httpErr(w, 403, err.Error()); return }
		h.mu.Lock(); armed := time.Now().Before(h.ctrlUntil); s := h.states[b.Device]; h.mu.Unlock()
		if !armed { httpErr(w, 403, "Comandos travados. Clique em LIBERAR COMANDOS primeiro."); return }
		if s == nil { httpErr(w, 404, "equipamento não encontrado"); return }
		var err error
		desc := ""
		h.CommandedBy(b.Device, originOf(r))
		if b.Action == "route" { h.Expect(fmt.Sprintf("%s:route:%d:%d", b.Device, b.A, b.B), originOf(r)) }
		if b.Action == "aux" { h.Expect(fmt.Sprintf("%s:aux:%d:%d", b.Device, b.A, b.B), originOf(r)) }
		switch s.Kind {
		case "atem":
			err = atemCommand(b.Device, b.Action, b.A, b.B)
			desc = atemDesc(s, b.Action, b.A, b.B)
		case "hyperdeck":
			if s.Via == "companion" && b.Action != "format" { httpErr(w, 409, "Este HyperDeck está em modo \"Via Companion\" (somente leitura). Comande pelo Companion."); return }
			if b.Action == "format" {
				h.mu.Lock()
				rec := s.Deck != nil && s.Deck.Recording()
				watched := false
				for _, id := range h.cfg.RecWatch { if id == b.Device { watched = h.recArmed } }
				h.mu.Unlock()
				if rec { httpErr(w, 409, "Este gravador está EM REC. Pare a gravação antes de formatar."); return }
				if watched { httpErr(w, 409, "Gravador vigiado com a vigilância ARMADA. Desarme a vigilância antes de formatar."); return }
				cfgMu.RLock(); pin := h.cfg.ControlPIN; cfgMu.RUnlock()
				if pin == "" { httpErr(w, 403, "Formatar apaga o cartão: defina antes um PIN em CONFIGURAÇÃO → COMANDOS FÍSICOS (no computador do rack)."); return }
				if msg := checkSecret(r, pin, b.PIN, "PIN incorreto."); msg != "" { h.LogO(originOf(r), b.Device, s.Name, "FORMATAÇÃO RECUSADA", "warn", msg, nil); httpErr(w, 403, msg); return }
				fs := "exFAT"
				if b.B == 1 { fs = "HFS+" }
				var det string
				if b.Media != "" || s.Via == "companion" {
					if b.Media == "" { httpErr(w, 400, "Escolha a mídia a formatar."); return }
					nm := cardName(b.Name, fs)
					err = deckRESTFormat(hostPort(Device{Host: s.Host}), b.Media, fs, nm)
					det = fmt.Sprintf("mídia %s · %s%s (REST API)", b.Media, fs, map[bool]string{true: " · nome " + nm, false: ""}[nm != ""])
				} else {
					det, err = deckFormat(b.Device, b.A, fs, b.Name)
				}
				if err == nil {
					h.LogO(originOf(r), b.Device, s.Name, "MÍDIA FORMATADA", "crit", "Formatação enviada: "+det+". Todo o conteúdo do cartão/SSD foi apagado.", map[string]any{"slot": b.A})
					writeJSON(w, ok()); return
				}
				desc = fmt.Sprintf("Formatar slot %d", b.A)
				break
			}
			if b.Action == "record" && strings.TrimSpace(b.Name) == "" { b.Name = h.TakeClipName(b.Device) }
			err = deckCommand(b.Device, b.Action, b.Name)
			if err == nil && b.Action == "record" { noteSentName(b.Device, clipNameRe.Replace(strings.TrimSpace(b.Name))) }
			desc = "HyperDeck " + strings.ToUpper(b.Action) + map[bool]string{true: " · clipe " + b.Name, false: ""}[b.Name != ""]
		case "bmcamera":
			if b.Action != "record" && b.Action != "stop" { err = fmt.Errorf("a câmera aceita só REC/STOP por aqui") } else { err = camCommand(b.Device, b.Action, b.Name) }
			desc = "Câmera " + strings.ToUpper(b.Action)
		case "videohub":
			if b.Action != "route" { err = fmt.Errorf("ação inválida") } else { err = hubRoute(b.Device, b.A, b.B) }
			desc = fmt.Sprintf("VideoHub saída %d ← entrada %d", b.A+1, b.B+1)
		default:
			err = fmt.Errorf("este equipamento não aceita comandos")
		}
		if err != nil {
			h.LogO(originOf(r), b.Device, s.Name, "COMANDO FALHOU", "warn", desc+": "+err.Error(), nil)
			httpErr(w, 502, err.Error()); return
		}
		h.LogO(originOf(r), b.Device, s.Name, "COMANDO ENVIADO", "warn", desc, nil)
		writeJSON(w, ok())
	})
	// ---- pós-produção ----
	marker := func(w http.ResponseWriter, r *http.Request, label, note string) {
		e := h.Marker(label, note, originOf(r))
		writeJSON(w, map[string]any{"ok": true, "msg": e.Msg})
	}
	m.HandleFunc("POST /api/marker", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Label, Note string }
		if !readJSON(w, r, &b) { return }
		marker(w, r, b.Label, b.Note)
	})
	m.HandleFunc("GET /api/marker", func(w http.ResponseWriter, r *http.Request) { marker(w, r, r.URL.Query().Get("label"), r.URL.Query().Get("note")) })
	m.HandleFunc("GET /api/deckmedia/{id}", func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock(); s := h.states[r.PathValue("id")]; h.mu.Unlock()
		if s == nil { httpErr(w, 404, "equipamento não encontrado"); return }
		list, err := deckRESTMedia(s.Host)
		if err != nil { httpErr(w, 502, err.Error()); return }
		writeJSON(w, list)
	})
	m.HandleFunc("GET /api/clipname/{id}", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"name": h.PreviewClipName(r.PathValue("id"))})
	})
	m.HandleFunc("GET /api/jobs/{id}/inventario", func(w http.ResponseWriter, r *http.Request) {
		inv, err := h.BuildInventory(r.PathValue("id"), r.URL.Query().Get("cards") != "0")
		if err != nil { httpErr(w, 404, err.Error()); return }
		writeJSON(w, inv)
	})
	m.HandleFunc("GET /api/jobs/{id}/inventario.csv", func(w http.ResponseWriter, r *http.Request) {
		inv, err := h.BuildInventory(r.PathValue("id"), true)
		if err != nil { httpErr(w, 404, err.Error()); return }
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="inventario_%s.csv"`, r.PathValue("id")))
		w.Write([]byte("\xef\xbb\xbf"))
		cw := csv.NewWriter(w); cw.Comma = ';'
		cw.Write([]string{"GRAVADOR", "IP", "SLOT", "VOLUME", "Nº", "CLIPE", "DURAÇÃO", "CODEC", "FORMATO", "REC INÍCIO", "REC FIM"})
		for _, d := range inv.Decks {
			for _, s := range d.Slots {
				for _, c := range s.Clips {
					a, b := "", ""
					if !c.RecStart.IsZero() { a = c.RecStart.Format("02/01/2006 15:04:05") }
					if !c.RecStop.IsZero() { b = c.RecStop.Format("02/01/2006 15:04:05") }
					cw.Write([]string{d.Name, d.Host, strconv.Itoa(s.Slot), s.Volume, strconv.Itoa(c.ID), c.Name, c.Duration, c.Codec, c.Format, a, b})
				}
			}
		}
		cw.Flush()
	})
	m.HandleFunc("GET /api/tc", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, h.TCSnapshot()) })
	m.HandleFunc("GET /api/jobs/{id}/edl", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		me := 0
		fmt.Sscanf(q.Get("me"), "%d", &me)
		txt, n, err := h.BuildEDL(r.PathValue("id"), q.Get("atem"), me, q.Get("deck"))
		if err != nil { httpErr(w, 404, err.Error()); return }
		if q.Get("check") == "1" { writeJSON(w, map[string]any{"events": n}); return }
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s_%s_ME%d.edl\"", r.PathValue("id"), q.Get("atem"), me+1))
		io.WriteString(w, txt)
	})
	m.HandleFunc("GET /api/jobs/{id}/markers.csv", func(w http.ResponseWriter, r *http.Request) {
		evs, err := h.ReadEvents(r.PathValue("id"))
		if err != nil { httpErr(w, 404, err.Error()); return }
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+r.PathValue("id")+"_marcadores.csv\"")
		io.WriteString(w, "\ufeffhora;marcador;observacao;no_ar;timecodes\r\n")
		for _, e := range evs {
			if e.Type != "MARCADOR" { continue }
			lbl, _ := e.Data["label"].(string); note, _ := e.Data["note"].(string); pgm, _ := e.Data["pgm"].(string)
			var tcs []string
			if m, ok := e.Data["tcs"].(map[string]any); ok { for k, v := range m { tcs = append(tcs, fmt.Sprintf("%s=%v", k, v)) } }
			sort.Strings(tcs)
			fmt.Fprintf(w, "%s;%s;%s;%s;%s\r\n", e.At.Format("15:04:05"), lbl, strings.ReplaceAll(note, ";", ","), pgm, strings.Join(tcs, " "))
		}
	})
	m.HandleFunc("GET /api/decks/{id}/clips", func(w http.ResponseWriter, r *http.Request) {
		res, err := deckClips(h, r.PathValue("id"))
		if err != nil { httpErr(w, 502, err.Error()); return }
		writeJSON(w, res)
	})
	m.HandleFunc("POST /api/checklist", func(w http.ResponseWriter, r *http.Request) {
		items, v := h.RunChecklist(originOf(r))
		writeJSON(w, map[string]any{"items": items, "verdict": v, "at": time.Now()})
	})
	// ---- saúde e diagnóstico ----
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, h.Health()) })
	m.HandleFunc("POST /api/diag/start", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Seconds int `json:"seconds"` }
		if !readJSON(w, r, &b) { return }
		p, err := h.StartDiag(b.Seconds)
		if err != nil { httpErr(w, 409, err.Error()); return }
		h.LogO(originOf(r), "", "RACK MONITOR", "DIAGNÓSTICO INICIADO", "info", fmt.Sprintf("Capturando %d s de protocolo.", b.Seconds), nil)
		writeJSON(w, map[string]any{"ok": true, "file": filepath.Base(p)})
	})
	m.HandleFunc("POST /api/diag/stop", func(w http.ResponseWriter, r *http.Request) { h.StopDiag(); writeJSON(w, ok()) })
	m.HandleFunc("GET /api/diag/{name}", func(w http.ResponseWriter, r *http.Request) {
		n := filepath.Base(r.PathValue("name"))
		if !strings.HasPrefix(n, "DIAG_") || !strings.HasSuffix(n, ".txt") { httpErr(w, 400, "arquivo inválido"); return }
		w.Header().Set("Content-Disposition", "attachment; filename=\""+n+"\"")
		http.ServeFile(w, r, filepath.Join(h.paths.Root, "diagnostico", n))
	})
	m.HandleFunc("POST /api/telegram/test", func(w http.ResponseWriter, r *http.Request) {
		var t Telegram
		if !readJSON(w, r, &t) { return }
		if t.Token == "" { cfgMu.RLock(); t.Token = h.cfg.Telegram.Token; cfgMu.RUnlock() }
		if err := sendTelegram(t, "✅ RACK MONITOR BY DON conectado ao Telegram — "+h.cfg.StationName); err != nil { httpErr(w, 400, err.Error()); return }
		writeJSON(w, ok())
	})
	// ---- perfis ----
	m.HandleFunc("GET /api/profiles", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, h.ListProfiles()) })
	m.HandleFunc("POST /api/profiles", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Name string `json:"name"` }
		if !readJSON(w, r, &b) { return }
		p, err := h.SaveProfile(b.Name)
		if err != nil { httpErr(w, 400, err.Error()); return }
		h.LogO(originOf(r), "", "RACK MONITOR", "PERFIL SALVO", "info", "Perfil de evento salvo: "+p.Name, nil)
		writeJSON(w, p)
	})
	m.HandleFunc("POST /api/profiles/load", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Name string `json:"name"` }
		if !readJSON(w, r, &b) { return }
		p, err := h.ReadProfile(b.Name)
		if err != nil { httpErr(w, 404, err.Error()); return }
		h.LoadProfile(*p)
		h.LogO(originOf(r), "", "RACK MONITOR", "PERFIL CARREGADO", "warn", fmt.Sprintf("Perfil \"%s\" aplicado (%d equipamentos).", p.Name, len(p.Devices)), nil)
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/profiles/import", func(w http.ResponseWriter, r *http.Request) {
		var p Profile
		if !readJSON(w, r, &p) { return }
		if p.Name == "" || len(p.Devices) == 0 { httpErr(w, 400, "arquivo de perfil inválido"); return }
		b, _ := json.MarshalIndent(p, "", "  ")
		if err := os.WriteFile(filepath.Join(h.profDir(), slug(p.Name)+".json"), b, 0o644); err != nil { httpErr(w, 500, err.Error()); return }
		writeJSON(w, ok())
	})
	m.HandleFunc("GET /api/profiles/{name}/export", func(w http.ResponseWriter, r *http.Request) {
		p, err := h.ReadProfile(r.PathValue("name"))
		if err != nil { httpErr(w, 404, err.Error()); return }
		w.Header().Set("Content-Disposition", "attachment; filename=\"perfil_"+slug(p.Name)+".json\"")
		writeJSON(w, p)
	})
	m.HandleFunc("DELETE /api/profiles/{name}", func(w http.ResponseWriter, r *http.Request) {
		if err := h.DeleteProfile(r.PathValue("name")); err != nil { httpErr(w, 404, err.Error()); return }
		writeJSON(w, ok())
	})
	// ---- rundown / relógio ----
	m.HandleFunc("GET /api/rundowns", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, h.PastRundowns(40)) })
	m.HandleFunc("PUT /api/rundown", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Items []RItem `json:"items"` }
		if !readJSON(w, r, &b) { return }
		if err := h.RundownAction("set", originOf(r), b.Items); err != nil { httpErr(w, 400, err.Error()); return }
		writeJSON(w, ok())
	})
	rdAct := func(w http.ResponseWriter, r *http.Request) {
		if err := h.RundownAction(r.PathValue("action"), originOf(r), nil); err != nil { httpErr(w, 400, err.Error()); return }
		writeJSON(w, ok())
	}
	m.HandleFunc("POST /api/rundown/{action}", rdAct)
	m.HandleFunc("GET /api/rundown/{action}", rdAct) // para botões do Companion (Generic HTTP GET)
	// ---- atalhos para Companion ----
	m.HandleFunc("GET /api/checklist/run", func(w http.ResponseWriter, r *http.Request) { _, v := h.RunChecklist(originOf(r)); writeJSON(w, map[string]any{"verdict": v}) })
	m.HandleFunc("GET /api/recwatch/{mode}", func(w http.ResponseWriter, r *http.Request) {
		on := r.PathValue("mode") == "arm"
		cfgMu.RLock(); n := len(h.cfg.RecWatch); cfgMu.RUnlock()
		if on && n == 0 { httpErr(w, 400, "sem gravadores vigiados"); return }
		h.mu.Lock(); h.recArmed = on; h.recArmedAt = time.Now(); h.dirty = true; h.mu.Unlock()
		h.LogO(originOf(r), "", "RACK MONITOR", map[bool]string{true: "VIGILÂNCIA ARMADA", false: "VIGILÂNCIA DESARMADA"}[on], map[bool]string{true: "warn", false: "info"}[on], "Pelo Companion/atalho.", nil)
		writeJSON(w, ok())
	})
	// ---- histórico / patch / e-mail ----
	m.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) {
		d, _ := strconv.Atoi(r.URL.Query().Get("days"))
		if d <= 0 { d = 30 }
		writeJSON(w, h.History(d))
	})
	m.HandleFunc("GET /api/patch", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, h.ListPatches()) })
	m.HandleFunc("POST /api/patch", func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Name string `json:"name"` }
		if !readJSON(w, r, &b) { return }
		f, err := h.SavePatch(b.Name)
		if err != nil { httpErr(w, 500, err.Error()); return }
		h.LogO(originOf(r), "", "RACK MONITOR", "FOTO DO PATCH", "info", "Patch salvo: "+nz(b.Name, f), nil)
		writeJSON(w, map[string]any{"ok": true, "file": f})
	})
	m.HandleFunc("GET /api/patch/{file}/diff", func(w http.ResponseWriter, r *http.Request) {
		d, name, err := h.DiffPatch(r.PathValue("file"))
		if err != nil { httpErr(w, 404, err.Error()); return }
		if d == nil { d = []map[string]string{} }
		writeJSON(w, map[string]any{"name": name, "diff": d})
	})
	m.HandleFunc("POST /api/email/test", func(w http.ResponseWriter, r *http.Request) {
		var e EmailCfg
		if !readJSON(w, r, &e) { return }
		if e.Pass == "" { cfgMu.RLock(); e.Pass = h.cfg.Email.Pass; cfgMu.RUnlock() }
		if err := sendEmail(e, "Teste · RACK MONITOR BY DON", "<p>E-mail de teste do <b>RACK MONITOR BY DON</b> — "+h.cfg.StationName+".</p>"); err != nil { httpErr(w, 400, err.Error()); return }
		writeJSON(w, ok())
	})
	m.HandleFunc("POST /api/jobs/{id}/send", func(w http.ResponseWriter, r *http.Request) {
		rep, err := h.BuildReport(r.PathValue("id"))
		if err != nil { httpErr(w, 404, err.Error()); return }
		txt, html := h.summaryText(rep)
		cfgMu.RLock(); t, e := h.cfg.Telegram, h.cfg.Email; cfgMu.RUnlock()
		sent := []string{}
		if t.Enabled { if err := sendTelegram(t, txt); err != nil { httpErr(w, 502, "Telegram: "+err.Error()); return }; sent = append(sent, "Telegram") }
		if e.Enabled { if err := sendEmail(e, "Relatório do job: "+rep.Job.Name, html); err != nil { httpErr(w, 502, "E-mail: "+err.Error()); return }; sent = append(sent, "e-mail") }
		if len(sent) == 0 { httpErr(w, 400, "Ative Telegram ou e-mail em CONFIGURAÇÃO."); return }
		writeJSON(w, map[string]any{"ok": true, "sent": sent})
	})
	// ---- interface ----
	files := http.FileServer(http.FS(static))
	m.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	}))
	return m
}

func (h *Hub) sse(w http.ResponseWriter, r *http.Request) {
	fl, okf := w.(http.Flusher)
	if !okf { http.Error(w, "sem streaming", 500); return }
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	ch := h.Subscribe()
	defer h.Unsubscribe(ch)
	fmt.Fprintf(w, "retry: 1500\nevent: state\ndata: %s\n\n", h.Snapshot())
	fl.Flush()
	ka := time.NewTicker(15 * time.Second)
	defer ka.Stop()
	for {
		select {
		case <-r.Context().Done(): return
		case msg := <-ch:
			w.Write(msg)
			// esvazia o que estiver na fila antes de enviar
			for n := 0; n < 64; n++ {
				select { case m2 := <-ch: w.Write(m2); continue; default: }
				break
			}
			fl.Flush()
		case <-ka.C:
			io.WriteString(w, ": ka\n\n"); fl.Flush()
		}
	}
}

func (h *Hub) canControl(r *http.Request) error {
	cfgMu.RLock(); defer cfgMu.RUnlock()
	if !h.cfg.ControlEnabled { return fmt.Errorf("Comandos físicos desativados em CONFIGURAÇÃO (modo somente leitura).") }
	if !isLocal(r) && !h.cfg.AllowRemoteControl { return fmt.Errorf("Comandos só podem ser enviados do computador do rack (localhost).") }
	return nil
}

func isLocal(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil { return false }
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func clientName(r *http.Request) string {
	if isLocal(r) { return "estação local" }
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return "via " + host
}

func localIPs() []string {
	var out []string
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 { continue }
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil { out = append(out, n.IP.String()) }
		}
	}
	return out
}

func ok() map[string]any { return map[string]any{"ok": true} }

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<20)).Decode(v); err != nil && err != io.EOF {
		httpErr(w, 400, "JSON inválido: "+err.Error()); return false
	}
	return true
}

func httpErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": msg})
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m { out = append(out, k) }
	for i := 1; i < len(out); i++ { for j := i; j > 0 && out[j] < out[j-1]; j-- { out[j], out[j-1] = out[j-1], out[j] } }
	return out
}

func atemDesc(s *DevState, action string, a, b int) string {
	name := func(src int) string { if s.Atem != nil { return s.Atem.Name(src) }; return fmt.Sprintf("fonte %d", src) }
	switch action {
	case "cut", "auto", "ftb": return fmt.Sprintf("ATEM %s no M/E %d", strings.ToUpper(action), a+1)
	case "pgm", "pvw": return fmt.Sprintf("ATEM %s M/E %d ← %s", strings.ToUpper(action), a+1, name(b))
	case "aux": return fmt.Sprintf("ATEM AUX %d ← %s", a+1, name(b))
	case "macro":
		n := ""
		if s.Atem != nil { n = s.Atem.Macros[a] }
		return fmt.Sprintf("ATEM MACRO %d %s", a+1, n)
	}
	return "ATEM " + action
}

var rdnsMu sync.Mutex
var rdnsCache = map[string]string{}

// originOf: "192.168.10.30 · PC-DIRETOR" (nome por DNS reverso quando a rede responde).
func originOf(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil { return r.RemoteAddr }
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() { return "esta estação (" + hostName() + ")" }
	rdnsMu.Lock()
	n, ok := rdnsCache[host]
	rdnsMu.Unlock()
	if !ok {
		ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
		names, err := net.DefaultResolver.LookupAddr(ctx, host)
		cancel()
		if err == nil && len(names) > 0 { n = strings.TrimSuffix(strings.SplitN(names[0], ".", 2)[0], ".") }
		rdnsMu.Lock(); rdnsCache[host] = n; rdnsMu.Unlock()
	}
	if n != "" { return host + " · " + strings.ToUpper(n) }
	return host
}
