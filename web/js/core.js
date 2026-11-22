// RACK MONITOR BY DON — painel web · núcleo: estado, utilidades, API, som, tempo real, cabeçalho, roteador e componentes
// Os arquivos de web/js compartilham o escopo global e são carregados em ordem pelo index.html.
"use strict";
let S = null;                // estado vindo do servidor
let EV = [];                 // linha do tempo ao vivo
let WHO = { local: false, canControl: false };
let view = "painel", params = [];
let soundOn = false, audio = null, lastBeep = 0;
let matrixDev = "", matrixFilter = "", matrixSel = new Set(), flashRows = {};
let evFilter = { sev: "info", dev: "", q: "" };
let rafPending = false, staticRendered = "";
const $ = (s, r = document) => r.querySelector(s);
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const jsq = s => esc(String(s ?? "").replace(/\\/g, "\\\\").replace(/'/g, "\\'"));
const pad = n => String(n).padStart(2, "0");
const hms = s => { s = Math.max(0, Math.floor(s)); return `${Math.floor(s / 3600)}:${pad(Math.floor(s / 60) % 60)}:${pad(s % 60)}`; };
const dur = s => { s = Math.max(0, Math.floor(s)); if (s < 60) return s + "s"; if (s < 3600) return Math.floor(s / 60) + "min " + pad(s % 60) + "s"; return Math.floor(s / 3600) + "h " + pad(Math.floor(s / 60) % 60) + "min"; };
const ago = t => dur((Date.now() - new Date(t)) / 1000);
const tm = t => { const d = new Date(t); return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`; };
const dt = t => { const d = new Date(t); return `${pad(d.getDate())}/${pad(d.getMonth() + 1)}/${d.getFullYear()} ${tm(t)}`; };
const valid = t => t && !String(t).startsWith("0001");
const dev = id => S?.devices?.find(d => d.id === id);
const devs = k => (S?.devices || []).filter(d => d.kind === k);
const recorders = () => (S?.devices || []).filter(d => d.kind === "hyperdeck" || d.kind === "bmcamera");
const kindName = { atem: "ATEM", hyperdeck: "HyperDeck", videohub: "VideoHub", bmcamera: "Câmera Blackmagic (REST)", webpresenter: "Encoder Web Presenter", ups: "Nobreak (SNMP)", switch: "Switch (SNMP)", ping: "Painel / só IP (ping)", tcp: "Porta TCP", http: "HTTP" };
const sevTxt = { ok: "OK", warn: "ATENÇÃO", crit: "CRÍTICO", off: "DESATIVADO" };
const TR = { record: "GRAVANDO", stopped: "PARADO", preview: "PREVIEW", play: "PLAY", forward: "AVANÇO", rewind: "RETROCESSO", jog: "JOG", shuttle: "SHUTTLE" };
const trp = t => TR[t] || (t || "—").toUpperCase();
let pointerDown = false;
document.addEventListener("pointerdown", () => pointerDown = true, true);
document.addEventListener("pointerup", () => { pointerDown = false; setTimeout(schedule, 30); }, true);
const armed = () => S && S.controlEnabled && new Date(S.controlArmedUntil) > Date.now();

// senha de administrador (alterar configuração por outro aparelho): fica só nesta aba
const adminPin = { get: () => { try { return sessionStorage.getItem("rackAdminPin") || ""; } catch { return ""; } }, set: v => { try { v ? sessionStorage.setItem("rackAdminPin", v) : sessionStorage.removeItem("rackAdminPin"); } catch { } } };
async function api(method, url, body, retried) {
  const headers = { "Content-Type": "application/json" };
  const ap = adminPin.get(); if (ap) headers["X-Admin-Pin"] = ap;
  const r = await fetch(url, { method, headers, body: body ? JSON.stringify(body) : undefined });
  const j = await r.json().catch(() => ({}));
  if (r.status === 401 && j.needAdminPin && (retried || 0) < 3) {
    adminPin.set("");
    const pin = await askSecret("SENHA DE ADMINISTRADOR", (ap ? `<div class="slotnote crit" style="margin:0 0 10px">${esc(j.error || "Senha incorreta.")}</div>` : "") + "Para alterar a configuração por este aparelho, digite a senha de administrador definida no computador do rack.");
    if (pin) { adminPin.set(pin); return api(method, url, body, (retried || 0) + 1); }
  }
  if (!r.ok) throw new Error(j.error || ("HTTP " + r.status));
  return j;
}
function askSecret(title, msg) {
  return new Promise(res => {
    const b = modal(title, `<div style="font-size:15px;line-height:1.5">${msg}</div><input id="sqIn" type="password" autocomplete="off" style="width:100%;margin-top:12px"><div class="row" style="margin-top:14px;justify-content:flex-end"><button class="btn" id="sqNo">CANCELAR</button><button class="btn red big" id="sqOk">OK</button></div>`);
    const inp = $("#sqIn", b); setTimeout(() => inp.focus(), 50);
    const done = v => { closeModal(); res(v); };
    $("#sqNo", b).onclick = () => done("");
    $("#sqOk", b).onclick = () => done(inp.value.trim());
    inp.onkeydown = e => { if (e.key === "Enter") done(inp.value.trim()); };
  });
}
function toast(msg, cls = "") {
  const t = document.createElement("div"); t.className = "toast " + cls; t.innerHTML = msg;
  $("#toasts").appendChild(t); setTimeout(() => t.remove(), cls === "crit" ? 9000 : 4500);
}
function modal(title, html) {
  $("#mtitle").textContent = title; $("#mbody").innerHTML = html; $("#modal").hidden = false;
  return $("#mbody");
}
function closeModal() { $("#modal").hidden = true; $("#mbody").innerHTML = ""; }
$("#mclose").onclick = closeModal;
$("#modal").onclick = e => { if (e.target.id === "modal") closeModal(); };
function confirmBox(title, msg, okTxt = "CONFIRMAR") {
  return new Promise(res => {
    const b = modal(title, `<div style="font-size:15px;line-height:1.5">${msg}</div><div class="row" style="margin-top:18px;justify-content:flex-end"><button class="btn" id="cNo">CANCELAR</button><button class="btn red big" id="cYes">${okTxt}</button></div>`);
    $("#cNo", b).onclick = () => { closeModal(); res(false); };
    $("#cYes", b).onclick = () => { closeModal(); res(true); };
  });
}
function pick(title, items, current) {
  return new Promise(res => {
    const b = modal(title, `<input id="pq" placeholder="Buscar…" style="width:100%"><div class="picks" id="pl"></div>`);
    const draw = q => {
      $("#pl", b).innerHTML = items.filter(i => !q || (i.label + " " + (i.sub || "")).toLowerCase().includes(q.toLowerCase()))
        .map(i => `<button data-v="${esc(i.value)}" class="${i.value == current ? "cur" : ""}">${esc(i.label)}<small>${esc(i.sub || "")}</small></button>`).join("");
      b.querySelectorAll("#pl button").forEach(x => x.onclick = () => { closeModal(); res(x.dataset.v); });
    };
    draw(""); $("#pq", b).oninput = e => draw(e.target.value); setTimeout(() => $("#pq", b)?.focus(), 30);
  });
}
async function control(device, action, a = 0, b = 0, name = "", ask = "") {
  if (!armed()) { toast("Comandos travados. Clique em 🔒 COMANDOS para liberar.", "warn"); return; }
  if (ask && !(await confirmBox("CONFIRMAR COMANDO REAL", ask))) return;
  try { await api("POST", "/api/control", { device, action, a, b, name }); toast("Comando enviado ✓", "ok"); }
  catch (e) { toast("Falhou: " + esc(e.message), "crit"); }
}

// ---------------- som ----------------
function beep(kind = "crit") {
  if (!soundOn || !audio) return;
  const t0 = audio.currentTime;
  const tones = kind === "crit" ? [880, 660, 880, 660] : [740];
  tones.forEach((f, i) => {
    const o = audio.createOscillator(), g = audio.createGain();
    o.type = "square"; o.frequency.value = f; g.gain.value = 0.0001;
    o.connect(g); g.connect(audio.destination);
    g.gain.exponentialRampToValueAtTime(0.18, t0 + i * 0.18 + 0.01);
    g.gain.exponentialRampToValueAtTime(0.0001, t0 + i * 0.18 + 0.16);
    o.start(t0 + i * 0.18); o.stop(t0 + i * 0.18 + 0.17);
  });
}
$("#soundBtn").onclick = () => {
  soundOn = !soundOn;
  if (soundOn && !audio) audio = new (window.AudioContext || window.webkitAudioContext)();
  $("#soundBtn").textContent = soundOn ? "🔊 SOM" : "🔇 SOM";
  $("#soundBtn").classList.toggle("live", soundOn);
  try { localStorage.setItem("rd_sound", soundOn ? "1" : "0"); } catch (e) { }
  if (soundOn) beep("warn");
};
setInterval(() => {
  if (!S) return;
  const crit = S.alerts?.some(a => a.sev === "crit" && !a.acked);
  if (crit && S.rules?.soundOnCritical && Date.now() - lastBeep > 6000) { beep("crit"); lastBeep = Date.now(); }
}, 1000);

// ---------------- tempo real ----------------
function connect() {
  const es = new EventSource("/sse");
  es.addEventListener("state", e => { S = fixState(JSON.parse(e.data)); $("#conn").hidden = true; schedule(); });
  es.addEventListener("log", e => {
    const ev = JSON.parse(e.data);
    EV.unshift(ev); if (EV.length > 3000) EV.length = 2500;
    if (ev.sev === "crit") {
      toast(`<b>${esc(ev.devName)}</b><br>${esc(ev.type)} — ${esc(ev.msg)}`, "crit");
      try { if (document.hidden && "Notification" in window && Notification.permission === "granted") new Notification("🔴 " + ev.type + " · " + (ev.devName || "RACK"), { body: ev.msg, icon: "icon-192.png", tag: ev.type + ev.device }); } catch (e) { }
    }
    if (ev.type === "ROTA ALTERADA" && ev.data) flashRows[ev.device + ":" + ev.data.out] = Date.now();
    schedule();
  });
  es.onerror = () => { $("#conn").hidden = false; };
}
// garante listas (o servidor antigo mandava null quando não havia nenhum alerta)
function fixState(s) {
  for (const k of ["devices", "alerts", "recWatch", "pairs", "protected", "auxCfg", "markerPresets", "schedule"]) if (!Array.isArray(s[k])) s[k] = [];
  if (!s.rules) s.rules = {};
  return s;
}
function schedule() { if (pointerDown) return; if (!rafPending) { rafPending = true; requestAnimationFrame(() => { rafPending = false; render(); }); } }
setInterval(() => { const d = new Date(); $("#clock").textContent = `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`; if (S && armed()) renderHeader(); }, 250);

// ---------------- cabeçalho ----------------
function renderHeader() {
  if (!S) return;
  $("#ver").textContent = "V" + S.version + (S.demo ? " · DEMO" : "");
  const crit = S.alerts.filter(a => a.sev === "crit"), warn = S.alerts.filter(a => a.sev === "warn");
  const pill = $("#statusPill");
  pill.className = "pill " + (crit.length ? "crit" : warn.length ? "warn" : "");
  pill.textContent = crit.length ? `${crit.length} CRÍTICO${crit.length > 1 ? "S" : ""}` : warn.length ? `${warn.length} ATENÇÃO` : "TUDO OK";
  $("#jobchip").innerHTML = S.job ? `<small>JOB ATUAL · ${dt(S.job.opened).slice(0, 16)}</small>${esc(S.job.name)}` : `<small>SEM JOB</small>—`;
  const ub = $("#updBtn"), U = S.update;
  if (ub) { ub.hidden = !U; if (U) ub.textContent = U.installing ? "⬆ " + (U.stage || "ATUALIZANDO…").toUpperCase() : `⬆ NOVA VERSÃO V${U.latest}`; }
  const rw = $("#recWatch");
  rw.className = "hbtn" + (S.recArmed ? " armed" : "");
  rw.textContent = S.recArmed ? "● VIGILÂNCIA ARMADA" : "VIGILÂNCIA DESARMADA";
  const cb = $("#ctrlBtn");
  cb.hidden = !S.controlEnabled;
  const rem = (new Date(S.controlArmedUntil) - Date.now()) / 1000;
  cb.className = "hbtn" + (rem > 0 ? " live" : "");
  cb.textContent = rem > 0 ? `🔓 LIBERADO ${hms(rem).slice(2)}` : "🔒 COMANDOS";
  document.body.classList.toggle("armed", rem > 0);
  const top = crit.find(a => !a.acked);
  const bn = $("#banner");
  if (top && view !== "wall" && view !== "tally") {
    bn.hidden = false;
    bn.innerHTML = `<span style="font-size:20px">⚠</span><span class="t">${esc(top.devName || "RACK")} · ${esc(top.title)}</span><span style="opacity:.85">${esc(top.msg)}</span><button id="ackTop">RECONHECER</button>`;
    $("#ackTop").onclick = () => api("POST", "/api/alerts/ack", { key: top.key });
  } else bn.hidden = true;
}
$("#statusPill").onclick = () => location.hash = "#/painel";
$("#updBtn").onclick = () => updInstall();
window.updInstall = async () => {
  let u; try { u = await api("GET", "/api/update?check=1"); } catch (e) { toast(esc(e.message), "crit"); return; }
  if (!u.available) { toast(u.error ? esc(u.error) : `Já está na versão mais recente (V${esc(u.current)}).`, u.error ? "warn" : "ok"); return; }
  const recs = recorders().filter(d => d.deck?.transport === "record").length;
  const notes = (u.notes || "").trim();
  if (!(await confirmBox("ATUALIZAR O RACK MONITOR", `Instalar a <b>V${esc(u.latest)}</b> (atual V${esc(u.current)})?<br><br>O monitor reinicia sozinho em poucos segundos — os equipamentos continuam funcionando normalmente, só a monitoração pausa nesse intervalo. Configuração, jobs e relatórios são mantidos.${recs ? `<br><br><b style="color:var(--warn)">${recs} gravador(es) em REC agora.</b> A gravação não é afetada, mas o registro fica alguns segundos sem leitura.` : ""}${notes ? `<div class="code" style="margin-top:10px;max-height:180px;overflow:auto;white-space:pre-wrap">${esc(notes)}</div>` : ""}`, "⬆ ATUALIZAR AGORA"))) return;
  try { await api("POST", "/api/update/install"); toast("Baixando e instalando… o painel volta sozinho.", "ok"); } catch (e) { toast(esc(e.message), "crit"); }
};
$("#recWatch").onclick = async () => {
  if (!S) return;
  if (!S.recArmed) {
    const names = (S.recWatch || []).map(id => dev(id)?.name).filter(Boolean);
    if (!names.length) { toast("Defina em CONFIGURAÇÃO quais gravadores devem estar em REC.", "warn"); location.hash = "#/config"; return; }
    if (!(await confirmBox("ARMAR VIGILÂNCIA DE GRAVAÇÃO", `Qualquer um destes gravadores fora de REC vira <b>alerta crítico</b> com som:<br><br>• ${names.map(esc).join("<br>• ")}<br><br><span class="mu">Somente leitura — nenhum comando é enviado aos gravadores.</span>`, "ARMAR"))) return;
  }
  api("POST", "/api/recwatch", { armed: !S.recArmed }).catch(e => toast(esc(e.message), "warn"));
};
$("#ctrlBtn").onclick = async () => {
  if (armed()) { await api("POST", "/api/control/disarm"); return; }
  if (!WHO.canControl) { toast(WHO.local ? "Comandos desativados na configuração." : "Comandos só podem ser liberados no computador do rack.", "warn"); return; }
  const b = modal("LIBERAR COMANDOS FÍSICOS", `<div class="hint" style="font-size:14px">Os botões passam a enviar comandos <b>reais</b> para ATEM, HyperDeck e VideoHub (corte, REC/STOP, roteamento). A liberação expira sozinha. Cada comando fica registrado na linha do tempo.</div>
   <div class="row" style="margin-top:14px">${S.pinRequired ? `<input id="pin" type="password" placeholder="PIN" style="width:120px">` : ""}
   <select id="secs"><option value="60">1 minuto</option><option value="120" selected>2 minutos</option><option value="300">5 minutos</option><option value="600">10 minutos</option></select>
   <button class="btn red big" id="doArm">LIBERAR</button></div>`);
  $("#doArm", b).onclick = async () => {
    try { await api("POST", "/api/control/arm", { pin: $("#pin", b)?.value || "", seconds: +$("#secs", b).value }); closeModal(); toast("Comandos liberados.", "warn"); }
    catch (e) { toast(esc(e.message), "crit"); }
  };
};

// ---------------- roteador ----------------
function route() {
  const h = (location.hash || "#/painel").slice(2).split("/");
  view = h[0] || "painel"; params = h.slice(1).map(decodeURIComponent);
  if (!$("#modal").hidden) closeModal();
  document.querySelectorAll("#nav a").forEach(a => a.classList.toggle("on", a.dataset.v === view || (["report", "resumo", "io"].includes(view) && a.dataset.v === "jobs")));
  document.body.classList.toggle("tallymode", view === "tally" && params.length >= 2);
  document.body.classList.toggle("wallmode", view === "wall" || (view === "relogio" && params[0] === "tela"));
  staticRendered = "";
  render();
}
window.addEventListener("hashchange", route);

function viewError(e) {
  console.error(e);
  staticRendered = "";
  $("#view").innerHTML = `<div class="empty" style="text-align:left"><b style="color:var(--red)">Esta tela encontrou um erro e não pôde ser desenhada.</b><br><br><code>${esc(e && (e.message || e))}</code><br><br>As outras abas continuam funcionando. <button class="btn" onclick="location.reload()">RECARREGAR PÁGINA</button></div>`;
}
function render() {
  try { renderHeader(); } catch (e) { console.error("cabeçalho", e); }
  if (!S) { $("#view").innerHTML = `<div class="empty">Conectando ao RACK MONITOR…</div>`; return; }
  const fn = { relogio: vClock, pos: vPos, checklist: vChecklist, saude: vSaude, resumo: vResumo, inventario: vInventario, io: vIO, painel: vPainel, atem: vAtem, gravacoes: vDecks, matriz: vMatriz, timeline: vTimeline, jobs: vJobs, report: vReport, config: vConfig, tally: vTally, wall: vWall }[view] || vPainel;
  const ae = document.activeElement, keep = ae && ae.id && $("#view").contains(ae) ? { id: ae.id, p: ae.selectionStart } : null;
  try {
    const r = fn();
    if (r && r.catch) r.catch(viewError);
  } catch (e) { viewError(e); }
  if (keep) { const el = document.getElementById(keep.id); if (el && el !== document.activeElement) { el.focus(); try { el.setSelectionRange(keep.p, keep.p); } catch (e) { } } }
}

// ---------------- componentes ----------------
function tileMetric(d) {
  if (!d.enabled) return `<div class="sub">monitoramento desativado</div>`;
  if (!d.online) return `<div><div class="big" style="color:var(--red)">${esc(d.status)}</div><div class="sub">${esc(d.detail || "")}${valid(d.since) ? " · há " + ago(d.since) : ""}</div></div>`;
  if (d.atem) {
    const M = Object.entries(d.atem.mes || {}).sort((x, y) => x[0] - y[0]);
    const nm = s => esc(d.atem.inputs?.[s]?.long || d.atem.inputs?.[s]?.short || "Fonte " + s);
    const extra = [d.atem.stream ? `STREAM ${d.atem.stream}` : "", d.atem.rec ? `REC ${d.atem.rec}` : ""].filter(Boolean).join(" · ");
    if (!M.length) return `<div class="sub">lendo estado…</div>`;
    return `<div style="min-width:0;flex:1"><table class="metab"><tr><th></th><th>PVW</th><th>PGM</th></tr>${M.map(([k, m]) => `<tr><td class="men">M/E ${+k + 1}</td><td class="pv">${nm(m.preview)}</td><td class="pg">${nm(m.program)}${m.ftb ? ` <b class="ftb">FTB</b>` : ""}</td></tr>`).join("")}</table>${extra ? `<div class="sub" style="margin-top:4px">${extra}</div>` : ""}</div>`;
  }
  if (d.deck) {
    const k = d.deck, rec = k.transport === "record";
    const pct = Math.min(100, (k.totalRemain / 3600) * 50);
    const cls = sevOfRemain(k.totalRemain);
    return `<div style="flex:1"><div class="row" style="justify-content:space-between">${rec ? `<span class="recdot"><i></i>REC</span>` : `<b style="letter-spacing:.08em">${esc(trp(k.transport))}</b>`}<span class="tc num" style="font-family:var(--mono)">${esc(k.tc || "")}</span></div>
      <div class="sub" style="margin-top:6px">Restante ${hms(k.totalRemain)} · slot ${k.activeSlot || "?"}${rec && k.slots?.[k.activeSlot] && k.slots[k.activeSlot].remainSec < (S.rules.slotWarnMin || 0) * 60 && nextSlot(k)[0] ? ` · <b style="color:var(--warn)">→ slot ${nextSlot(k)[0]} em ${hms(k.slots[k.activeSlot].remainSec)}</b>` : ""}</div><div class="bar ${cls}"><i style="width:${pct}%"></i></div></div>`;
  }
  if (d.hub) {
    const bad = (S.alerts || []).filter(a => a.device === d.id && a.key.startsWith("route")).length;
    return `<div><div class="big">${d.hub.inputs}×${d.hub.outputs}</div><div class="sub">${Object.keys(d.hub.routes || {}).length} rotas · ${d.hub.changes} trocas no job${bad ? ` · <b style="color:var(--red)">${bad} protegida(s) fora</b>` : ""}</div></div>`;
  }
  if (d.stream) {
    const st = d.stream, on = st.status === "Streaming";
    return `<div style="flex:1"><div class="row" style="justify-content:space-between">${on ? `<span class="recdot"><i></i>NO AR</span>` : `<b style="letter-spacing:.08em;color:var(--warn)">${esc((st.status || "—").toUpperCase())}</b>`}<span class="tc num" style="font-family:var(--mono)">${esc(st.duration || "")}</span></div>
      <div class="sub" style="margin-top:6px">${(st.bitrate / 1e6).toFixed(2)} Mb/s · ${esc(st.platform || "")} · cache ${st.cacheUsed}%</div><div class="bar ${st.cacheUsed >= 50 ? "crit" : st.cacheUsed >= 20 ? "warn" : ""}"><i style="width:${Math.max(2, st.cacheUsed)}%"></i></div></div>`;
  }
  if (d.snmp?.ups) {
    const u = d.snmp.ups;
    return `<div style="flex:1"><div class="row" style="justify-content:space-between"><b style="letter-spacing:.06em;color:${u.onBattery ? "var(--red)" : "var(--ok)"}">${u.onBattery ? "⚡ NA BATERIA" : "REDE ELÉTRICA"}</b><span class="num">${u.charge >= 0 ? u.charge + "%" : ""}</span></div>
      <div class="sub" style="margin-top:6px">autonomia ${u.minutesLeft >= 0 ? u.minutesLeft + " min" : "?"} · carga de saída ${u.load}%${u.onBattery ? " · há " + hms(u.secOnBattery) : ""}</div><div class="bar ${u.charge < 30 ? "crit" : u.charge < 60 ? "warn" : ""}"><i style="width:${Math.max(0, u.charge)}%"></i></div></div>`;
  }
  if (d.snmp?.ports) {
    const P = d.snmp.ports, down = P.filter(p => p.known && !p.up);
    return `<div style="flex:1"><div class="pgrid">${P.map(p => `<span class="pt ${!p.known ? "" : p.up ? "up" : "dn"}" title="Porta ${p.index} · ${esc(p.alias || p.name)}${p.speed ? " · " + p.speed + " Mb/s" : ""}${p.inErrors ? " · " + p.inErrors + " erros" : ""}">${p.index}</span>`).join("")}</div>
      <div class="sub" style="margin-top:6px">${down.length ? `<b style="color:var(--red)">CAIU: ${down.map(p => esc(p.alias || p.index)).join(", ")}</b>` : `${P.filter(p => p.up).length}/${P.length} portas com link`}</div></div>`;
  }
  if (d.probe) return `<div><div class="big">${d.latencyMs ? d.latencyMs.toFixed(1) + " ms" : "OK"}</div><div class="sub">${esc(d.probe.message)}</div></div>`;
  return `<div class="sub">${esc(d.detail)}</div>`;
}
function sevOfRemain(sec) { const r = S.rules; return sec / 60 < r.remainCritMin ? "crit" : sec / 60 < r.remainWarnMin ? "warn" : ""; }
function tile(d) {
  const link = { atem: "#/atem", hyperdeck: "#/gravacoes", bmcamera: "#/gravacoes", videohub: "#/matriz/" + d.id }[d.kind] || "#/saude";
  return `<div class="tile ${d.severity}" onclick="location.hash='${link}'"><div class="h"><span class="dot ${d.severity}"></span><div style="min-width:0;flex:1"><div class="n">${esc(d.name)}</div><div class="ip">${esc(kindName[d.kind] || d.kind)}${d.via === "companion" ? " · via Companion" : ""} · ${esc(d.host)}:${d.port}${d.online && d.latencyMs ? ` · ${d.latencyMs.toFixed(1)} ms` : ""}</div></div><span class="tag ${d.severity}">${sevTxt[d.severity] || ""}</span></div><div class="m">${tileMetric(d)}</div></div>`;
}
function alertCard(a) {
  return `<div class="alert ${a.sev} ${a.acked ? "acked" : ""}"><b>${esc(a.title)}</b><div class="d">${esc(a.devName || "RACK MONITOR")} — ${esc(a.msg)}</div><div class="w">desde ${tm(a.since)} · há ${ago(a.since)}${a.latched ? " · permanece até reconhecer" : ""}${a.acked ? " · reconhecido" : ""}</div>${a.acked ? "" : `<button class="btn" onclick="ack('${esc(a.key)}')">RECONHECER</button>`}</div>`;
}
window.ack = k => api("POST", "/api/alerts/ack", { key: k });
function originTag(o) {
  if (!o) return "";
  if (o === "externo") return `<span class="tag" title="A troca foi feita fora deste monitor: painel físico, Companion, ATEM Software Control ou outro programa. O protocolo Blackmagic não informa o IP de quem trocou.">EXTERNO</span>`;
  return `<span class="tag ok" title="Comando enviado por este monitor">${esc(o)}</span>`;
}
function evRow(e) {
  return `<div class="ev ${e.sev}"><span class="t">${tm(e.at)}</span><span><b>${esc(e.type)}</b> · <span class="mu">${esc(e.devName)}</span>${e.origin ? " " + originTag(e.origin) : ""}<br><span class="sm">${esc(e.msg)}</span></span></div>`;
}
