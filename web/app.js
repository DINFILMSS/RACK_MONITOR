// RACK MONITOR BY DON — interface web (sem dependências externas)
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

async function api(method, url, body) {
  const r = await fetch(url, { method, headers: { "Content-Type": "application/json" }, body: body ? JSON.stringify(body) : undefined });
  const j = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(j.error || ("HTTP " + r.status));
  return j;
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

// ---------------- PAINEL ----------------
function vPainel() {
  const D = S.devices.filter(d => d.enabled);
  const on = D.filter(d => d.online).length;
  const recs = recorders().filter(d => d.deck?.transport === "record").length;
  const crit = S.alerts.filter(a => a.sev === "crit").length;
  const groups = {};
  S.devices.forEach(d => { const g = d.group || ({ atem: "Switchers", hyperdeck: "Gravação", videohub: "Matrizes", ping: "Painéis", bmcamera: "Câmeras", webpresenter: "Streaming", ups: "Energia e rede", switch: "Energia e rede" }[d.kind] || "Rede"); (groups[g] = groups[g] || []).push(d); });
  // painéis sem grupo

  const al = S.alerts;
  $("#view").innerHTML = `
  <div class="kpis">
    <div class="kpi ${on === D.length ? "ok" : "crit"}"><span>EQUIPAMENTOS ONLINE</span><b class="num">${on}/${D.length}</b></div>
    <div class="kpi ${recs ? "crit" : ""}" ><span>GRAVADORES EM REC</span><b class="num">${recs}/${recorders().length}</b></div>
    <div class="kpi ${crit ? "crit" : "ok"}"><span>ALERTAS CRÍTICOS</span><b class="num">${crit}</b></div>
    <div class="kpi"><span>JOB EM ANDAMENTO</span><b class="num">${S.job ? dur((Date.now() - new Date(S.job.opened)) / 1000) : "—"}</b></div>
  </div>
  <div class="dash">
    <div>
      <h2>ALERTAS ATIVOS ${al.length ? `<span class="r"><button class="btn" onclick="ack('*')">RECONHECER TODOS</button></span>` : ""}</h2>
      <div class="alerts">${al.length ? al.map(alertCard).join("") : `<div class="okbig"><b>TUDO OK</b><div class="mu sm" style="margin-top:6px">${on} equipamentos respondendo · regras ativas</div></div>`}</div>
      <h2 style="margin-top:18px">ÚLTIMOS EVENTOS <span class="r"><a class="btn ghost" href="#/timeline">VER TUDO</a></span></h2>
      <div class="card feed" style="padding:6px 10px">${EV.filter(e => e.sev !== "trace").slice(0, 30).map(evRow).join("") || `<div class="empty">Sem eventos ainda.</div>`}</div>
    </div>
    <div>${Object.entries(groups).map(([g, list]) => `<div class="group"><div class="gh">${esc(g.toUpperCase())}</div><div class="tiles">${list.map(tile).join("")}</div></div>`).join("")}</div>
  </div>`;
}

// ---------------- ATEM ----------------
function vAtem() {
  const L = devs("atem");
  if (!L.length) { $("#view").innerHTML = `<div class="empty">Nenhuma ATEM cadastrada. Adicione em CONFIGURAÇÃO.</div>`; return; }
  $("#view").innerHTML = L.map(d => {
    const a = d.atem;
    const head = `<div class="atemhead"><span class="dot ${d.severity}"></span><span class="t">${esc(d.name)}</span><span class="tag">${esc(a?.product || "ATEM")}</span><span class="mu sm">${esc(d.host)} · UDP ${d.port}${a ? " · protocolo " + esc(a.version) : ""}${d.online ? ` · ${d.latencyMs?.toFixed(1)} ms` : ""}</span>
      ${a?.stream ? `<span class="tag ${a.stream === "STREAMING" ? "crit" : ""}">STREAM ${esc(a.stream)} ${esc(a.streamTime || "")}</span>` : ""}${a?.rec ? `<span class="tag ${a.rec === "GRAVANDO" ? "crit" : ""}">REC ${esc(a.rec)} ${esc(a.recTime || "")}</span>` : ""}
      ${a && a.macroRunning >= 0 ? `<span class="tag warn">MACRO ▶ ${esc(a.macros?.[a.macroRunning] || "#" + (a.macroRunning + 1))}</span>` : ""}
      ${Object.entries(a?.dsk || {}).map(([k, v]) => `<span class="tag ${v ? "crit" : ""}">DSK ${+k + 1} ${v ? "NO AR" : "OFF"}</span>`).join("")}</div>`;
    if (!d.online || !a) return `<section class="atem card">${head}<div class="empty">${esc(d.status)} — ${esc(d.detail)}</div></section>`;
    const nm = s => a.inputs?.[s]?.long || "Fonte " + s;
    const sh = s => a.inputs?.[s]?.short || s;
    const ext = Object.values(a.inputs || {}).filter(i => i.id > 0 && i.id < 1000).sort((x, y) => x.id - y.id);
    const others = Object.values(a.inputs || {}).filter(i => i.id === 0 || (i.id >= 1000 && i.id < 4000) || i.id === 6000).sort((x, y) => x.id - y.id);
    const bus = [...ext, ...others];
    const big = bus.length > 20;
    const mes = Object.entries(a.mes || {}).sort((x, y) => x[0] - y[0]).map(([i, m]) => `
      <div class="me"><div class="row"><span class="lbl">M/E ${+i + 1}</span>${m.ftb ? `<span class="tag crit">FADE TO BLACK</span>` : ""}${m.inTransition ? `<span class="tag warn">EM TRANSIÇÃO</span>` : ""}
        ${Object.entries(m.keys || {}).map(([k, v]) => `<span class="tag ${v ? "pgm" : ""}">USK ${+k + 1}${v ? " NO AR" : ""}</span>`).join("")}<span class="spacer"></span><span class="mu xs">no ar há ${valid(m.programSince) ? ago(m.programSince) : "—"}</span></div>
        <div class="busrow"><div class="bus pvw"><small>PREVIEW</small><b>${esc(nm(m.preview))}</b><div class="s">fonte ${m.preview}</div></div><div class="bus pgm"><small>PROGRAM</small><b>${esc(nm(m.program))}</b><div class="s">fonte ${m.program}</div></div></div>
        <div class="tbar"><i style="width:${m.inTransition ? m.transPos / 100 : 0}%"></i></div>
        ${big && !busOpen.has(d.id) ? "" : `<div class="srcs">`}${big && !busOpen.has(d.id) ? "" : bus.map(x => `<button class="src ${x.id === m.program ? "p" : x.id === m.preview ? "v" : ""}" title="${esc(x.long)}" ${S.controlEnabled ? `onclick="control('${d.id}','pvw',${i},${x.id})"` : "disabled"}>${esc(x.short || x.id)}</button>`).join("") + "</div>"}
        ${S.controlEnabled ? `<div class="ctrl needarm"><button class="btn red" onclick="control('${d.id}','cut',${i})">CUT</button><button class="btn" onclick="control('${d.id}','auto',${i})">AUTO</button><button class="btn" onclick="control('${d.id}','ftb',${i},0,'','Ativar/desativar FADE TO BLACK no M/E ${+i + 1} de ${jsq(d.name)}?')">FTB</button></div>` : ""}
      </div>`).join("");
    const ac = auxCfgOf(d.id);
    const allAux = Object.entries(a.aux || {}).sort((x, y) => x[0] - y[0]);
    const nIgn = allAux.filter(([k]) => ac[k]?.mode === "ignore").length;
    const af = auxFilter.toLowerCase();
    const auxs = allAux.filter(([k]) => (showIgnoredAux || ac[k]?.mode !== "ignore") && (!af || `aux ${+k + 1} ${auxName(d, k)} ${nm(a.aux[k])}`.toLowerCase().includes(af))).map(([k, s]) => {
      const c = Object.assign({}, ac[k] || {}); c.label = auxName(d, k);
      const bad = c.mode === "lock" && c.expected >= 0 && c.expected !== s;
      return `<div class="aux ${c.mode === "ignore" ? "ign" : ""} ${bad ? "bad" : ""}">
        <div class="ah"><span class="an">AUX ${+k + 1}</span><span class="al" title="${esc(c.label)}">${esc(c.label)}</span><button class="btn ghost" title="Nome, monitorar, ignorar ou travar" onclick="auxSet('${d.id}',${k})">⚙</button>${S.controlEnabled ? `<button class="btn needarm" onclick="auxPick('${d.id}',${k})">ROTEAR</button>` : ""}</div>
        <b class="as">${esc(nm(s))}</b>
        ${c.mode === "lock" ? `<span class="tag ${bad ? "crit" : "ok"}">🔐 ${bad ? "FORA DO PADRÃO · esperado " + esc(nm(c.expected)) : "TRAVADO EM " + esc(nm(c.expected))}</span>` : c.mode === "ignore" ? `<span class="tag off">IGNORADO · não registra</span>` : ""}</div>`;
    }).join("");
    const macros = Object.entries(a.macros || {}).map(([k, n]) => `<button class="btn ${a.macroRunning == k ? "red" : ""} ${S.controlEnabled ? "needarm" : ""}" ${S.controlEnabled ? `onclick="control('${d.id}','macro',${k},0,'','Executar a macro <b>${jsq(n)}</b> em ${jsq(d.name)}?')"` : "disabled"}>▶ ${esc(n)}</button>`).join("");
    const tl = Object.entries(a.tally || {}).filter(([, f]) => f).map(([s, f]) => `<span class="tag ${f & 1 ? "pgm" : "pvw"}">${esc(sh(+s))}</span>`).join(" ");
    return `<section class="atem card">${head}<div class="mes ${Object.keys(a.mes || {}).length === 4 ? "four" : ""}">${mes}</div>
      ${big ? `<div class="row" style="margin-top:8px"><button class="btn ghost" onclick="busOpen.has('${d.id}')?busOpen.delete('${d.id}'):busOpen.add('${d.id}');render()">${busOpen.has(d.id) ? "▾ ESCONDER" : "▸ MOSTRAR"} BOTÕES DAS ${bus.length} FONTES</button><span class="mu xs">${ext.length} entradas — escondidos para a tela caber; PGM/PVW continuam ao vivo acima</span></div>` : ""}
      <h2 style="margin-top:16px">SAÍDAS AUX <span class="mu xs" style="letter-spacing:.05em">${allAux.length} lidas da mesa${nIgn ? " · " + nIgn + " ignorada(s)" : ""}</span><span class="r">${allAux.length > 8 ? `<input class="auxq" id="auxq_${d.id}" placeholder="buscar AUX…" value="${esc(auxFilter)}" oninput="auxFilter=this.value;clearTimeout(window._aq);window._aq=setTimeout(render,250)" style="padding:5px 8px;width:170px">` : ""}${nIgn ? `<button class="btn ghost" onclick="showIgnoredAux=!showIgnoredAux;render()">${showIgnoredAux ? "ESCONDER" : "MOSTRAR"} ${nIgn} IGNORADO(S)</button>` : ""}</span></h2><div class="auxs ${allAux.length > 8 ? "compact" : ""}">${auxs || `<div class="mu">${allAux.length ? "nenhum AUX com esse filtro" : "sem AUX"}</div>`}</div>
      ${ccuTable(d)}
      <div class="row" style="margin-top:14px;align-items:flex-start;gap:24px"><div style="flex:1"><h2>MACROS</h2><div class="row">${macros || `<span class="mu sm">nenhuma macro gravada</span>`}</div></div>
      <div style="flex:1"><h2>TALLY ATUAL</h2><div class="row">${tl || `<span class="mu sm">—</span>`}</div><div class="xs mu" style="margin-top:6px">Tally em celulares: <a href="#/tally">abrir TALLY</a></div></div></div></section>`;
  }).join("");
}
function ccuTable(d) {
  const cams = Object.values(d.atem?.cams || {}).sort((x, y) => x.input - y.input);
  if (!cams.length) return "";
  const cols = ["Íris", "Foco", "Ganho", "Ganho dB", "Shutter", "Balanço de branco", "Filtro ND", "Lift (preto)", "Gama", "Ganho de cor", "Contraste", "Matiz/Saturação"].filter(c => cams.some(x => x.values[c]));
  const lastLog = cam => EV.find(e => e.device === d.id && e.type === "CONTROLE DE CÂMERA" && e.data?.input === cam);
  return `<h2 style="margin-top:16px">CONTROLE DE CÂMERA (CCU) <span class="tag warn" title="Leitura experimental: confira os valores com o Camera Control Panel no primeiro uso">EXPERIMENTAL</span><span class="mu xs" style="letter-spacing:.04em">ajustes feitos pelo Camera Control Panel, ATEM Software Control ou Companion</span></h2>
    <div class="scroll" style="max-height:none"><table><thead><tr><th>CÂMERA</th>${cols.map(c => `<th>${esc(c.toUpperCase())}</th>`).join("")}<th>AJUSTES NO JOB</th><th>ÚLTIMO</th></tr></thead><tbody>
    ${cams.map(c => { const l = lastLog(c.input); const onAir = Object.values(d.atem.mes || {}).some(m => m.program === c.input);
      return `<tr><td><b>${esc(d.atem.inputs?.[c.input]?.long || "CAM " + c.input)}</b>${onAir ? ` <span class="tag pgm">NO AR</span>` : ""}</td>${cols.map(k => `<td class="num">${esc(c.values[k] || "—")}</td>`).join("")}<td class="num">${c.changes}</td><td class="mu xs">${l ? tm(l.at) + " · " + esc(l.data.param) : "—"}</td></tr>`; }).join("")}</tbody></table></div>`;
}
let showIgnoredAux = false;
const busOpen = new Set();
const AUXDEF = /^(auxiliary|auxiliar|aux|output|saída|saida)\s*\d+$/i;
function auxMesa(d, k) { const n = d?.atem?.inputs?.[8001 + +k]?.long || ""; return AUXDEF.test(n.trim()) ? "" : n; }
function auxName(d, k) { return auxCfgOf(d.id)[k]?.label || auxMesa(d, k); }
let auxFilter = "";
function auxCfgOf(id) { const m = {}; (S.auxCfg || []).filter(x => x.device === id).forEach(x => m[x.aux] = x); return m; }
window.auxSet = (id, aux) => {
  const d = dev(id), c = auxCfgOf(id)[aux] || { mode: "monitor", label: "" };
  const cur = d.atem?.inputs?.[d.atem.aux[aux]]?.long || "";
  const b = modal(`AUX ${aux + 1} · ${d.name}`, `<label class="form" style="display:block"><span class="xs mu">NOME DESTA SAÍDA (ex.: VIDEO MAN, TELÃO, RETORNO PALCO)${auxMesa(d, aux) ? ` · na mesa: <b>${esc(auxMesa(d, aux))}</b>` : ""}</span><input id="axl" style="width:100%" value="${esc(c.label)}" placeholder="${esc(auxMesa(d, aux) || "sem nome")}"></label>
    <div style="margin-top:14px;display:grid;gap:8px">
      <label class="chk"><input type="radio" name="axm" value="monitor" ${c.mode === "monitor" ? "checked" : ""}><span><b>MONITORAR</b> — aparece na tela, registra trocas no job</span></label>
      <label class="chk"><input type="radio" name="axm" value="ignore" ${c.mode === "ignore" ? "checked" : ""}><span><b>IGNORAR</b> — some da tela, da linha do tempo, do relatório e do Companion (ex.: AUX do video man que troca o tempo todo)</span></label>
      <label class="chk"><input type="radio" name="axm" value="lock" ${c.mode === "lock" ? "checked" : ""}><span><b>TRAVAR</b> — alerta crítico se sair de <b>${esc(cur)}</b> (fonte atual)</span></label></div>
    <div class="row" style="margin-top:16px;justify-content:flex-end"><button class="btn red big" id="axs">SALVAR</button></div>`);
  $("#axs", b).onclick = async () => {
    try { await api("POST", "/api/auxcfg", { device: id, aux, label: $("#axl", b).value, mode: b.querySelector("input[name=axm]:checked").value }); closeModal(); toast("AUX atualizado.", "ok"); }
    catch (e) { toast(esc(e.message), "crit"); }
  };
};
window.auxPick = async (id, aux) => {
  const d = dev(id); if (!d?.atem) return;
  const items = Object.values(d.atem.inputs).sort((x, y) => x.id - y.id).map(i => ({ value: i.id, label: i.long, sub: `fonte ${i.id} · ${i.short}` }));
  const v = await pick(`AUX ${aux + 1} · ${d.name}`, items, d.atem.aux[aux]);
  if (v !== undefined) control(id, "aux", aux, +v, "", `Rotear AUX ${aux + 1} de <b>${esc(d.name)}</b> para <b>${esc(d.atem.inputs[v]?.long)}</b>?`);
};

// ---------------- GRAVAÇÕES ----------------
function vDecks() {
  const L = recorders();
  const watch = new Set(S.recWatch || []);
  const pairs = (S.pairs || []).map((p, i) => {
    const a = dev(p.primary), b = dev(p.backup);
    if (!a || !b) return "";
    const ra = a.deck?.transport === "record", rb = b.deck?.transport === "record";
    const fmtOk = !a.deck || !b.deck || (a.deck.videoFormat === b.deck.videoFormat && a.deck.fileFormat === b.deck.fileFormat);
    let v = "ok", t = "PAR GRAVANDO · OK";
    if (!a.online || !b.online) { v = "crit"; t = "PAR SEM CONEXÃO"; }
    else if (ra !== rb) { v = S.recArmed ? "crit" : "warn"; t = "DIVERGÊNCIA · UM GRAVA, OUTRO NÃO"; }
    else if (!ra && !rb) { v = S.recArmed ? "crit" : "warn"; t = "PAR PARADO"; }
    else if (!fmtOk) { v = "warn"; t = "GRAVANDO · FORMATOS DIFERENTES"; }
    const side = (d, role) => `<div class="side"><div class="mu xs">${role}</div><b>${esc(d.name)}</b><div class="row" style="margin-top:6px">${d.deck?.transport === "record" ? `<span class="recdot"><i></i>REC</span>` : `<b>${esc(d.deck ? trp(d.deck.transport) : d.status)}</b>`}<span class="spacer"></span><span style="font-family:var(--mono)">${esc(d.deck?.tc || "")}</span></div><div class="xs mu" style="margin-top:4px">${esc(d.deck?.videoFormat || "")} · ${esc(d.deck?.fileFormat || "")} · resta ${hms(d.deck?.totalRemain || 0)}</div></div>`;
    return `<div class="pair ${v}"><div class="row"><span class="verdict">${t}</span><span class="spacer"></span><span class="tag">${esc(p.label || "PAR " + (i + 1))}</span></div><div class="two">${side(a, "PRINCIPAL")}${side(b, "BACKUP")}</div></div>`;
  }).join("");
  const cards = L.map(d => {
    const k = d.deck || {}, rec = k.transport === "record";
    const slots = Object.entries(k.slots || {}).map(([i, s]) => {
      const active = +i === k.activeSlot;
      const cls = s.status === "mounted" ? (active || s.remainSec < 60 ? sevOfRemain(s.remainSec) : "") : (active || s.status === "error" ? "crit" : "idle");
      const pct = Math.min(100, s.remainSec / 3600 * 33);
      return `<div class="slot"><div class="r"><span>SLOT ${i}${+i === k.activeSlot ? " ◂ ativo" : ""} · ${esc(s.volume || "")}</span><b class="num" style="${cls === "idle" ? "color:var(--mu2)" : ""}">${s.status === "mounted" ? hms(s.remainSec) : ({ empty: active ? "SEM MÍDIA" : "VAZIO", error: "ERRO", mounting: "MONTANDO" }[s.status] || s.status)}</b></div><div class="bar ${cls}"><i style="width:${s.status === "mounted" ? pct : cls === "idle" ? 0 : 100}%"></i></div></div>`;
    }).join("");
    return `<div class="deck ${rec ? "rec" : ""} ${d.severity === "crit" ? "crit" : ""}">
      <div class="row"><span class="dot ${d.severity}"></span><b style="font-size:15px">${esc(d.name)}</b><span class="spacer"></span>${watch.has(d.id) ? `<span class="tag ${S.recArmed ? "crit" : ""}">VIGIADO</span>` : ""}</div>
      <div class="xs mu2" style="font-family:var(--mono);margin-top:3px">${d.via === "companion" ? `<span class="tag warn" title="Lido pelas variáveis do Companion (somente leitura)">VIA COMPANION</span> ${esc(d.compLabel || "")}` : `${esc(d.host)}:${d.port}`} · ${esc(k.model || "")}</div>
      ${d.online ? `<div class="row" style="margin-top:10px">${rec ? `<span class="recdot st"><i></i>GRAVANDO</span>` : `<span class="st" style="color:var(--mu)">${esc(trp(k.transport))}</span>`}<span class="spacer"></span>${rec && valid(k.recSince) ? `<span class="mu sm" title="${k.recSinceConn ? "já gravava quando o monitor conectou" : ""}">em REC há ${k.recSinceConn ? "≥ " : ""}${ago(k.recSince)}</span>` : ""}</div>
      <div class="tc num">${esc(k.tc || "--:--:--:--")}</div>${k.tcStall ? `<span class="tag crit">TIMECODE PARADO</span>` : ""}
      <div class="meta"><span>Formato <b>${esc(k.videoFormat || "—")}</b></span><span>Codec <b>${esc(k.fileFormat || "—")}</b></span><span>Clipe <b>${esc(k.clip || "—")}</b></span><span>Total restante <b class="num">${hms(k.totalRemain || 0)}</b></span></div>
      ${slotNotice(d)}
      ${slots}
      ${S.controlEnabled ? `${d.via !== "companion" ? `<div class="ctrl needarm"><button class="btn red" onclick="deckRec('${d.id}')">● REC</button><button class="btn" onclick="control('${d.id}','stop',0,0,'','PARAR a gravação de <b>${jsq(d.name)}</b>?')">■ STOP</button></div>` : ""}${d.kind === "hyperdeck" ? `<div class="row" style="margin-top:8px;justify-content:flex-end"><button class="btn ghost needarm" style="font-size:11px" title="Apagar e formatar um cartão/SSD" onclick="deckFormat('${d.id}')">⌫ FORMATAR CARTÃO / SSD</button></div>` : ""}` : ""}`
      : `<div class="empty" style="margin-top:12px">${esc(d.status)} · ${esc(d.detail)}</div>`}
    </div>`;
  }).join("");
  $("#view").innerHTML = `${pairs ? `<h2>PARES PRINCIPAL / BACKUP</h2><div class="pairs">${pairs}</div>` : ""}
    <h2>GRAVADORES <span class="r">${S.job ? `<a class="btn" href="#/inventario/${S.job.id}">▤ INVENTÁRIO DE CARTÕES</a> ` : ""}${S.controlEnabled ? `<button class="btn red needarm" onclick="recAll()">● REC EM TODOS OS VIGIADOS</button>` : ""}</span></h2>
    <div class="decks">${cards || `<div class="empty">Nenhum HyperDeck cadastrado.</div>`}</div>
    <div class="hint" style="margin-top:14px">REC confirmado pelo protocolo (transporte = record) e timecode avançando. Não substitui a conferência dos arquivos gravados.</div>`;
}
function nextSlot(k) {
  for (const [i, x] of Object.entries(k.slots || {})) if (+i !== k.activeSlot && x.status === "mounted" && x.remainSec > 60) return [+i, x];
  return [0, null];
}
function slotNotice(d) {
  const k = d.deck; if (!k) return "";
  const sl = k.slots?.[k.activeSlot], warn = (S.rules.slotWarnMin || 0) * 60;
  const recent = EV.find(e => e.device === d.id && e.type === "TROCA DE SLOT" && Date.now() - new Date(e.at) < 15 * 60000);
  let h = "";
  if (k.transport === "record" && sl && warn > 0 && sl.remainSec < warn) {
    const [n, x] = nextSlot(k);
    h += n ? `<div class="slotnote warn">⇢ VAI TROCAR PARA O SLOT ${n} em <b class="num">${hms(sl.remainSec)}</b><small>${esc(x.volume || "")} · ${hms(x.remainSec)} livres</small></div>`
      : `<div class="slotnote crit">⚠ SLOT ${k.activeSlot} ACABA em <b class="num">${hms(sl.remainSec)}</b><small>sem outro slot com espaço — a gravação vai PARAR</small></div>`;
  }
  if (recent) h += `<div class="slotnote ok">✓ TROCOU DE SLOT às ${tm(recent.at)}<small>${esc(recent.msg)}</small></div>`;
  return h;
}
window.deckFormat = id => {
  if (!armed()) { toast("Comandos travados. Clique em 🔒 COMANDOS para liberar.", "warn"); return; }
  const d = dev(id), k = d.deck || {};
  if (k.transport === "record") { toast("Pare a gravação antes de formatar.", "crit"); return; }
  const slots = Object.entries(k.slots || {});
  const rest = d.via === "companion";
  const b = modal("FORMATAR MÍDIA · " + d.name, `<div class="slotnote crit" style="margin:0 0 12px">⚠ APAGA TODO O CONTEÚDO do cartão/SSD escolhido. Não tem volta.</div>
    ${rest ? `<div class="mu sm" style="margin:-4px 0 10px">Modo "Via Companion": a formatação vai pela <b>REST API</b> do HyperDeck (outro canal, não derruba o Companion). Precisa do firmware 8.4+.</div>` : ""}
    <div class="form"><label>${rest ? "MÍDIA" : "SLOT"}<select id="fs_s">${rest ? `<option value="">lendo mídias do HyperDeck…</option>` : slots.map(([i, x]) => `<option value="${i}" ${+i !== k.activeSlot ? "selected" : ""}>Slot ${i} · ${esc(x.volume || mediaTxt(x.status))} · ${x.status === "mounted" ? hms(x.remainSec) + " livres" : mediaTxt(x.status)}</option>`).join("")}</select></label>
    <label>SISTEMA DE ARQUIVOS<select id="fs_f"><option value="0">exFAT (Windows e Mac)</option><option value="1">HFS+ (Mac)</option></select></label>
    <label>NOME DO CARTÃO<input id="fs_n" value="${esc((S.job?.name || d.name).replace(/[^\w-]+/g, "_").slice(0, 24))}"></label></div>
    <label class="form" style="display:block;margin-top:12px"><span class="xs mu">PARA CONFIRMAR, DIGITE <b style="color:var(--red)">FORMATAR</b></span><input id="fs_c" style="width:100%" autocomplete="off"></label>
    <div class="row" style="margin-top:14px;justify-content:flex-end"><button class="btn" onclick="closeModal()">CANCELAR</button><button class="btn red big" id="fs_go" disabled>⌫ FORMATAR AGORA</button></div>`);
  let media = [];
  if (rest) api("GET", "/api/deckmedia/" + id).then(L => {
    media = L || [];
    $("#fs_s", b).innerHTML = media.length ? media.map(m => `<option value="${esc(m.deviceName)}" ${m.activeDisk ? "" : "selected"}>${esc(m.volume || m.deviceName)} · ${esc(m.deviceName)}${m.activeDisk ? " · ATIVA (gravando aqui)" : ""}${m.remainingRecordTime ? " · " + hms(m.remainingRecordTime) + " livres" : ""}</option>`).join("") : `<option value="">nenhuma mídia montada</option>`;
  }).catch(e => { $("#fs_s", b).innerHTML = `<option value="">não leu as mídias</option>`; toast(esc(e.message), "crit"); });
  $("#fs_c", b).oninput = e => $("#fs_go", b).disabled = e.target.value.trim().toUpperCase() !== "FORMATAR";
  $("#fs_go", b).onclick = async () => {
    const sel = $("#fs_s", b).value;
    if (rest && !sel) { toast("Escolha a mídia.", "warn"); return; }
    const slot = rest ? 0 : +sel, lbl = rest ? (media.find(m => m.deviceName === sel)?.volume || sel) : "Slot " + slot;
    $("#fs_go", b).disabled = true; $("#fs_go", b).textContent = "FORMATANDO…";
    try { await api("POST", "/api/control", { device: id, action: "format", a: slot, b: +$("#fs_f", b).value, name: $("#fs_n", b).value, media: rest ? sel : "" }); closeModal(); toast(`${esc(lbl)} de ${esc(d.name)} formatado.`, "ok"); }
    catch (e) { $("#fs_go", b).textContent = "⌫ FORMATAR AGORA"; toast("Não formatou: " + esc(e.message), "crit"); }
  };
};
const mediaTxt = s => ({ mounted: "montada", empty: "sem mídia", error: "erro", mounting: "montando" }[s] || s || "?");
window.deckRec = async id => {
  const d = dev(id);
  let nm = "";
  try { nm = (await api("GET", "/api/clipname/" + id)).name || ""; } catch (e) { }
  if (await confirmBox("INICIAR GRAVAÇÃO", `Iniciar REC em <b>${esc(d.name)}</b>?<br><span class="mu sm">Nome do clipe: <b style="font-family:var(--mono)">${esc(nm || "padrão do gravador")}</b></span>`, "● GRAVAR"))
    control(id, "record", 0, 0, "");
};
window.recAll = async () => {
  const ids = (S.recWatch || []).filter(id => dev(id)?.online && dev(id)?.deck?.transport !== "record");
  if (!ids.length) { toast("Todos os gravadores vigiados já estão em REC.", "ok"); return; }
  const names = {};
  for (const id of ids) { try { names[id] = (await api("GET", "/api/clipname/" + id)).name || ""; } catch (e) { } }
  if (!(await confirmBox("REC EM TODOS", `Enviar REC para ${ids.length} gravador(es):<br>• ${ids.map(i => esc(dev(i).name) + (names[i] ? ` <span class="mu sm" style="font-family:var(--mono)">→ ${esc(names[i])}</span>` : "")).join("<br>• ")}`, "● GRAVAR TODOS"))) return;
  for (const id of ids) await control(id, "record", 0, 0, "");
};

// ---------------- MATRIZ ----------------
function vMatriz() {
  const L = devs("videohub");
  if (!L.length) { $("#view").innerHTML = `<div class="empty">Nenhuma VideoHub cadastrada.</div>`; return; }
  if (params[0]) matrixDev = params[0];
  if (!dev(matrixDev) || dev(matrixDev).kind !== "videohub") matrixDev = L[0].id;
  const d = dev(matrixDev), hb = d.hub;
  const key = "m:" + matrixDev;
  if (staticRendered !== key) {
    staticRendered = key;
    $("#view").innerHTML = `<div class="tabs">${L.map(x => `<button class="${x.id === matrixDev ? "on" : ""}" onclick="location.hash='#/matriz/${x.id}'"><span class="dot ${x.severity}"></span> ${esc(x.name)}</button>`).join("")}</div>
      <div class="row" style="margin-bottom:10px"><input id="mq" placeholder="Filtrar saída ou entrada…" style="width:280px" value="${esc(matrixFilter)}">
      <span class="spacer"></span><span class="mu sm" id="msel"></span>
      <button class="btn" onclick="protect('lock')" title="Alerta crítico se a rota mudar do valor atual">🔐 TRAVAR NA ROTA ATUAL</button>
      <button class="btn" onclick="protect('watch')" title="Avisa qualquer troca">👁 VIGIAR TROCAS</button>
      <button class="btn ghost" onclick="protect('remove')">REMOVER PROTEÇÃO</button></div>
      <div id="minfo" class="mu sm" style="margin-bottom:8px"></div>
      <div class="scroll"><table><thead><tr><th style="width:34px"><input type="checkbox" id="mall"></th><th style="width:60px">SAÍDA</th><th>DESTINO</th><th style="width:40px"></th><th style="width:60px">ENTRADA</th><th>ORIGEM</th><th>PROTEÇÃO</th><th style="width:110px"></th></tr></thead><tbody id="mbody2"></tbody></table></div>`;
    $("#mq").oninput = e => { matrixFilter = e.target.value; drawMatrix(); };
    $("#mall").onchange = e => { const rows = matrixRows(); rows.forEach(r => e.target.checked ? matrixSel.add(r) : matrixSel.delete(r)); drawMatrix(); };
  }
  drawMatrix();
}
function matrixRows() {
  const d = dev(matrixDev), hb = d?.hub; if (!hb) return [];
  const q = matrixFilter.toLowerCase();
  return Object.keys(hb.routes || {}).map(Number).sort((a, b) => a - b).filter(o => {
    if (!q) return true;
    const i = hb.routes[o];
    return `${o + 1} ${hb.outLabels?.[o] || ""} ${i + 1} ${hb.inLabels?.[i] || ""}`.toLowerCase().includes(q);
  });
}
function drawMatrix() {
  const d = dev(matrixDev), hb = d?.hub, body = $("#mbody2");
  if (!body) return;
  $("#minfo").innerHTML = d.online && hb ? `${esc(hb.model)} · ${hb.inputs} entradas × ${hb.outputs} saídas · ${hb.changes} trocas neste job · latência ${d.latencyMs?.toFixed(1)} ms · atualização instantânea (push da matriz)` : `<span style="color:var(--red)">${esc(d.status)} — ${esc(d.detail)}</span>`;
  $("#msel").textContent = matrixSel.size ? `${matrixSel.size} saída(s) selecionada(s)` : "";
  if (!hb) { body.innerHTML = ""; return; }
  const prot = {}; (S.protected || []).filter(p => p.device === matrixDev).forEach(p => prot[p.output] = p);
  body.innerHTML = matrixRows().map(o => {
    const i = hb.routes[o], p = prot[o];
    const bad = p && p.expected >= 0 && p.expected !== i;
    const fl = flashRows[matrixDev + ":" + o] && Date.now() - flashRows[matrixDev + ":" + o] < 2500;
    return `<tr class="${p ? "prot" : ""} ${bad ? "bad" : ""} ${fl ? "flash" : ""}"><td><input type="checkbox" ${matrixSel.has(o) ? "checked" : ""} onchange="msel(${o},this.checked)"></td>
      <td class="num"><b>${o + 1}</b></td><td>${esc(hb.outLabels?.[o] || "")}</td><td class="mu">◀</td><td class="num"><b>${i + 1}</b></td><td>${esc(hb.inLabels?.[i] || "")}</td>
      <td>${p ? (p.expected >= 0 ? `<span class="tag ${bad ? "crit" : "ok"}">🔐 ${bad ? "FORA · esperado " + (p.expected + 1) + " " + esc(hb.inLabels?.[p.expected] || "") : "TRAVADA"}</span>` : `<span class="tag warn">👁 VIGIADA</span>`) : ""}${hb.locks?.[o] && hb.locks[o] !== "U" ? ` <span class="tag">LOCK ${esc(hb.locks[o])}</span>` : ""}</td>
      <td>${S.controlEnabled ? `<button class="btn needarm" onclick="routePick(${o})">ROTEAR</button>` : ""}${bad && S.controlEnabled ? ` <button class="btn red needarm" onclick="control('${matrixDev}','route',${o},${p.expected},'','Restaurar saída ${o + 1} para a entrada ${p.expected + 1}?')">↺</button>` : ""}</td></tr>`;
  }).join("");
}
window.msel = (o, on) => { on ? matrixSel.add(o) : matrixSel.delete(o); $("#msel").textContent = matrixSel.size ? `${matrixSel.size} saída(s) selecionada(s)` : ""; };
window.protect = async mode => {
  if (!matrixSel.size) { toast("Selecione saídas na tabela primeiro.", "warn"); return; }
  await api("POST", "/api/protect", { device: matrixDev, outputs: [...matrixSel], mode });
  toast({ lock: "Saídas travadas na rota atual.", watch: "Saídas sob vigilância.", remove: "Proteção removida." }[mode], "ok");
  matrixSel.clear();
};
window.routePick = async o => {
  const d = dev(matrixDev), hb = d.hub;
  const items = Object.keys(hb.inLabels).map(Number).sort((a, b) => a - b).map(i => ({ value: i, label: `${i + 1} · ${hb.inLabels[i]}` }));
  const v = await pick(`SAÍDA ${o + 1} · ${hb.outLabels[o] || ""}`, items, hb.routes[o]);
  if (v !== undefined) control(matrixDev, "route", o, +v, "", `Rotear saída <b>${o + 1} ${esc(hb.outLabels[o] || "")}</b> para entrada <b>${+v + 1} ${esc(hb.inLabels[v] || "")}</b>?`);
};

// ---------------- LINHA DO TEMPO ----------------
function vTimeline() {
  if (staticRendered !== "tl") {
    staticRendered = "tl";
    $("#view").innerHTML = `<div class="row" style="margin-bottom:12px">
      <select id="fs"><option value="trace">Tudo (inclui preview)</option><option value="info">Informações e acima</option><option value="warn">Atenção e crítico</option><option value="crit">Somente crítico</option></select>
      <select id="fd"><option value="">Todos os equipamentos</option>${S.devices.map(d => `<option value="${d.id}">${esc(d.name)}</option>`).join("")}<option value="__sys">RACK MONITOR</option></select>
      <input id="fq" placeholder="Buscar texto…" style="width:240px">
      <span class="spacer"></span>${S.job ? `<a class="btn" href="/api/jobs/${S.job.id}/csv">EXPORTAR CSV</a><a class="btn" href="#/resumo/${S.job.id}">RESUMO DO JOB</a><a class="btn" href="#/report/${S.job.id}">RELATÓRIO COMPLETO</a>` : ""}</div>
      <div class="scroll card" style="padding:0"><table><thead><tr><th style="width:90px">HORA</th><th style="width:80px">NÍVEL</th><th style="width:220px">EQUIPAMENTO</th><th style="width:200px">EVENTO</th><th>DESCRIÇÃO</th><th style="width:210px">ORIGEM</th></tr></thead><tbody id="tlb"></tbody></table></div>`;
    $("#fs").value = evFilter.sev; $("#fd").value = evFilter.dev; $("#fq").value = evFilter.q;
    $("#fs").onchange = e => { evFilter.sev = e.target.value; drawTL(); };
    $("#fd").onchange = e => { evFilter.dev = e.target.value; drawTL(); };
    $("#fq").oninput = e => { evFilter.q = e.target.value; drawTL(); };
  }
  drawTL();
}
const rank = { trace: 0, info: 1, warn: 2, crit: 3 };
function drawTL() {
  const q = evFilter.q.toLowerCase();
  const rows = EV.filter(e => rank[e.sev] >= rank[evFilter.sev] && (!evFilter.dev || (evFilter.dev === "__sys" ? !e.device : e.device === evFilter.dev)) && (!q || (e.type + " " + e.msg + " " + e.devName + " " + (e.origin || "")).toLowerCase().includes(q))).slice(0, 800);
  const lbl = { trace: "detalhe", info: "info", warn: "atenção", crit: "CRÍTICO" };
  $("#tlb").innerHTML = rows.map(e => `<tr><td class="num" style="font-family:var(--mono);font-size:12px">${tm(e.at)}</td><td><span class="tag ${e.sev === "info" ? "" : e.sev}">${lbl[e.sev]}</span></td><td>${esc(e.devName)}</td><td><b>${esc(e.type)}</b></td><td>${esc(e.msg)}</td><td>${originTag(e.origin)}</td></tr>`).join("") || `<tr><td colspan="6" class="mu">Nenhum evento com esse filtro.</td></tr>`;
}

// ---------------- JOBS ----------------
async function vJobs() {
  if (staticRendered === "jobs") return;
  staticRendered = "jobs";
  const jobs = await api("GET", "/api/jobs").catch(() => []);
  const j = S.job || {};
  $("#view").innerHTML = `<div class="cfg">
    <div class="card"><h2>JOB ATUAL</h2>${S.job ? `<div class="form">
      <label>NOME DO JOB<input id="jn" value="${esc(j.name)}"></label><label>CLIENTE<input id="jc" value="${esc(j.client)}"></label>
      <label>LOCAL / EVENTO<input id="jv" value="${esc(j.venue)}"></label><label>OPERADOR<input id="jo" value="${esc(j.operator)}"></label></div>
      <label class="form" style="margin-top:10px;display:block"><span class="xs mu">OBSERVAÇÕES</span><textarea id="jx" rows="3" style="width:100%">${esc(j.notes)}</textarea></label>
      <div class="row" style="margin-top:12px"><button class="btn red" id="jsave">SALVAR DADOS</button><a class="btn" href="#/resumo/${j.id}">RESUMO (1 PÁGINA)</a><a class="btn" href="#/report/${j.id}">RELATÓRIO COMPLETO</a><a class="btn" href="#/inventario/${j.id}">INVENTÁRIO DE CARTÕES</a><a class="btn" href="/api/jobs/${j.id}/csv">CSV</a><button class="btn" onclick="sendRep('${j.id}')">ENVIAR RESUMO AGORA</button><span class="spacer"></span><button class="btn" id="jclose">ENCERRAR JOB</button></div>
      <div class="hint" style="margin-top:8px">Aberto em ${dt(j.opened)} · o nome do job também vira o nome dos clipes ao gravar pelo painel.</div>` : `<div class="empty">Nenhum job aberto — eventos não estão sendo arquivados.</div>`}</div>
    <div class="card"><h2>NOVO JOB</h2><div class="form">
      <label>NOME<input id="nn" placeholder="ex.: Final Campeonato · Arena"></label><label>CLIENTE<input id="nc"></label>
      <label>LOCAL / EVENTO<input id="nv"></label><label>OPERADOR<input id="no" value="${esc(j.operator || "")}"></label></div>
      <div class="row" style="margin-top:12px"><button class="btn red" id="nnew">ABRIR NOVO JOB</button><span class="hint">O job atual é encerrado e arquivado automaticamente.</span></div></div>
    <div class="card wide"><h2>RELATÓRIO DE ENTRADAS E SAÍDAS (ATEM + VIDEOHUB)</h2><div class="row"><span class="hint" style="flex:1">Lista de inputs, AUX e rotas como estão AGORA: nomes, números e o que está roteado em cada saída. Ideal para conferir a montagem, mandar para a equipe ou arquivar o patch do evento. PDF pelo navegador ou CSV para Excel.</span><a class="btn red" href="#/io">ABRIR RELATÓRIO DE I/O</a></div></div>
    <div class="card wide"><h2>HISTÓRICO GERAL DA HOUSE <span class="r"><select id="hd" onchange="loadHist()"><option value="30">últimos 30 dias</option><option value="90">90 dias</option><option value="365">12 meses</option></select></span></h2><div id="hist"><div class="empty">Carregando…</div></div></div>
    <div class="card wide"><h2>HISTÓRICO DE JOBS</h2><table><thead><tr><th>ABERTO</th><th>JOB</th><th>CLIENTE</th><th>LOCAL</th><th>DURAÇÃO</th><th></th></tr></thead><tbody>
      ${jobs.map(x => `<tr><td class="num">${dt(x.opened)}</td><td><b>${esc(x.name)}</b>${!valid(x.closed) ? ` <span class="tag ok">ABERTO</span>` : ""}</td><td>${esc(x.client)}</td><td>${esc(x.venue)}</td><td class="num">${dur(((valid(x.closed) ? new Date(x.closed) : Date.now()) - new Date(x.opened)) / 1000)}</td>
        <td style="text-align:right"><a class="btn" href="#/resumo/${x.id}">RESUMO</a> <a class="btn ghost" href="#/report/${x.id}">COMPLETO</a> <a class="btn ghost" href="#/inventario/${x.id}">CARTÕES</a> <a class="btn ghost" href="/api/jobs/${x.id}/csv">CSV</a></td></tr>`).join("")}</tbody></table></div></div>`;
  if (S.job) {
    $("#jsave").onclick = async () => { await api("PUT", "/api/job", { name: $("#jn").value, client: $("#jc").value, venue: $("#jv").value, operator: $("#jo").value, notes: $("#jx").value }); toast("Job salvo.", "ok"); };
    $("#jclose").onclick = async () => { if (await confirmBox("ENCERRAR JOB", "Encerrar e arquivar o job atual? Os eventos param de ser gravados até abrir um novo job.", "ENCERRAR")) { await api("POST", "/api/jobs/close"); staticRendered = ""; setTimeout(render, 300); } };
  }
  loadHist();
  $("#nnew").onclick = async () => {
    const name = $("#nn").value.trim(); if (!name) { toast("Dê um nome ao job.", "warn"); return; }
    await api("POST", "/api/jobs", { name, client: $("#nc").value, venue: $("#nv").value, operator: $("#no").value });
    toast("Novo job aberto.", "ok"); EV = []; staticRendered = ""; setTimeout(render, 300);
  };
}

window.sendRep = async id => { try { const r = await api("POST", `/api/jobs/${id}/send`); toast("Resumo enviado: " + r.sent.join(" e "), "ok"); } catch (e) { toast(esc(e.message), "crit"); } };
window.loadHist = async () => {
  const el = $("#hist"); if (!el) return;
  const h = await api("GET", "/api/history?days=" + ($("#hd")?.value || 30)).catch(() => null);
  if (!h) { el.innerHTML = `<div class="empty">Falhou.</div>`; return; }
  const jobs = h.jobs || [], dv = (h.devices || []).filter(d => d.drops || d.alerts), tl = (h.titles || []).slice(0, 10), dk = h.decks || [];
  el.innerHTML = `<div class="kpis" style="grid-template-columns:repeat(5,1fr)"><div class="kpi"><span>JOBS</span><b class="num">${jobs.length}</b></div><div class="kpi"><span>HORAS NO AR</span><b class="num">${(h.seconds / 3600).toFixed(1)}</b></div><div class="kpi"><span>CORTES</span><b class="num">${h.cuts}</b></div><div class="kpi"><span>MARCADORES</span><b class="num">${h.markers}</b></div><div class="kpi ${h.crit ? "crit" : "ok"}"><span>EVENTOS CRÍTICOS</span><b class="num">${h.crit}</b></div></div>
   <div class="cfg"><div><h2>EQUIPAMENTOS QUE MAIS DÃO PROBLEMA</h2><table><thead><tr><th>EQUIPAMENTO</th><th>QUEDAS</th><th>TEMPO FORA</th><th>ALERTAS</th><th>JOBS</th></tr></thead><tbody>${dv.slice(0, 12).map(d => `<tr><td><b>${esc(d.name)}</b></td><td class="num" style="color:${d.drops ? "var(--red)" : ""}">${d.drops}</td><td class="num">${hms(d.offSeconds)}</td><td class="num">${d.alerts}</td><td class="num">${d.jobs}</td></tr>`).join("") || `<tr><td colspan="5" class="mu">Nenhum problema no período.</td></tr>`}</tbody></table></div>
   <div><h2>ALERTAS MAIS FREQUENTES</h2><table><tbody>${tl.map(t => `<tr><td>${esc(t.title)}</td><td class="num" style="text-align:right"><b>${t.count}</b></td></tr>`).join("") || `<tr><td class="mu">Nenhum.</td></tr>`}</tbody></table>
   <h2 style="margin-top:14px">GRAVAÇÃO NO PERÍODO</h2><table><tbody>${dk.map(d => `<tr><td>${esc(d.name)}</td><td class="num">${(d.recSeconds / 3600).toFixed(1)} h</td><td class="num ${d.interrupted ? "" : "mu"}" style="color:${d.interrupted ? "var(--red)" : ""}">${d.interrupted} interrup.</td><td class="num mu">${d.slotChanges} trocas de slot</td></tr>`).join("")}</tbody></table></div></div>`;
};

// ---------------- RELATÓRIO ----------------
async function vReport() {
  const id = params[0];
  if (staticRendered === "rep:" + id) return;
  staticRendered = "rep:" + id;
  $("#view").innerHTML = `<div class="empty">Calculando relatório…</div>`;
  let r;
  try { r = await api("GET", `/api/jobs/${id}/report`); } catch (e) { $("#view").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  const j = r.job, cuts = (r.mes || []).reduce((a, m) => a + m.cuts, 0), drops = (r.devices || []).reduce((a, d) => a + d.drops, 0);
  const recT = (r.decks || []).reduce((a, d) => a + d.recSeconds, 0);
  const me = (r.mes || []).map(m => {
    const max = Math.max(1, ...m.sources.map(s => s.seconds)), tot = m.sources.reduce((a, s) => a + s.seconds, 0) || 1;
    return `<h3>${esc(m.devName)} · M/E ${m.me + 1} — TEMPO NO AR POR FONTE (${m.cuts} cortes)</h3><table><thead><tr><th>FONTE</th><th style="width:45%"></th><th>TEMPO</th><th>%</th><th>ENTRADAS NO AR</th></tr></thead><tbody>
      ${m.sources.map(s => `<tr><td><b>${esc(s.name)}</b></td><td><span class="sbar" style="width:${(s.seconds / max * 100).toFixed(1)}%"></span></td><td class="num">${hms(s.seconds)}</td><td class="num">${(s.seconds / tot * 100).toFixed(1)}%</td><td class="num">${s.takes}</td></tr>`).join("")}</tbody></table>`;
  }).join("");
  $("#view").innerHTML = `<div class="noprint row"><a class="btn" href="#/jobs">← JOBS</a><a class="btn" href="#/resumo/${j.id}">VER RESUMO</a><span class="spacer"></span>${r.live ? `<button class="btn" onclick="staticRendered='';render()">ATUALIZAR</button>` : ""}<a class="btn" href="/api/jobs/${j.id}/csv">CSV</a><button class="btn red big" onclick="window.print()">IMPRIMIR / SALVAR PDF</button></div>
  <div class="report">
    <div class="rh"><img src="logo.png" alt=""><div><h1>RELATÓRIO DE JOB DE TRANSMISSÃO</h1><div style="color:#666;letter-spacing:.12em;font-size:12px;font-weight:700">RACK MONITOR BY DON · V${esc(r.version)} · ${esc(r.station)}</div></div></div>
    <div class="meta"><div><span>JOB</span><b>${esc(j.name)}</b></div><div><span>CLIENTE</span>${esc(j.client || "—")}</div><div><span>LOCAL</span>${esc(j.venue || "—")}</div><div><span>OPERADOR</span>${esc(j.operator || "—")}</div>
      <div><span>INÍCIO</span>${dt(r.from)}</div><div><span>FIM</span>${r.live ? "em andamento (" + dt(r.to) + ")" : dt(r.to)}</div><div><span>GERADO EM</span>${dt(r.generated)}</div><div><span>ID</span>${esc(j.id)}</div></div>
    <div class="rk"><div><span>DURAÇÃO</span><b>${hms(r.seconds)}</b></div><div><span>CORTES NO PGM</span><b>${cuts}</b></div><div><span>TEMPO GRAVADO (SOMA)</span><b>${hms(recT)}</b></div><div><span>QUEDAS DE CONEXÃO</span><b class="${drops ? "crit" : ""}">${drops}</b></div><div><span>EVENTOS CRÍTICOS</span><b class="${r.counts.crit ? "crit" : ""}">${r.counts.crit || 0}</b></div></div>
    ${j.notes ? `<h3>OBSERVAÇÕES</h3><div>${esc(j.notes)}</div>` : ""}
    ${me}
    <h3>GRAVAÇÕES</h3><table><thead><tr><th>GRAVADOR</th><th>TEMPO EM REC</th><th>INÍCIOS</th><th>PARADAS</th><th>TROCAS DE SLOT</th><th>INTERRUPÇÕES SEM COMANDO</th></tr></thead><tbody>${(r.decks || []).map(d => `<tr><td><b>${esc(d.devName)}</b></td><td class="num">${hms(d.recSeconds)}</td><td>${d.starts}</td><td>${d.stops}</td><td>${d.slotChanges || 0}</td><td class="${d.interrupted ? "crit" : ""}">${d.interrupted}</td></tr>`).join("") || `<tr><td colspan="6">Sem gravações registradas.</td></tr>`}</tbody></table>
    ${(r.timeline || []).some(e => e.type === "TROCA DE SLOT") ? `<h3>TROCAS DE SLOT</h3><table><tbody>${r.timeline.filter(e => e.type === "TROCA DE SLOT").map(e => `<tr><td class="num" style="width:150px">${dt(e.at)}</td><td style="width:240px"><b>${esc(e.devName)}</b></td><td>${esc(e.msg)}</td></tr>`).join("")}</tbody></table>` : ""}
    ${(r.ccu || []).length ? `<h3>CONTROLE DE CÂMERA — AJUSTES DO OPERADOR DE CCU</h3><table><thead><tr><th>CÂMERA</th><th>MESA</th><th>AJUSTES</th><th>O QUE FOI MEXIDO</th></tr></thead><tbody>${r.ccu.map(c => `<tr><td><b>${esc(c.camera)}</b></td><td>${esc(c.devName)}</td><td class="num">${c.count}</td><td>${Object.entries(c.params).sort((a, b) => b[1] - a[1]).map(([k, v]) => esc(k) + " " + v + "×").join(" · ")}</td></tr>`).join("")}</tbody></table>` : ""}
    ${(r.timeline || []).some(e => e.type === "MARCADOR") ? `<h3>MARCADORES DO OPERADOR</h3><table><tbody>${r.timeline.filter(e => e.type === "MARCADOR").map(e => `<tr><td class="num" style="width:90px">${tm(e.at)}</td><td style="width:160px"><b>${esc(e.data?.label || "")}</b></td><td>${esc(e.msg)}</td></tr>`).join("")}</tbody></table>` : ""}
    ${(r.timeline || []).some(e => e.type === "CHECKLIST") ? `<h3>CHECKLISTS</h3><table><tbody>${r.timeline.filter(e => e.type === "CHECKLIST").map(e => `<tr><td class="num" style="width:150px">${dt(e.at)}</td><td class="${e.sev === "crit" ? "crit" : ""}"><b>${esc(e.msg)}</b>${(e.data?.items || []).filter(i => i.status !== "ok").map(i => `<div style="font-size:11.5px;color:#555">• ${esc(i.item)}: ${esc(i.detail)}</div>`).join("")}</td></tr>`).join("")}</tbody></table>` : ""}
    <h3>SAÚDE DOS EQUIPAMENTOS</h3><table><thead><tr><th>EQUIPAMENTO</th><th>QUEDAS</th><th>TEMPO FORA</th><th>TROCAS (ROTA/AUX)</th><th>ALERTAS</th></tr></thead><tbody>${(r.devices || []).map(d => `<tr><td><b>${esc(d.devName)}</b></td><td class="${d.drops ? "crit" : ""}">${d.drops}</td><td class="num">${hms(d.offSeconds)}</td><td>${d.changes}</td><td>${d.alerts}</td></tr>`).join("")}</tbody></table>
    <h3>OCORRÊNCIAS CRÍTICAS (${(r.critical || []).length})</h3><table><tbody>${(r.critical || []).map(e => `<tr><td class="num" style="width:150px">${dt(e.at)}</td><td style="width:210px"><b>${esc(e.devName)}</b></td><td class="crit" style="width:220px">${esc(e.type)}</td><td>${esc(e.msg)}</td></tr>`).join("") || `<tr><td>Nenhuma ocorrência crítica registrada.</td></tr>`}</tbody></table>
    <h3>LINHA DO TEMPO COMPLETA (${(r.timeline || []).length} eventos)</h3><table><tbody>${(r.timeline || []).map(e => `<tr><td class="num" style="width:80px">${tm(e.at)}</td><td style="width:200px">${esc(e.devName)}</td><td style="width:190px" class="${e.sev === "crit" ? "crit" : ""}"><b>${esc(e.type)}</b></td><td>${esc(e.msg)}${e.origin ? ` <span style="color:#777">· origem: ${esc(e.origin)}</span>` : ""}</td></tr>`).join("")}</tbody></table>
    <div class="note">Tempos no ar calculados a partir das mudanças de PROGRAM recebidas da ATEM em tempo real; períodos com a mesa desconectada não são contabilizados. REC confirmado pelo protocolo HyperDeck (transporte e timecode) — não substitui a verificação dos arquivos.</div>
  </div>`;
}

// ---------------- RESUMO (1 página) ----------------
async function vResumo() {
  const id = params[0];
  if (staticRendered === "res:" + id) return;
  staticRendered = "res:" + id;
  $("#view").innerHTML = `<div class="empty">Calculando resumo…</div>`;
  let r;
  try { r = await api("GET", `/api/jobs/${id}/report`); } catch (e) { $("#view").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  const j = r.job, mes = r.mes || [];
  const cuts = mes.reduce((a, m) => a + m.cuts, 0), drops = (r.devices || []).reduce((a, d) => a + d.drops, 0);
  const recT = (r.decks || []).reduce((a, d) => a + d.recSeconds, 0), intr = (r.decks || []).reduce((a, d) => a + d.interrupted, 0);
  const used = new Set(); mes.forEach(m => m.sources.forEach(s => used.add(m.device + "|" + s.name)));
  const totalIn = id => { const a = dev(id)?.atem; return a ? Object.keys(a.inputs || {}).filter(k => +k > 0 && +k < 1000).length : 0; };
  const meBlock = m => {
    const tot = m.sources.reduce((a, s) => a + s.seconds, 0) || 1, ti = totalIn(m.device);
    return `<div class="rs-me"><div class="rs-mh"><b>${esc(m.devName)} · M/E ${m.me + 1}</b><span>${m.used} inputs usados${ti ? " de " + ti : ""} · ${m.cuts} cortes</span></div>
      <table><thead><tr><th>INPUT</th><th style="width:70px;text-align:right">VEZES</th><th style="width:90px;text-align:right">TEMPO</th><th style="width:60px;text-align:right">%</th></tr></thead><tbody>
      ${m.sources.map(s => `<tr><td>${esc(s.name)}</td><td class="num" style="text-align:right"><b>${s.takes}</b></td><td class="num" style="text-align:right">${hms(s.seconds)}</td><td class="num" style="text-align:right">${(s.seconds / tot * 100).toFixed(0)}%</td></tr>`).join("")}</tbody></table></div>`;
  };
  $("#view").innerHTML = `<div class="noprint row"><a class="btn" href="#/jobs">← JOBS</a><a class="btn ghost" href="#/report/${j.id}">RELATÓRIO COMPLETO</a><span class="spacer"></span>${r.live ? `<button class="btn" onclick="staticRendered='';render()">ATUALIZAR</button>` : ""}<a class="btn" href="/api/jobs/${j.id}/resumo.csv">CSV DO RESUMO</a><button class="btn red big" onclick="window.print()">IMPRIMIR / PDF</button></div>
  <div class="report resumo">
    <div class="rh"><img src="logo.png" alt=""><div style="flex:1"><h1>RESUMO DO JOB</h1><div style="font-size:15px;font-weight:700;margin-top:2px">${esc(j.name)}</div><div style="color:#666;font-size:12px">${esc([j.client, j.venue, j.operator].filter(Boolean).join(" · "))}</div></div>
      <div style="text-align:right;font-size:12px;color:#555">${dt(r.from).slice(0, 16)} → ${r.live ? "em andamento" : dt(r.to).slice(11, 16)}<br><b style="font-size:20px;color:#111">${hms(r.seconds)}</b></div></div>
    <div class="rk"><div><span>INPUTS USADOS</span><b>${used.size}</b></div><div><span>CORTES NO PGM</span><b>${cuts}</b></div><div><span>TEMPO GRAVADO</span><b>${hms(recT)}</b></div><div><span>QUEDAS</span><b class="${drops ? "crit" : ""}">${drops}</b></div><div><span>CRÍTICOS</span><b class="${r.counts.crit ? "crit" : ""}">${r.counts.crit || 0}</b></div></div>
    <div class="rs-grid">${mes.map(meBlock).join("") || `<div>Nenhuma troca de PGM registrada neste job.</div>`}</div>
    ${(r.decks || []).length ? `<h3>GRAVADORES</h3><div class="rs-decks">${r.decks.map(d => `<div><b>${esc(d.devName.replace(/^HyperDeck\s*-\s*/i, ""))}</b><span>${hms(d.recSeconds)}${d.slotChanges ? ` · ${d.slotChanges} troca(s) de slot` : ""}${d.interrupted ? ` · <em class="crit">${d.interrupted} interrupção(ões)</em>` : ""}</span></div>`).join("")}</div>` : ""}
    ${(r.timeline || []).some(e => e.type === "MARCADOR") ? `<h3>MARCADORES</h3><div class="rs-crit">${r.timeline.filter(e => e.type === "MARCADOR").map(e => `<div><span>${tm(e.at)}</span> <b>${esc(e.data?.label || "")}</b> — ${esc((e.msg || "").split(" · ").slice(1).join(" · "))}</div>`).join("")}</div>` : ""}
    ${(() => { const c = [...(r.timeline || [])].reverse().find(e => e.type === "CHECKLIST"); return c ? `<h3>ÚLTIMO CHECKLIST</h3><div class="${c.sev === "crit" ? "crit" : ""}"><b>${esc(c.msg)}</b> · ${tm(c.at)}</div>` : ""; })()}
    ${(r.ccu || []).length ? `<h3>AJUSTES DE CCU (CONTROLE DE CÂMERA)</h3><div class="rs-decks">${r.ccu.map(c => `<div><b>${esc(c.camera)}</b><span>${c.count} ajuste(s)</span></div>`).join("")}</div>` : ""}
    ${(r.timeline || []).some(e => e.type === "TROCA DE SLOT") ? `<h3>TROCAS DE SLOT</h3><div class="rs-crit">${r.timeline.filter(e => e.type === "TROCA DE SLOT").map(e => `<div><span>${tm(e.at)}</span> <b>${esc(e.devName)}</b> — ${esc(e.msg)}</div>`).join("")}</div>` : ""}
    ${(r.critical || []).length ? `<h3>OCORRÊNCIAS CRÍTICAS (${r.critical.length})</h3><div class="rs-crit">${r.critical.slice(0, 8).map(e => `<div><span>${tm(e.at)}</span> <b>${esc(e.devName)}</b> — ${esc(e.type)}</div>`).join("")}${r.critical.length > 8 ? `<div class="mu2">+ ${r.critical.length - 8} no relatório completo</div>` : ""}</div>` : `<h3>OCORRÊNCIAS CRÍTICAS</h3><div>Nenhuma. ${intr ? "" : "Gravações sem interrupção."}</div>`}
    <div class="note">VEZES = quantas vezes o input entrou no PROGRAM. TEMPO = soma do tempo no ar. Gerado em ${dt(r.generated)} · RACK MONITOR BY DON V${esc(r.version)}</div>
  </div>`;
}

// ---------------- INVENTÁRIO DE CARTÕES ----------------
async function vInventario() {
  const id = params[0];
  if (staticRendered === "inv:" + id) return;
  staticRendered = "inv:" + id;
  $("#view").innerHTML = `<div class="empty">Lendo os cartões dos HyperDecks…</div>`;
  let r;
  try { r = await api("GET", `/api/jobs/${id}/inventario`); } catch (e) { $("#view").innerHTML = `<div class="empty">${esc(e.message)}</div>`; return; }
  const j = r.job || {}, D = r.decks || [];
  const tclips = D.reduce((a, d) => a + (d.slots || []).reduce((b, s) => b + (s.clips || []).length, 0), 0);
  const tsec = D.reduce((a, d) => a + (d.slots || []).reduce((b, s) => b + s.seconds, 0), 0);
  const trec = D.reduce((a, d) => a + d.recSec, 0);
  const hm = t => valid(t) ? tm(t) : "—";
  const deck = d => {
    const slots = (d.slots || []).map(s => `<div class="inv-slot"><div class="inv-sh"><b>SLOT ${s.slot}</b> · ${esc(s.volume || "sem nome")} · ${esc(mediaTxt(s.status))}<span>${(s.clips || []).length} clipe(s) · ${hms(s.seconds)}</span></div>
      ${s.error ? `<div class="crit">${esc(s.error)}</div>` : ""}
      ${(s.clips || []).length ? `<table><thead><tr><th style="width:36px">Nº</th><th>CLIPE</th><th style="width:90px">DURAÇÃO</th><th style="width:160px">CODEC · FORMATO</th><th style="width:120px">REC (HORÁRIO)</th></tr></thead><tbody>
        ${s.clips.map(c => `<tr><td class="num">${c.id}</td><td><b>${esc(c.name)}</b></td><td class="num">${esc(c.duration || "")}</td><td class="xs">${esc([c.codec, c.format].filter(Boolean).join(" · "))}</td><td class="num">${valid(c.recStart) ? hm(c.recStart) + (valid(c.recStop) ? "–" + hm(c.recStop) : "") : "—"}</td></tr>`).join("")}</tbody></table>` : (s.status === "mounted" ? `<div class="mu2 sm">Cartão vazio.</div>` : "")}</div>`).join("");
    const ses = (d.sessions || []).length ? `<div class="inv-ses"><b>GRAVAÇÕES NESTE JOB:</b> ${d.sessions.map(s => `${s.initial ? "≤ " : ""}${hm(s.start)}–${s.open ? "em REC" : hm(s.stop)} (${hms(s.seconds)}${s.initial ? " · já gravava quando o monitor conectou" : ""}${(s.slots || []).length ? " · slot " + s.slots.join("→") : ""}${s.name ? " · " + esc(s.name) : ""})`).join(" · ")}</div>` : `<div class="inv-ses mu2">Nenhuma gravação registrada neste job.</div>`;
    return `<div class="inv-deck"><div class="inv-dh"><b>${esc(d.name)}</b><span>${esc([d.model, d.host].filter(Boolean).join(" · "))}</span></div>${d.note ? `<div class="inv-note">${esc(d.note)}</div>` : ""}${ses}${slots}</div>`;
  };
  $("#view").innerHTML = `<div class="noprint row"><a class="btn" href="#/jobs">← JOBS</a><a class="btn ghost" href="#/gravacoes">GRAVADORES</a><span class="spacer"></span>${r.live ? `<button class="btn" onclick="staticRendered='';render()">LER CARTÕES DE NOVO</button>` : ""}<a class="btn" href="/api/jobs/${esc(id)}/inventario.csv">CSV</a><button class="btn red big" onclick="window.print()">IMPRIMIR / PDF</button></div>
  <div class="report resumo inv">
    <div class="rh"><img src="logo.png" alt=""><div style="flex:1"><h1>INVENTÁRIO DE CARTÕES</h1><div style="font-size:15px;font-weight:700;margin-top:2px">${esc(j.name || "")}</div><div style="color:#666;font-size:12px">${esc([j.client, j.venue, j.operator].filter(Boolean).join(" · "))}</div></div>
      <div style="text-align:right;font-size:12px;color:#555">${r.live ? "lido agora" : "foto tirada ao encerrar o job"}<br>${dt(r.generated).slice(0, 16)}</div></div>
    <div class="rk"><div><span>GRAVADORES</span><b>${D.length}</b></div><div><span>CLIPES NOS CARTÕES</span><b>${tclips}</b></div><div><span>DURAÇÃO NOS CARTÕES</span><b>${hms(tsec)}</b></div><div><span>TEMPO EM REC NO JOB</span><b>${hms(trec)}</b></div></div>
    ${r.snapshot ? "" : `<div class="inv-note">Lista de clipes não disponível para este job (encerrado em versão anterior). Abaixo, só as gravações registradas no log.</div>`}
    ${D.map(deck).join("") || `<div>Nenhum HyperDeck.</div>`}
    <div class="note">Clipes e durações lidos dos cartões pelo protocolo HyperDeck (disk list). Horário de REC cruzado pelo nome do clipe quando o REC foi dado pelo monitor. Confira as mídias antes de liberar os cartões. Gerado por RACK MONITOR BY DON V${esc(r.version)}</div>
    <div class="inv-sign"><div>ENTREGUE POR</div><div>RECEBIDO POR</div><div>DATA / HORA</div></div>
  </div>`;
}

// ---------------- RELATÓRIO DE I/O ----------------
let ioSel = null;
function ioData(d) {
  const rows = [];
  if (d.atem) {
    const a = d.atem, ac = auxCfgOf(d.id);
    const kind = id => id === 0 ? "Black" : id < 1000 ? "Entrada" : id < 2000 ? "Barras" : id < 3000 ? "Cor" : id < 4000 ? "Media Player" : id >= 6000 && id < 7000 ? "Super Source" : id >= 7000 && id < 8000 ? "Clean Feed" : id >= 8000 && id < 9000 ? "Saída AUX" : id >= 10000 ? "M/E" : "Interno";
    Object.values(a.inputs || {}).filter(i => i.id < 8000 || i.id >= 9000).sort((x, y) => x.id - y.id).forEach(i => {
      const onPgm = Object.entries(a.mes || {}).filter(([, m]) => m.program === i.id).map(([k]) => "PGM M/E" + (+k + 1));
      const onAux = Object.entries(a.aux || {}).filter(([, s]) => s === i.id).map(([k]) => "AUX " + (+k + 1));
      rows.push({ sec: "INPUTS", n: i.id, tipo: kind(i.id), nome: i.long, curto: i.short, rota: [...onPgm, ...onAux].join(", ") });
    });
    Object.entries(a.aux || {}).sort((x, y) => x[0] - y[0]).forEach(([k, s]) => {
      const c = ac[k] || {};
      rows.push({ sec: "SAÍDAS AUX", n: +k + 1, tipo: c.mode === "ignore" ? "ignorado" : c.mode === "lock" ? "travado" : "monitorado", nome: auxName(d, k) || "AUX " + (+k + 1), curto: "", rota: `${s} · ${a.inputs?.[s]?.long || ""}` });
    });
    Object.entries(a.mes || {}).forEach(([k, m]) => rows.push({ sec: "M/E", n: +k + 1, tipo: "M/E", nome: "M/E " + (+k + 1), curto: "", rota: `PGM ${a.inputs?.[m.program]?.long || m.program} · PVW ${a.inputs?.[m.preview]?.long || m.preview}` }));
  }
  if (d.hub) {
    const hb = d.hub;
    Object.keys(hb.inLabels || {}).map(Number).sort((a, b) => a - b).forEach(i => {
      const outs = Object.entries(hb.routes || {}).filter(([, v]) => v === i).map(([o]) => +o + 1);
      rows.push({ sec: "ENTRADAS", n: i + 1, tipo: "Entrada", nome: hb.inLabels[i], curto: "", rota: outs.length ? "→ saídas " + outs.join(", ") : "" });
    });
    Object.keys(hb.outLabels || {}).map(Number).sort((a, b) => a - b).forEach(o => {
      const i = hb.routes?.[o];
      rows.push({ sec: "SAÍDAS", n: o + 1, tipo: "Saída", nome: hb.outLabels[o], curto: hb.locks?.[o] && hb.locks[o] !== "U" ? "LOCK" : "", rota: i === undefined ? "" : `← ${i + 1} · ${hb.inLabels?.[i] || ""}` });
    });
  }
  return rows;
}
function vIO() {
  const L = S.devices.filter(d => (d.kind === "atem" || d.kind === "videohub"));
  if (!ioSel) ioSel = new Set(L.map(d => d.id));
  if (staticRendered === "io" && pointerDown) return;
  if (staticRendered === "io") return;
  staticRendered = "io";
  const sel = L.filter(d => ioSel.has(d.id));
  const block = d => {
    if (!d.online || !(d.atem || d.hub)) return `<h3>${esc(d.name)}</h3><div class="crit">Sem conexão — dados indisponíveis (${esc(d.status)}).</div>`;
    const rows = ioData(d), secs = [...new Set(rows.map(r => r.sec))];
    const info = d.atem ? `${esc(d.atem.product)} · ${Object.keys(d.atem.mes || {}).length} M/E · ${Object.keys(d.atem.aux || {}).length} AUX` : `${esc(d.hub.model)} · ${d.hub.inputs}×${d.hub.outputs}`;
    return `<h3>${esc(d.name)} <span style="color:#666;font-weight:400;letter-spacing:0">· ${info} · ${esc(d.host)}</span></h3>` + secs.map(sc => `<div class="io-sec">${sc}</div><table class="io"><thead><tr><th style="width:60px">Nº</th><th style="width:110px">TIPO</th><th>NOME</th>${d.atem && sc === "INPUTS" ? `<th style="width:70px">CURTO</th>` : d.hub && sc === "SAÍDAS" ? `<th style="width:60px"></th>` : ""}<th>${d.hub ? "ROTA" : sc === "INPUTS" ? "NO AR AGORA EM" : "FONTE ATUAL"}</th></tr></thead><tbody>
      ${rows.filter(r => r.sec === sc).map(r => `<tr><td class="num"><b>${r.n}</b></td><td>${esc(r.tipo)}</td><td>${esc(r.nome)}</td>${(d.atem && sc === "INPUTS") || (d.hub && sc === "SAÍDAS") ? `<td>${esc(r.curto)}</td>` : ""}<td>${esc(r.rota)}</td></tr>`).join("")}</tbody></table>`).join("");
  };
  setTimeout(patchList, 50);
  $("#view").innerHTML = `<div class="noprint card" style="margin-bottom:12px"><h2>COMPARAR MONTAGEM (FOTO DO PATCH)</h2><div class="row"><input id="pn" placeholder="nome da foto (ex.: Arena · montagem ok)" style="flex:1"><button class="btn red" onclick="patchSave()">📸 SALVAR FOTO AGORA</button><select id="pf" style="flex:1"><option value="">comparar com…</option></select><button class="btn" onclick="patchDiff()">COMPARAR COM AGORA</button></div><div class="hint" style="margin-top:6px">Salve uma foto quando a montagem estiver certa. No próximo evento, compare: o monitor lista cada nome de input, AUX, label e rota da VideoHub que mudou.</div><div id="pdiff" style="margin-top:10px"></div></div>
  <div class="noprint card" style="margin-bottom:12px"><div class="row"><b style="letter-spacing:.1em">INCLUIR:</b>${L.map(d => `<label class="chk"><input type="checkbox" ${ioSel.has(d.id) ? "checked" : ""} onchange="ioSel[this.checked?'add':'delete']('${d.id}');staticRendered='';render()">${esc(d.name)}</label>`).join("")}</div>
    <div class="row" style="margin-top:10px"><a class="btn" href="#/jobs">← JOBS</a><button class="btn" onclick="staticRendered='';render()">ATUALIZAR AGORA</button><span class="spacer"></span><button class="btn" onclick="ioCSV()">BAIXAR CSV</button><button class="btn red big" onclick="window.print()">IMPRIMIR / PDF</button></div></div>
  <div class="report">
    <div class="rh"><img src="logo.png" alt=""><div><h1>ENTRADAS E SAÍDAS · ATEM + VIDEOHUB</h1><div style="color:#666;letter-spacing:.12em;font-size:12px;font-weight:700">FOTO DO PATCH EM ${dt(new Date())} · ${esc(S.station)}${S.job ? " · " + esc(S.job.name) : ""}</div></div></div>
    ${sel.map(block).join("") || `<div style="margin-top:20px">Selecione ao menos um equipamento.</div>`}
    <div class="note">Nomes e rotas lidos ao vivo dos equipamentos no momento da geração. Numeração das VideoHubs começa em 1 (como no painel da Blackmagic).</div>
  </div>`;
}
async function patchList() { const l = await api("GET", "/api/patch").catch(() => []); const s = $("#pf"); if (s) s.innerHTML = `<option value="">comparar com…</option>` + l.map(p => `<option value="${esc(p.file)}">${esc(p.name)} · ${dt(p.at).slice(0, 16)}</option>`).join(""); }
window.patchSave = async () => { const r = await api("POST", "/api/patch", { name: $("#pn").value }); toast("Foto salva.", "ok"); await patchList(); $("#pf").value = r.file; };
window.patchDiff = async () => {
  const f = $("#pf").value; if (!f) { toast("Escolha uma foto.", "warn"); return; }
  const r = await api("GET", `/api/patch/${encodeURIComponent(f)}/diff`);
  $("#pdiff").innerHTML = r.diff.length ? `<div class="mu sm" style="margin-bottom:6px"><b style="color:var(--warn)">${r.diff.length} diferença(s)</b> em relação a "${esc(r.name)}"</div><div class="scroll" style="max-height:340px"><table><thead><tr><th>EQUIPAMENTO</th><th>ITEM</th><th>ANTES</th><th>AGORA</th></tr></thead><tbody>${r.diff.map(x => `<tr><td>${esc(x.device)}</td><td><b>${esc(x.item)}</b></td><td class="mu">${esc(x.before)}</td><td style="color:var(--warn)">${esc(x.after)}</td></tr>`).join("")}</tbody></table></div>` : `<div class="okbig" style="padding:12px"><b style="font-size:18px">MONTAGEM IGUAL À FOTO</b></div>`;
};
window.ioCSV = () => {
  const L = S.devices.filter(d => ioSel?.has(d.id) && (d.atem || d.hub));
  const q = v => `"${String(v ?? "").replace(/"/g, '""')}"`;
  const lines = [["equipamento", "secao", "numero", "tipo", "nome", "curto", "rota"].join(";")];
  L.forEach(d => ioData(d).forEach(r => lines.push([d.name, r.sec, r.n, r.tipo, r.nome, r.curto, r.rota].map(q).join(";"))));
  const blob = new Blob(["\ufeff" + lines.join("\r\n")], { type: "text/csv;charset=utf-8" });
  const a = document.createElement("a"); a.href = URL.createObjectURL(blob);
  const n = new Date(); a.download = `IO_${n.getFullYear()}${pad(n.getMonth() + 1)}${pad(n.getDate())}_${pad(n.getHours())}${pad(n.getMinutes())}.csv`; a.click();
};

// ---------------- RELÓGIO / RUNDOWN ----------------
const mmss = s => { const neg = s < 0; s = Math.abs(Math.round(s)); const h = Math.floor(s / 3600); return (neg ? "−" : "") + (h ? h + ":" + pad(Math.floor(s / 60) % 60) : Math.floor(s / 60)) + ":" + pad(s % 60); };
let rdEdit = null;
function vClock() {
  const tela = params[0] === "tela";
  const r = S.rundown || { items: [], current: -1 };
  const items = r.items || [];
  const now = Date.now(), d = new Date();
  const started = r.current >= 0 && valid(r.startedAt), ended = valid(r.ended);
  const cur = started && r.current < items.length ? items[r.current] : null;
  const prog = started ? ((ended ? new Date(r.ended) : now) - new Date(r.startedAt)) / 1000 : 0;
  const inBlock = cur ? (now - new Date(r.blockAt)) / 1000 : 0;
  const free = cur && !cur.dur; // bloco sem duração = cronômetro progressivo
  const left = cur ? (free ? inBlock : cur.dur - inBlock) : 0;
  const planned = items.slice(0, Math.max(0, r.current)).reduce((a, x) => a + x.dur, 0) + (cur ? Math.min(inBlock, cur.dur) : 0);
  const drift = started && !ended ? prog - planned - (cur && inBlock > cur.dur ? 0 : 0) : 0;
  const delay = started ? prog - (items.slice(0, Math.max(0, r.current)).reduce((a, x) => a + x.dur, 0) + inBlock) : 0;
  const next = started && r.current + 1 < items.length ? items[r.current + 1] : null;
  const live = `<div class="clk">
    <div class="clk-now"><small>HORA</small><b class="num">${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}</b></div>
    <div class="clk-blk ${free ? "" : cur && left < 0 ? "over" : cur && left < 30 ? "soon" : ""}"><small>${cur ? "BLOCO " + (r.current + 1) + "/" + items.length + " · " + esc(cur.title) : ended ? "PROGRAMA ENCERRADO" : "AGUARDANDO INÍCIO"}</small><b class="num">${cur ? mmss(left) : "--:--"}</b><span>${cur ? (free ? "tempo no bloco (sem duração definida)" : left < 0 ? "ESTOURADO" : "restante no bloco") : ""}</span></div>
    <div class="clk-side"><div><small>NO AR HÁ</small><b class="num">${started ? mmss(prog) : "--:--"}</b></div><div><small>PRÓXIMO</small><b>${next ? esc(next.title) + ` <span class="num mu">${next.dur ? mmss(next.dur) : "livre"}</span>` : "—"}</b></div>
      <div><small>RITMO</small><b class="num" style="color:${Math.abs(delay) < 15 ? "var(--ok)" : delay > 0 ? "var(--red)" : "var(--warn)"}">${started && !ended ? (Math.abs(delay) < 15 ? "NO TEMPO" : (delay > 0 ? "ATRASADO " : "ADIANTADO ") + mmss(Math.abs(delay))) : "—"}</b></div></div></div>
    <div class="row clkbtn">${!started || ended ? `<button class="btn red big" onclick="rd('start')">▶ INICIAR PROGRAMA</button>` : `<button class="btn big" onclick="rd('prev')">◀ BLOCO ANTERIOR</button><button class="btn red big" style="flex:1" onclick="rd('next')">${next ? "PRÓXIMO BLOCO ▶ " + esc(next.title) : "■ ENCERRAR PROGRAMA"}</button>`}
      ${tela ? `<a class="btn ghost" href="#/relogio">SAIR DA TELA CHEIA</a>` : `<a class="btn ghost" href="#/relogio/tela">TELA CHEIA</a>`}</div>
    <div class="rdlist">${items.map((x, i) => { const st = i < r.current ? "done" : i === r.current && !ended ? "cur" : ""; const lg = (r.log || []).find(l => l.index === i);
      return `<div class="${st}"><span class="num">${i + 1}</span><b>${esc(x.title)}</b><span class="num mu">${x.dur ? mmss(x.dur) : "livre"}</span><span class="mu xs">${lg ? "início " + tm(lg.at) : ""}</span></div>`; }).join("") || `<div class="mu">Monte o roteiro abaixo.</div>`}</div>`;
  if (tela) { $("#view").innerHTML = live; return; }
  if (staticRendered !== "clk") {
    staticRendered = "clk";
    rdEdit = (items.length ? items : [{ title: "ABERTURA", dur: 120 }, { title: "BLOCO 1", dur: 900 }, { title: "INTERVALO", dur: 300 }]).map(x => Object.assign({}, x));
    $("#view").innerHTML = `<div id="clklive"></div><div class="card" style="margin-top:14px"><h2>ROTEIRO (RUNDOWN) DESTE JOB <span class="r"><button class="btn" onclick="rdEdit.push({title:'NOVO BLOCO',dur:300});drawRd()">+ BLOCO</button><button class="btn red" onclick="rdSave()">SALVAR ROTEIRO</button></span></h2>
      <div class="hint"><b style="color:var(--ok)">Salva sozinho</b> a cada alteração. Duração vazia = cronômetro livre (conta para cima). Cada início de bloco vira um MARCADOR com o timecode dos gravadores (entra na EDL e nos relatórios). Stream Deck: GET <code>http://${esc(location.hostname)}:${location.port || 80}/api/rundown/next</code> (também <code>start</code>, <code>prev</code>).</div>
      <div id="rdedit" style="margin-top:10px"></div></div>`;
    drawRd(true);
    if (!items.length) rdAuto(); // grava o roteiro sugerido para o INICIAR já funcionar
  }
  $("#clklive").innerHTML = live;
}
function drawRd(skipSave) {
  if (!skipSave) rdAuto();
  $("#rdedit").innerHTML = rdEdit.map((x, i) => `<div class="row" style="margin-bottom:6px"><span class="num mu" style="width:24px">${i + 1}</span><input value="${esc(x.title)}" style="flex:1" onchange="rdEdit[${i}].title=this.value;rdAuto()"><input value="${x.dur ? mmss(x.dur) : ""}" placeholder="livre" style="width:90px" title="duração (min:seg ou h:mm:ss) — vazio = cronômetro livre" onchange="rdEdit[${i}].dur=parseDur(this.value);drawRd()"><button class="btn ghost" onclick="if(${i}>0){[rdEdit[${i}-1],rdEdit[${i}]]=[rdEdit[${i}],rdEdit[${i}-1]];drawRd()}">↑</button><button class="btn ghost" onclick="rdEdit.splice(${i},1);drawRd()">✕</button></div>`).join("") + `<div class="mu sm">Total planejado: <b class="num">${mmss(rdEdit.reduce((a, x) => a + x.dur, 0))}</b></div>`;
}
window.drawRd = drawRd;
window.parseDur = v => { const p = String(v).split(":").map(Number); return p.reduce((a, x) => a * 60 + (x || 0), 0); };
window.rdSave = async (quiet) => { try { await api("PUT", "/api/rundown", { items: (rdEdit || []).filter(x => x.title.trim()) }); if (!quiet) toast("Roteiro salvo.", "ok"); } catch (e) { toast(esc(e.message), "crit"); } };
let rdT = 0;
function rdAuto() { clearTimeout(rdT); rdT = setTimeout(() => rdSave(true), 400); }
window.rd = async a => {
  try {
    if (a === "start" && rdEdit && rdEdit.length) await rdSave(true); // o que está na tela é o que vale
    await api("POST", "/api/rundown/" + a);
  } catch (e) { toast(esc(e.message), "warn"); }
};
setInterval(() => { if (view === "relogio") render(); }, 500);

// ---------------- PÓS-PRODUÇÃO ----------------
let posJobs = null;
async function vPos() {
  if (staticRendered !== "pos") {
    staticRendered = "pos";
    posJobs = await api("GET", "/api/jobs").catch(() => []);
    const atems = devs("atem"), decks = recorders();
    const pairDeck = (S.pairs || [])[0]?.primary || decks[0]?.id || "";
    $("#view").innerHTML = `<div class="cfg">
    <div class="card wide"><h2>MARCADORES DO OPERADOR <span class="mu xs" style="letter-spacing:.04em">clique ou use as teclas 1–9 nesta tela · grava o timecode de todos os HyperDecks no instante</span></h2>
      <div class="mk">${(S.markerPresets || []).map((p, i) => `<button class="mkb" onclick="mark('${jsq(p)}')"><small>${i < 9 ? i + 1 : ""}</small>${esc(p)}</button>`).join("")}</div>
      <div class="row" style="margin-top:10px"><input id="mkl" placeholder="marcador livre (ex.: ENTREVISTA TÉCNICO)" style="flex:1"><input id="mkn" placeholder="observação (opcional)" style="flex:1"><button class="btn red big" onclick="mark($('#mkl').value,$('#mkn').value);$('#mkl').value='';$('#mkn').value=''">● MARCAR</button></div>
      <div id="tcbar" class="tcbar"></div>
      <div class="hint" style="margin-top:8px"><b>Stream Deck / Companion:</b> módulo <i>Generic HTTP</i> → ação GET em <code>http://${esc(location.hostname)}:${location.port || 80}/api/marker?label=GOL</code> (troque GOL pelo texto do botão). Cada toque vira um marcador com TC.</div>
      <h2 style="margin-top:14px">MARCADORES DESTE JOB</h2><div id="mklist" class="feed card" style="padding:4px 10px"></div></div>

    <div class="card"><h2>EDL DO CORTE (DaVinci / Premiere / Avid)</h2>
      <div class="hint">Gera uma EDL CMX 3600 com cada corte do PROGRAM e o timecode do gravador de referência. Com as câmeras gravadas em ISO no mesmo TC, o editor recebe o corte do diretor pronto para conformar. Marcadores entram como comentários <code>LOC</code>.</div>
      <div class="form" style="margin-top:10px">
        <label>JOB<select id="ej">${posJobs.map(j => `<option value="${j.id}">${esc(j.name)} · ${dt(j.opened).slice(0, 16)}</option>`).join("")}</select></label>
        <label>ATEM<select id="ea">${atems.map(d => `<option value="${d.id}">${esc(d.name)}</option>`).join("")}</select></label>
        <label>M/E<select id="em">${[0, 1, 2, 3].map(i => `<option value="${i}">M/E ${i + 1}</option>`).join("")}</select></label>
        <label>TC DE REFERÊNCIA<select id="ed">${decks.map(d => `<option value="${d.id}" ${d.id === pairDeck ? "selected" : ""}>${esc(d.name)}</option>`).join("")}</select></label></div>
      <div class="row" style="margin-top:12px"><button class="btn red" onclick="edl()">BAIXAR EDL</button><a class="btn" id="mkcsv" href="#">MARCADORES (CSV)</a><span id="edlinfo" class="mu sm"></span></div>
      <div class="hint" style="margin-top:8px">Precisão típica de 1–2 frames (o TC é interpolado entre leituras). O gravador de referência precisa estar em REC durante o trecho; trechos sem REC ficam fora da EDL.</div></div>

    <div class="card"><h2>CLIPES GRAVADOS POR CARTÃO</h2>
      <div class="row"><select id="cd" style="flex:1">${devs("hyperdeck").map(d => `<option value="${d.id}">${esc(d.name)}</option>`).join("")}</select><button class="btn red" onclick="loadClips()">LER CLIPES</button><button class="btn" onclick="clipsCSV()">CSV DE TODOS</button></div>
      <div id="clips" style="margin-top:10px"><div class="empty">Escolha um gravador e clique em LER CLIPES.</div></div></div>
    </div>`;
    $("#ej").onchange = () => { $("#mkcsv").href = `/api/jobs/${$("#ej").value}/markers.csv`; };
    $("#ej").onchange();
  }
  posLive();
}
function posLive() {
  const bar = $("#tcbar"); if (!bar) return;
  bar.innerHTML = recorders().filter(d => d.online && d.deck).map(d => `<div class="${d.deck.transport === "record" ? "rec" : ""}"><small>${esc(d.name.replace(/^HyperDeck\s*-\s*/i, ""))}</small><b class="num">${esc(d.deck.tc || "--")}</b></div>`).join("");
  const ml = EV.filter(e => e.type === "MARCADOR").slice(0, 40);
  $("#mklist").innerHTML = ml.map(e => `<div class="ev warn"><span class="t">${tm(e.at)}</span><span><b>${esc(e.data?.label || "MARCADOR")}</b>${e.origin ? " " + originTag(e.origin) : ""}<br><span class="sm">${esc(e.msg)}</span></span></div>`).join("") || `<div class="empty" style="margin:8px 0">Nenhum marcador ainda.</div>`;
}
window.mark = async (label, note = "") => {
  try { const r = await api("POST", "/api/marker", { label, note }); toast("◆ " + esc(r.msg), "ok"); } catch (e) { toast(esc(e.message), "crit"); }
};
document.addEventListener("keydown", e => {
  if (view !== "pos" || e.target.matches("input,textarea,select") || e.ctrlKey || e.metaKey || e.altKey) return;
  const n = +e.key; const p = S?.markerPresets || [];
  if (n >= 1 && n <= 9 && p[n - 1]) { e.preventDefault(); mark(p[n - 1]); }
});
window.edl = async () => {
  const q = `atem=${$("#ea").value}&me=${$("#em").value}&deck=${$("#ed").value}`, j = $("#ej").value;
  try {
    const c = await api("GET", `/api/jobs/${j}/edl?${q}&check=1`);
    $("#edlinfo").textContent = `${c.events} eventos de corte`;
    if (!c.events) { toast("Nenhum corte com o gravador de referência em REC nesse job/M-E.", "warn"); return; }
    location.href = `/api/jobs/${j}/edl?${q}`;
  } catch (e) { toast(esc(e.message), "crit"); }
};
let lastClips = {};
window.loadClips = async () => {
  const id = $("#cd").value; $("#clips").innerHTML = `<div class="empty">Lendo…</div>`;
  try {
    const r = await api("GET", `/api/decks/${id}/clips`); lastClips[id] = r;
    $("#clips").innerHTML = r.map(s => `<div class="mu xs" style="margin:8px 0 4px;letter-spacing:.1em"><b>SLOT ${s.slot}</b> · ${esc(s.volume || "")} · ${esc(mediaTxt(s.status))}${s.error ? ` · <span style="color:var(--red)">${esc(s.error)}</span>` : ""}</div>
      ${(s.clips || []).length ? `<table><thead><tr><th>#</th><th>CLIPE</th><th>FORMATO</th><th>DURAÇÃO</th></tr></thead><tbody>${s.clips.map(c => `<tr><td class="num">${c.id}</td><td><b>${esc(c.name)}</b></td><td class="mu sm">${esc([c.codec, c.format].filter(Boolean).join(" · "))}</td><td class="num">${esc(c.duration)}</td></tr>`).join("")}</tbody></table>` : `<div class="mu sm">sem clipes</div>`}`).join("");
  } catch (e) { $("#clips").innerHTML = `<div class="empty">${esc(e.message)}</div>`; }
};
window.clipsCSV = async () => {
  const q = v => `"${String(v ?? "").replace(/"/g, '""')}"`;
  const lines = [["gravador", "slot", "cartao", "id", "clipe", "codec", "formato", "duracao"].join(";")];
  for (const d of devs("hyperdeck").filter(x => x.online)) {
    const r = await api("GET", `/api/decks/${d.id}/clips`).catch(() => []);
    r.forEach(s => (s.clips || []).forEach(c => lines.push([d.name, s.slot, s.volume, c.id, c.name, c.codec, c.format, c.duration].map(q).join(";"))));
  }
  const a = document.createElement("a"); a.href = URL.createObjectURL(new Blob(["\ufeff" + lines.join("\r\n")], { type: "text/csv" }));
  a.download = "clipes_" + new Date().toISOString().slice(0, 10) + ".csv"; a.click();
};

// ---------------- CHECKLIST ----------------
function lastChecklist() { return EV.find(e => e.type === "CHECKLIST" && e.data?.items); }
function vChecklist() {
  const c = lastChecklist();
  const items = c?.data?.items || [];
  const groups = [...new Set(items.map(i => i.group))];
  const cls = !c ? "" : c.sev === "crit" ? "crit" : c.sev === "warn" ? "warn" : "ok";
  const ic = { ok: "✓", warn: "!", crit: "✕" };
  $("#view").innerHTML = `<div class="row ckbar" style="margin-bottom:12px;flex-wrap:nowrap"><button class="btn red big" style="white-space:nowrap" onclick="runCheck()">▶ RODAR CHECKLIST AGORA</button><span class="hint">Confere rede, mídia, REC, par principal/backup, rotas e AUX travados, FTB e stream. O resultado fica salvo no job e entra no relatório. Também pode ser agendado em CONFIGURAÇÃO → AGENDA.</span><span class="spacer"></span>${c ? `<button class="btn" onclick="window.print()">IMPRIMIR / PDF</button>` : ""}</div>
  ${c ? `<div class="ckv ${cls}"><b>${esc(c.data.verdict)}</b><span>${dt(c.at)} · ${items.length} itens · ${esc(c.origin || "")}</span></div>
    <div class="ckg">${groups.map(g => `<div class="card"><h2>${esc(g.toUpperCase())}</h2>${items.filter(i => i.group === g).sort((a, b) => rank[b.status === "ok" ? "info" : b.status] - rank[a.status === "ok" ? "info" : a.status]).map(i => `<div class="cki ${i.status}"><span class="ci">${ic[i.status]}</span><div><b>${esc(i.item)}</b><div class="mu sm">${esc(i.detail)}</div></div></div>`).join("")}</div>`).join("")}</div>`
    : `<div class="empty">Nenhum checklist neste job ainda. Rode antes de entrar no ar.</div>`}`;
}
window.runCheck = async () => { try { const r = await api("POST", "/api/checklist"); toast(esc(r.verdict), r.verdict.startsWith("PRONTO PARA") ? "ok" : r.verdict.startsWith("NÃO") ? "crit" : "warn"); setTimeout(render, 300); } catch (e) { toast(esc(e.message), "crit"); } };

// ---------------- SAÚDE ----------------
let HEALTH = null, healthT = 0;
function spark(samples, warn) {
  if (!samples?.length) return `<span class="mu xs">coletando…</span>`;
  const W = 260, H = 34, n = samples.length, mx = Math.max(warn || 1, ...samples.map(s => s.lat || 0)) * 1.1;
  const x = i => (n === 1 ? W : (i / (n - 1)) * W).toFixed(1), y = v => (H - 2 - (v / mx) * (H - 4)).toFixed(1);
  const line = samples.map((s, i) => `${i ? "L" : "M"}${x(i)},${y(s.up ? s.lat : 0)}`).join("");
  const off = samples.map((s, i) => s.up ? "" : `<rect x="${x(i)}" y="0" width="${Math.max(2, W / n)}" height="${H}" fill="var(--red)" opacity=".55"/>`).join("");
  return `<svg width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" class="spark">${warn ? `<line x1="0" x2="${W}" y1="${y(warn)}" y2="${y(warn)}" stroke="var(--warn)" stroke-dasharray="3 3" opacity=".5"/>` : ""}${off}<path d="${line}" fill="none" stroke="var(--ok)" stroke-width="1.5"/></svg>`;
}
async function vSaude() {
  if (staticRendered !== "saude" || Date.now() - healthT > 10000) {
    staticRendered = "saude"; healthT = Date.now();
    HEALTH = await api("GET", "/api/health").catch(() => HEALTH);
  }
  if (!HEALTH) return;
  const dg = HEALTH.diag || [], st = dg[0] || {}, files = dg.slice(1);
  (HEALTH.devices || []).forEach(d => { d.samples = d.samples || []; d.unknown = d.unknown || []; });
  HEALTH.devices = HEALTH.devices || [];
  $("#view").innerHTML = `<div class="cfg">
  <div class="card wide"><h2>GRAVADOR DE DIAGNÓSTICO</h2>
    <div class="hint">Grava tudo o que ATEM, HyperDecks e VideoHubs mandam pela rede (somente leitura). Use no primeiro dia no rack real — e mexa no Camera Control Panel, troque AUX e corte durante a captura. Mande o arquivo para ajustar o que for experimental.</div>
    <div class="row" style="margin-top:10px">${st.running ? `<span class="tag crit">● GRAVANDO · faltam ${st.left}s</span><button class="btn" onclick="api('POST','/api/diag/stop').then(()=>{healthT=0;render()})">PARAR</button>` : [30, 60, 120, 300].map(s => `<button class="btn ${s === 60 ? "red" : ""}" onclick="diagStart(${s})">● CAPTURAR ${s < 60 ? s + " s" : s / 60 + " min"}</button>`).join("")}</div>
    ${files.length ? `<table style="margin-top:10px"><tbody>${files.slice(0, 8).map(f => `<tr><td><b>${esc(f.name)}</b></td><td class="mu sm">${dt(f.at)}</td><td class="num">${(f.size / 1024).toFixed(0)} KB</td><td style="text-align:right"><a class="btn" href="/api/diag/${encodeURIComponent(f.name)}">BAIXAR</a></td></tr>`).join("")}</tbody></table>` : ""}</div>
  <div class="card wide"><h2>LATÊNCIA E DISPONIBILIDADE · ÚLTIMAS 2 H <span class="mu xs" style="letter-spacing:.04em">vermelho = fora do ar · tracejado = limite de latência</span></h2>
    <table><thead><tr><th>EQUIPAMENTO</th><th>AGORA</th><th>DISPONÍVEL</th><th>QUEDAS</th><th>HISTÓRICO</th><th>DETALHES</th></tr></thead><tbody>
    ${HEALTH.devices.map(d => { const up = d.samples.length ? d.samples.filter(s => s.up).length / d.samples.length * 100 : 0;
      return `<tr><td><span class="dot ${d.online ? "ok" : "crit"}"></span> <b>${esc(d.name)}</b><div class="mu xs">${esc(kindName[d.kind] || d.kind)}</div></td><td class="num">${d.online ? d.lat.toFixed(1) + " ms" : `<span style="color:var(--red)">OFFLINE</span>`}</td>
        <td class="num" style="color:${up < 99 ? "var(--warn)" : "var(--ok)"}">${d.samples.length ? up.toFixed(1) + "%" : "—"}</td><td class="num">${d.reconnects}</td><td>${spark(d.samples, S.rules.latencyWarnMs)}</td>
        <td class="mu xs">${esc(d.extra || "")}${d.unknown?.length ? `<div style="margin-top:3px">comandos ainda não interpretados: <span style="font-family:var(--mono)">${esc(d.unknown.slice(0, 18).join(" "))}${d.unknown.length > 18 ? " …" : ""}</span></div>` : ""}</td></tr>`; }).join("")}</tbody></table></div></div>`;
}
window.diagStart = async s => { try { await api("POST", "/api/diag/start", { seconds: s }); toast(`Capturando ${s} s — opere normalmente.`, "ok"); healthT = 0; render(); } catch (e) { toast(esc(e.message), "crit"); } };
setInterval(() => { if (view === "saude") { healthT = 0; render(); } }, 10000);

// ---------------- CONFIGURAÇÃO ----------------
let CFG = null, showPorts = false;
async function vConfig() {
  if (staticRendered === "cfg") return;
  staticRendered = "cfg";
  const r = await api("GET", "/api/config"); CFG = r.config; CFG._pinSet = r.pinSet; CFG._local = r.local; CFG._dir = r.dataDir;
  const net = await api("GET", "/api/netinfo").catch(() => ({ ips: [] }));
  CFG._prefix = (net.ips.find(i => i.startsWith("192.168.10.")) || net.ips[0] || "192.168.10.1").split(".").slice(0, 3).join(".");
  drawConfig();
}
function drawConfig() {
  const c = CFG, R = c.rules;
  const decks = c.devices.filter(d => d.kind === "hyperdeck" || d.kind === "bmcamera");
  const opt = (v, cur) => `<option value="${esc(v)}" ${v === cur ? "selected" : ""}>${esc(kindName[v] || v)}</option>`;
  const dopt = (cur) => decks.map(d => `<option value="${d.id}" ${d.id === cur ? "selected" : ""}>${esc(d.name)}</option>`).join("");
  $("#view").innerHTML = `<div class="cfg">
  <div class="card wide"><h2>EQUIPAMENTOS <span class="r"><button class="btn ghost" onclick="showPorts=!showPorts;drawConfig()">${showPorts ? "ESCONDER" : "EDITAR"} PORTAS</button><button class="btn" onclick="addDev()">+ ADICIONAR</button><button class="btn" onclick="scan()">🔎 PROCURAR NA REDE</button></span></h2>
    <table class="devtab"><thead><tr><th>NOME</th><th style="width:130px">TIPO</th><th style="width:170px">IP / URL</th><th style="width:90px">PORTA</th><th style="width:150px">GRUPO</th><th style="width:70px">ATIVO</th><th style="width:150px">ID (Companion)</th><th style="width:40px"></th></tr></thead><tbody>
    ${c.devices.map((d, i) => `<tr><td><input value="${esc(d.name)}" onchange="CFG.devices[${i}].name=this.value"></td>
      <td><select onchange="CFG.devices[${i}].kind=this.value;CFG.devices[${i}].port=({atem:9910,hyperdeck:9993,videohub:9990,http:80,bmcamera:80,webpresenter:9977,ups:161,switch:161}[this.value]||CFG.devices[${i}].port);drawConfig()">${["atem", "hyperdeck", "videohub", "bmcamera", "webpresenter", "ups", "switch", "ping", "tcp", "http"].map(k => opt(k, d.kind)).join("")}</select></td>
      <td><input value="${esc(d.host)}" placeholder="192.168.10.x" onchange="CFG.devices[${i}].host=this.value.trim()"></td><td>${d.kind === "ping" ? `<span class="mu sm">— (ping)</span>` : ["tcp", "http"].includes(d.kind) || showPorts ? `<input type="number" value="${d.port}" onchange="CFG.devices[${i}].port=+this.value">` : `<span class="mu sm" title="Porta padrão da Blackmagic — não precisa mexer">auto · ${d.port}</span>`}</td>
      <td><input value="${esc(d.group || "")}" placeholder="grupo (opcional)" onchange="CFG.devices[${i}].group=this.value">${["ups", "switch"].includes(d.kind) ? `<div class="row" style="margin-top:4px;flex-wrap:nowrap"><input placeholder="community (public)" value="${esc(d.community || "")}" onchange="CFG.devices[${i}].community=this.value.trim()" title="Community SNMP">${d.kind === "switch" ? `<input placeholder="portas ex.: 1-8,24" value="${esc(d.ports || "")}" onchange="CFG.devices[${i}].ports=this.value.trim()" title="Portas (ifIndex) a vigiar">` : ""}</div>` : ""}</td><td style="text-align:center"><input type="checkbox" ${d.enabled ? "checked" : ""} onchange="CFG.devices[${i}].enabled=this.checked" style="width:18px;height:18px;accent-color:var(--red)"></td>
      <td class="mu xs" style="font-family:var(--mono)">${esc(d.id || "(novo)")}</td><td><button class="btn ghost" onclick="CFG.devices.splice(${i},1);drawConfig()">✕</button></td></tr>${d.kind === "hyperdeck" ? `
      <tr class="subrow"><td colspan="8"><div class="row" style="gap:12px">
        <span class="mu xs" style="letter-spacing:.1em;font-weight:800">↳ COMO LER ESTE HYPERDECK</span>
        <label class="chk"><input type="radio" name="via${i}" ${!d.via ? "checked" : ""} onchange="CFG.devices[${i}].via='';drawConfig()">Conexão direta</label>
        <label class="chk"><input type="radio" name="via${i}" ${d.via === "companion" ? "checked" : ""} onchange="CFG.devices[${i}].via='companion';drawConfig()">Via Companion <span class="mu xs">(quando o Companion já usa a única conexão do HyperDeck)</span></label>
        ${d.via === "companion" ? `<span style="flex-basis:100%;height:0"></span><span class="mu sm" style="margin-left:22px">Nome da conexão no Companion:</span>
          <input id="cl${i}" style="width:280px;font-family:var(--mono)" placeholder="ex.: hyperdeck_5_PRODUZ" value="${esc(d.compLabel || "")}" oninput="CFG.devices[${i}].compLabel=this.value.trim()">
          <button class="btn red" onclick="CFG.devices[${i}].compLabel=$('#cl${i}').value.trim();compTest(CFG.devices[${i}].compLabel,${i})">TESTAR / ESCOLHER</button>
          ${!d.compLabel ? `<span class="tag warn">escreva o nome ou clique em TESTAR para escolher</span>` : ""}` : ""}
      </div></td></tr>` : ""}`).join("")}</tbody></table>
    <div class="hint" style="margin-top:8px"><b>Só o IP é necessário.</b> A porta é fixa de fábrica em cada tipo de equipamento Blackmagic e já vem preenchida (por isso aparece "auto"). Ela só importa em <b>Porta TCP</b>/<b>HTTP</b>, onde você diz qual serviço testar, ou se alguém mudou a porta via NAT/roteador. — ATEM: UDP 9910 (protocolo nativo, sem SDK) · HyperDeck: TCP 9993 · VideoHub: TCP 9990 · <b>Painel / só IP</b>: ATEM Advanced Panel, Broadcast Panel 1/2 M/E, Camera Control Panel, câmeras, conversores — online/offline e latência por ping · Porta TCP: qualquer equipamento IP (encoder, NDI, câmera PTZ, intercom, servidor) · HTTP: página de status ou URL completa.</div></div>

  <div class="card"><h2>GRAVAÇÃO · VIGILÂNCIA E PARES</h2>
    <div class="hint">Com a vigilância <b>armada</b> (botão no topo), estes gravadores devem estar em REC — qualquer um parado vira alerta crítico com som.</div>
    <div class="form" style="margin-top:8px">${decks.map(d => `<label class="chk"><input type="checkbox" ${c.recWatch?.includes(d.id) ? "checked" : ""} onchange="toggleWatch('${d.id}',this.checked)">${esc(d.name)}</label>`).join("") || `<span class="mu">Cadastre HyperDecks acima e salve.</span>`}</div>
    <h2 style="margin-top:16px">PARES PRINCIPAL / BACKUP <span class="r"><button class="btn" onclick="CFG.pairs=[...(CFG.pairs||[]),{primary:'',backup:'',label:''}];drawConfig()">+ PAR</button></span></h2>
    ${(c.pairs || []).map((p, i) => `<div class="row" style="margin-bottom:6px"><select onchange="CFG.pairs[${i}].primary=this.value"><option value="">Principal…</option>${dopt(p.primary)}</select><select onchange="CFG.pairs[${i}].backup=this.value"><option value="">Backup…</option>${dopt(p.backup)}</select><input placeholder="rótulo" value="${esc(p.label)}" onchange="CFG.pairs[${i}].label=this.value" style="width:150px"><button class="btn ghost" onclick="CFG.pairs.splice(${i},1);drawConfig()">✕</button></div>`).join("") || `<div class="mu sm">Nenhum par.</div>`}
    <div class="hint">O par compara REC, formato de vídeo e codec dos dois gravadores.</div></div>

  <div class="card wide"><h2>AUX DAS ATEMs · O QUE MONITORAR</h2>
    <div class="hint"><b style="color:var(--ok)">Salva na hora</b> (não precisa do botão SALVAR). Dê nome às saídas AUX e escolha o que o monitor deve acompanhar. <b>Ignorar</b> tira o AUX da tela, da linha do tempo, do relatório e do Companion (ex.: AUX do video man, que troca o tempo todo). <b>Travar</b> gera alerta crítico se o AUX sair da fonte atual (ex.: AUX que alimenta o encoder ou o telão).</div>
    ${c.devices.filter(d => d.kind === "atem").map(d => { const live = dev(d.id)?.atem; const ks = Object.keys(live?.aux || {}).map(Number).sort((a, b) => a - b);
      return `<div style="margin-top:12px"><div class="row"><b>${esc(d.name)}</b><span class="mu sm">${ks.length ? ks.length + " AUX lidos da mesa" : ""}</span><span class="spacer"></span>${ks.length ? `<button class="btn ghost" onclick="bulkAux('${d.id}','monitor')">MONITORAR TODOS</button><button class="btn ghost" onclick="bulkAux('${d.id}','ignore')">IGNORAR TODOS</button><button class="btn ghost" onclick="bulkAux('${d.id}','unnamed')" title="Ignora os AUX sem nome no monitor nem na mesa">IGNORAR OS SEM NOME</button>` : ""}</div>${ks.length ? `<div class="auxcfg">${ks.map(k => { const x = (c.auxCfg || []).find(y => y.device === d.id && y.aux === k) || { mode: "monitor", label: "" };
        return `<div class="row"><span class="num" style="width:56px"><b>AUX ${k + 1}</b></span><input placeholder="${esc(auxMesa(dev(d.id), k) || "nome (opcional)")}" value="${esc(x.label)}" style="flex:1;min-width:120px" onchange="setAuxCfg('${d.id}',${k},'label',this.value.trim())"><select onchange="setAuxCfg('${d.id}',${k},'mode',this.value)">${[["monitor", "Monitorar"], ["ignore", "Ignorar"], ["lock", "Travar na atual"]].map(([v, t]) => `<option value="${v}" ${x.mode === v ? "selected" : ""}>${t}</option>`).join("")}</select><span class="mu xs" style="width:150px;overflow:hidden;white-space:nowrap;text-overflow:ellipsis">agora: ${esc(live.inputs?.[live.aux[k]]?.long || live.aux[k])}</span></div>`; }).join("")}</div>` : `<div class="mu sm">ATEM ainda não conectada — os AUX aparecem aqui quando ela responder.</div>`}</div>`; }).join("") || `<div class="mu">Nenhuma ATEM cadastrada.</div>`}</div>

  <div class="card"><h2>REGRAS DE ALERTA</h2><div class="form">
    <label>SEM RESPOSTA APÓS (s)<input type="number" value="${R.offlineGraceSec}" onchange="CFG.rules.offlineGraceSec=+this.value"></label>
    <label>MÍDIA · ATENÇÃO ABAIXO DE (min)<input type="number" value="${R.remainWarnMin}" onchange="CFG.rules.remainWarnMin=+this.value"></label>
    <label>MÍDIA · CRÍTICO ABAIXO DE (min)<input type="number" value="${R.remainCritMin}" onchange="CFG.rules.remainCritMin=+this.value"></label>
    <label>AVISAR TROCA DE SLOT (min antes; -1 desliga)<input type="number" value="${R.slotWarnMin}" onchange="CFG.rules.slotWarnMin=+this.value"></label>
    <label>TIMECODE PARADO EM REC (s)<input type="number" value="${R.tcStallSec}" onchange="CFG.rules.tcStallSec=+this.value"></label>
    <label>PAR DIVERGENTE APÓS (s)<input type="number" value="${R.pairMismatchSec}" onchange="CFG.rules.pairMismatchSec=+this.value"></label>
    <label>LATÊNCIA ALTA ACIMA DE (ms)<input type="number" value="${R.latencyWarnMs}" onchange="CFG.rules.latencyWarnMs=+this.value"></label>
    <label>ESQUECEU DE GRAVAR · TOLERÂNCIA (s)<input type="number" min="3" value="${R.forgotRecSec || 10}" onchange="CFG.rules.forgotRecSec=+this.value" title="Depois que o relógio/rundown começa ou a ATEM sai do preto, quanto esperar antes de alarmar gravadores parados."></label>
    <label>CCU · REGISTRAR AJUSTE A PARTIR DE (%)<input type="number" min="1" max="50" value="${R.ccuMinPct || 5}" onchange="CFG.rules.ccuMinPct=+this.value" title="Variação mínima de íris, foco, lift, gama, ganho de cor, contraste para virar evento. Balanço de branco: 150 K."></label></div>
    <div class="form" style="margin-top:10px">
    ${[["alertFTB", "Alertar FADE TO BLACK ativo"], ["streamDropIsCrit", "Queda de stream da ATEM é crítica"], ["formatMismatchCrit", "Formato diferente no par é crítico"], ["soundOnCritical", "Repetir som enquanto houver crítico"], ["logPreview", "Registrar trocas de PREVIEW"], ["ccuOff", "NÃO registrar ajustes de controle de câmera (CCU)"], ["forgotRecOff", "Desligar o alarme ESQUECEU DE GRAVAR"], ["forgotRecNoAtem", "Esqueceu de gravar: NÃO usar \"ATEM saiu do preto\" (só o relógio)"]].map(([k, t]) => `<label class="chk"><input type="checkbox" ${R[k] ? "checked" : ""} onchange="CFG.rules.${k}=this.checked">${t}</label>`).join("")}</div></div>

  <div class="card"><h2>NOME DOS CLIPES</h2>
    <div class="mu sm">Ao dar REC pelo monitor, cada HyperDeck grava com este nome. O número continua de onde parou no cartão.</div>
    <div class="form" style="margin-top:8px"><label style="flex:2">MODELO DO NOME<input value="${esc(c.clipTemplate || "{JOB}_{DECK}_{NNN}")}" onchange="CFG.clipTemplate=this.value" style="font-family:var(--mono)"></label></div>
    <div class="xs mu2" style="margin-top:4px">{JOB} nome do job · {DECK} nome do gravador · {NNN} número 001, 002… · {DATA} 20261116 · {HORA} 1430</div>
    <label class="chk" style="margin-top:6px"><input type="checkbox" ${c.clipNameOff ? "checked" : ""} onchange="CFG.clipNameOff=this.checked">Não nomear (usa o nome padrão do gravador)</label>
    <div class="xs mu2" style="margin-top:4px">Vale para REC dado por este monitor. REC dado pelo Companion, painel da ATEM ou no próprio HyperDeck usa o nome configurado no gravador.</div></div>

  <div class="card"><h2>COMPANION · STREAM DECK</h2>
    <label class="chk"><input type="checkbox" ${c.companion.enabled ? "checked" : ""} onchange="CFG.companion.enabled=this.checked">Enviar variáveis para o Companion (variáveis personalizadas)</label>
    <div class="form" style="margin-top:8px"><label>IP DO COMPANION<input value="${esc(c.companion.host)}" onchange="CFG.companion.host=this.value"></label><label>PORTA HTTP<input type="number" value="${c.companion.port}" onchange="CFG.companion.port=+this.value"></label><label>PREFIXO<input value="${esc(c.companion.prefix)}" onchange="CFG.companion.prefix=this.value"></label></div>
    <div class="hint" style="margin-top:8px">Crie no Companion (Variables → Custom Variables) as variáveis que quiser usar, com os nomes abaixo. O monitor atualiza o valor a cada mudança. Também é possível ler tudo em <a href="/api/companion" target="_blank">/api/companion</a> (JSON) pelo módulo Generic HTTP.</div>
    <div class="code" id="cvars" style="margin-top:8px">carregando…</div></div>

  <div class="card"><h2>WEBHOOK · ALERTAS PARA FORA</h2>
    <label class="form" style="display:block"><span class="xs mu">URL (Discord, Slack, Teams, n8n, Node-RED…)</span><input style="width:100%" value="${esc(c.webhookUrl || "")}" placeholder="https://…" onchange="CFG.webhookUrl=this.value"></label>
    <div class="hint" style="margin-top:6px">Cada alerta de atenção/crítico gera um POST JSON com <code>text</code>/<code>content</code> prontos para chat.</div>
    <h2 style="margin-top:16px">ESTAÇÃO</h2><label class="form" style="display:block"><span class="xs mu">NOME DESTA ESTAÇÃO</span><input style="width:100%" value="${esc(c.stationName)}" onchange="CFG.stationName=this.value"></label>
    <div class="hint" style="margin-top:6px">Dados e jobs em <code>${esc(c._dir)}</code></div></div>

  <div class="card"><h2>AGENDA <span class="r"><button class="btn" onclick="CFG.schedule=[...(CFG.schedule||[]),{time:'20:00',action:'arm',days:'',label:''}];drawConfig()">+ HORÁRIO</button></span></h2>
    <div class="hint">Arma/desarma a vigilância de gravação ou roda o checklist sozinho. Dias: vazio = todos; ou números 1–7 (1 = segunda … 7 = domingo), ex.: <code>67</code> = sáb e dom.</div>
    ${(c.schedule || []).map((x, i) => `<div class="row" style="margin-top:6px"><input type="time" value="${esc(x.time)}" onchange="CFG.schedule[${i}].time=this.value" style="width:110px"><select onchange="CFG.schedule[${i}].action=this.value">${[["checklist", "Rodar checklist"], ["arm", "Armar vigilância"], ["disarm", "Desarmar vigilância"]].map(([v, t]) => `<option value="${v}" ${x.action === v ? "selected" : ""}>${t}</option>`).join("")}</select><input placeholder="dias" value="${esc(x.days)}" onchange="CFG.schedule[${i}].days=this.value.replace(/[^1-7]/g,'')" style="width:70px"><input placeholder="rótulo (ex.: JOGO)" value="${esc(x.label)}" onchange="CFG.schedule[${i}].label=this.value" style="flex:1"><button class="btn ghost" onclick="CFG.schedule.splice(${i},1);drawConfig()">✕</button></div>`).join("") || `<div class="mu sm" style="margin-top:6px">Nenhum horário.</div>`}
    <div class="hint" style="margin-top:6px">Dica: checklist 30 min antes, armar no horário do programa, desarmar ao fim.</div></div>

  <div class="card"><h2>TELEGRAM · ALERTAS NO CELULAR</h2>
    <label class="chk"><input type="checkbox" ${c.telegram?.enabled ? "checked" : ""} onchange="CFG.telegram=Object.assign(CFG.telegram||{}, {enabled:this.checked})">Enviar alertas pelo Telegram</label>
    <div class="form" style="margin-top:8px"><label>TOKEN DO BOT<input type="password" value="${esc(c.telegram?.token || "")}" onchange="CFG.telegram=Object.assign(CFG.telegram||{}, {token:this.value.trim()})"></label><label>CHAT ID (pessoa ou grupo)<input value="${esc(c.telegram?.chatId || "")}" onchange="CFG.telegram=Object.assign(CFG.telegram||{}, {chatId:this.value.trim()})"></label></div>
    <label class="chk" style="margin-top:8px"><input type="checkbox" ${c.telegram?.onlyCrit ? "checked" : ""} onchange="CFG.telegram=Object.assign(CFG.telegram||{}, {onlyCrit:this.checked})">Somente críticos</label>
    <div class="row" style="margin-top:8px"><button class="btn" onclick="tgTest()">ENVIAR TESTE</button></div>
    <div class="hint" style="margin-top:6px">No Telegram: fale com <b>@BotFather</b> → /newbot → copie o token. Mande uma mensagem para o seu bot (ou adicione-o ao grupo da técnica) e pegue o chat id com <b>@userinfobot</b>. O PC do rack precisa de internet.</div></div>

  <div class="card wide"><h2>SAÍDA TSL · TALLY E NOMES PARA MULTIVIEWER / UMD <span class="r"><button class="btn" onclick="CFG.tsl=[...(CFG.tsl||[]),{enabled:true,label:'',host:'',port:8900,proto:'udp5',atem:'${esc(c.devices.find(d => d.kind === "atem")?.id || "")}',me:0,offset:0,screen:0,short:false}];drawConfig()">+ DESTINO</button></span></h2>
    <div class="hint">Envia tally (vermelho = PGM, verde = PVW) e o nome de cada fonte da ATEM para multiviewers, displays sob monitores (UMD) e controladores de tally que aceitam TSL. Endereço/índice = número da fonte + deslocamento.</div>
    ${(c.tsl || []).map((t, i) => `<div class="row" style="margin-top:6px"><input type="checkbox" ${t.enabled ? "checked" : ""} onchange="CFG.tsl[${i}].enabled=this.checked" style="width:18px;height:18px;accent-color:var(--red)"><input placeholder="rótulo" value="${esc(t.label)}" onchange="CFG.tsl[${i}].label=this.value" style="width:170px"><input placeholder="IP do destino" value="${esc(t.host)}" onchange="CFG.tsl[${i}].host=this.value.trim()" style="width:140px"><input type="number" value="${t.port}" onchange="CFG.tsl[${i}].port=+this.value" style="width:80px" title="porta"><select onchange="CFG.tsl[${i}].proto=this.value">${[["udp5", "TSL 5.0 UDP"], ["tcp5", "TSL 5.0 TCP"], ["udp31", "TSL 3.1 UDP"]].map(([v, l]) => `<option value="${v}" ${t.proto === v ? "selected" : ""}>${l}</option>`).join("")}</select><select onchange="CFG.tsl[${i}].atem=this.value">${c.devices.filter(d => d.kind === "atem").map(d => `<option value="${d.id}" ${t.atem === d.id ? "selected" : ""}>${esc(d.name)}</option>`).join("")}</select><select onchange="CFG.tsl[${i}].me=+this.value">${[0, 1, 2, 3].map(m => `<option value="${m}" ${t.me === m ? "selected" : ""}>M/E ${m + 1}</option>`).join("")}</select><input type="number" value="${t.offset}" onchange="CFG.tsl[${i}].offset=+this.value" style="width:70px" title="deslocamento do endereço"><label class="chk"><input type="checkbox" ${t.short ? "checked" : ""} onchange="CFG.tsl[${i}].short=this.checked">nome curto</label><button class="btn ghost" onclick="CFG.tsl.splice(${i},1);drawConfig()">✕</button></div>`).join("") || `<div class="mu sm" style="margin-top:6px">Nenhum destino TSL.</div>`}</div>

  <div class="card"><h2>RELATÓRIO AUTOMÁTICO NO FIM DO JOB</h2>
    <label class="chk"><input type="checkbox" ${c.reportOnClose ? "checked" : ""} onchange="CFG.reportOnClose=this.checked">Ao encerrar o job, enviar o resumo pelo Telegram e/ou e-mail ativados</label>
    <h2 style="margin-top:14px">E-MAIL (SMTP)</h2>
    <label class="chk"><input type="checkbox" ${c.email?.enabled ? "checked" : ""} onchange="CFG.email=Object.assign(CFG.email||{},{enabled:this.checked})">Enviar por e-mail</label>
    <div class="form" style="margin-top:8px">${[["host", "SERVIDOR (ex.: smtp.gmail.com)"], ["port", "PORTA (587 ou 465)"], ["user", "USUÁRIO"], ["pass", "SENHA / SENHA DE APP"], ["from", "REMETENTE"], ["to", "DESTINATÁRIOS (vírgula)"]].map(([k, l]) => `<label>${l}<input ${k === "pass" ? 'type="password"' : k === "port" ? 'type="number"' : ""} value="${esc(c.email?.[k] ?? "")}" onchange="CFG.email=Object.assign(CFG.email||{},{${k}:${k === "port" ? "+this.value" : "this.value.trim()"}})"></label>`).join("")}</div>
    <div class="row" style="margin-top:8px"><button class="btn" onclick="mailTest()">ENVIAR E-MAIL DE TESTE</button></div>
    <div class="hint" style="margin-top:6px">Gmail: use uma "senha de app" (Conta Google → Segurança). O PC do rack precisa de internet.</div></div>

  <div class="card"><h2>KIT COMPANION / STREAM DECK</h2>
    <div class="hint">No Companion, módulo <b>Generic HTTP</b> → ação <b>GET</b> com estes endereços. As variáveis (lista mais abaixo) mostram REC, tempo restante, PGM e alertas nos botões.</div>
    <div class="code" style="margin-top:8px">${(() => { const b = `http://${location.hostname}:${location.port || 80}`; return [...(c.markerPresets || []).map(p => `MARCADOR ${p.padEnd(16)} ${b}/api/marker?label=${encodeURIComponent(p)}`), `PRÓXIMO BLOCO            ${b}/api/rundown/next`, `INICIAR PROGRAMA         ${b}/api/rundown/start`, `RODAR CHECKLIST          ${b}/api/checklist/run`, `ARMAR VIGILÂNCIA         ${b}/api/recwatch/arm`, `DESARMAR VIGILÂNCIA      ${b}/api/recwatch/disarm`].map(esc).join("\n"); })()}</div>
    <div class="hint" style="margin-top:6px">O arquivo de página do Companion (.companionconfig) muda entre versões. Se quiser a página pronta para importar, exporte uma página qualquer do <b>seu</b> Companion e mande — o gerador é feito no formato exato da sua versão.</div></div>

  <div class="card"><h2>NOTIFICAÇÕES NESTE COMPUTADOR</h2>
    <div class="hint">Mostra alertas críticos do Windows mesmo com o navegador minimizado (o painel precisa estar aberto em alguma aba). Funciona em <b>http://localhost</b>; em celulares pela rede o navegador bloqueia — use o Telegram.</div>
    <div class="row" style="margin-top:8px"><button class="btn" onclick="askNotif()">🔔 ATIVAR NOTIFICAÇÕES</button><span class="mu sm" id="nst"></span></div></div>

  <div class="card"><h2>BOTÕES DE MARCADOR</h2>
    <label class="form" style="display:block"><span class="xs mu">UM POR LINHA (os 9 primeiros viram teclas 1–9)</span><textarea rows="6" style="width:100%" onchange="CFG.markerPresets=this.value.split('\n').map(x=>x.trim()).filter(Boolean)">${esc((c.markerPresets || []).join("\n"))}</textarea></label></div>

  <div class="card"><h2>PERFIS DE EVENTO / HOUSE</h2>
    <div class="hint">Salva equipamentos, regras, vigilância, pares, rotas protegidas, AUX e agenda com um nome (ex.: "Arena", "Estúdio 2", "Caminhão"). Carregar troca tudo de uma vez.</div>
    <div class="row" style="margin-top:8px"><input id="pfn" placeholder="nome do perfil" style="flex:1"><button class="btn red" onclick="pfSave()">SALVAR ATUAL</button><label class="btn" style="cursor:pointer">IMPORTAR<input type="file" accept=".json" hidden onchange="pfImport(this.files[0])"></label></div>
    <div id="pflist" style="margin-top:8px"></div></div>

  <div class="card wide"><h2>SISTEMA</h2><div id="sysbox" class="row"><span class="mu sm">carregando…</span></div></div>

  <div class="card wide"><h2>COMANDOS FÍSICOS (SEGURANÇA)</h2>
    ${c._local ? `<div class="form"><label class="chk"><input type="checkbox" ${c.controlEnabled ? "checked" : ""} onchange="CFG.controlEnabled=this.checked">Permitir comandos (CUT, AUTO, FTB, AUX, macros, REC/STOP, roteamento VideoHub)</label>
      <label class="chk"><input type="checkbox" ${c.allowRemoteControl ? "checked" : ""} onchange="CFG.allowRemoteControl=this.checked">Permitir comandos a partir de outros aparelhos da rede</label>
      <label>PIN PARA LIBERAR (opcional)<input type="password" placeholder="${c._pinSet ? "•••• (definido — digite para trocar)" : "sem PIN"}" onchange="CFG.newPin=this.value"></label></div>
      <div class="hint" style="margin-top:8px">Padrão de fábrica: <b>somente leitura</b>. Mesmo permitidos, os comandos ficam travados até alguém clicar em 🔒 COMANDOS, e a liberação expira sozinha. Tudo é registrado na linha do tempo.</div>`
      : `<div class="hint">Estas opções só podem ser alteradas no computador do rack (localhost).</div>`}</div>
  </div>
  <div class="row" style="position:sticky;bottom:0;background:linear-gradient(transparent,var(--bg) 40%);padding:18px 0 6px"><span class="spacer"></span><button class="btn" onclick="staticRendered='';render()">DESCARTAR</button><button class="btn red big" onclick="saveCfg()">SALVAR CONFIGURAÇÃO</button></div>`;
  pfDraw();
  api("GET", "/api/system").then(sy => {
    const el = $("#sysbox"); if (!el) return;
    if (!sy.local) { el.innerHTML = `<span class="hint">Opções do sistema só aparecem no computador do rack.</span>`; return; }
    el.innerHTML = `${sy.autostartSupported ? `<label class="chk"><input type="checkbox" ${sy.autostart ? "checked" : ""} onchange="api('POST','/api/system/autostart',{on:this.checked}).then(()=>toast(this.checked?'O monitor vai abrir sozinho ao ligar o computador.':'Início automático desligado.','ok')).catch(e=>toast(esc(e.message),'crit'))">Iniciar o monitor junto com o ${sy.os === "darwin" ? "Mac" : "Windows"}</label>` : ""}
      <span class="spacer"></span><button class="btn" onclick="api('POST','/api/system/openfolder')">📁 ABRIR PASTA DE DADOS</button>
      <button class="btn red" onclick="sysQuit()">⏻ ENCERRAR MONITOR</button>
      <div style="flex-basis:100%;border-top:1px solid var(--line,#333);margin-top:10px;padding-top:10px"><b style="letter-spacing:.1em;font-size:12px">ATUALIZAÇÕES</b>
        <div class="form" style="margin-top:6px"><label style="flex:2">REPOSITÓRIO NO GITHUB (usuario/repositorio)<input value="${esc(CFG.updateRepo || "")}" placeholder="${esc(sy.defaultRepo || "ex.: donfilm/rack-monitor")}" onchange="CFG.updateRepo=this.value.trim()"></label></div>
        <label class="chk"><input type="checkbox" ${CFG.updateCheckOff ? "" : "checked"} onchange="CFG.updateCheckOff=!this.checked">Procurar versão nova ao abrir e a cada 6 horas (avisa no topo da tela)</label>
        <label class="chk"><input type="checkbox" ${CFG.updateAuto ? "checked" : ""} onchange="CFG.updateAuto=this.checked">Instalar sozinho quando sair versão nova (só quando nenhum gravador estiver em REC)</label>
        <div class="row" style="margin-top:6px"><span id="updst" class="mu sm">V${esc(sy.version)}</span><span class="spacer"></span><button class="btn" onclick="updCheck()">PROCURAR AGORA</button><button class="btn red" onclick="updInstall()">⬆ ATUALIZAR</button></div></div>
      <div class="hint" style="flex-basis:100%">O monitor roda sem janela. Para abrir o painel de novo, use o atalho/ícone do RACK MONITOR (se já estiver rodando, ele só abre o painel). V${esc(sy.version)} · dados em <code>${esc(sy.dataDir)}</code></div>`;
  }).catch(() => { });
  api("GET", "/api/companion").then(v => { const k = Object.keys(v).sort(); $("#cvars").textContent = k.map(x => `$(custom:${x})`.padEnd(48) + " = " + v[x]).join("\n"); }).catch(() => { });
}
window.drawConfig = drawConfig;
window.updCheck = async () => {
  const el = $("#updst"); if (el) el.textContent = "procurando…";
  try {
    const u = await api("GET", "/api/update?check=1");
    const t = u.error ? u.error : u.available ? `Nova versão V${u.latest} disponível (atual V${u.current}).` : `Você está na versão mais recente (V${u.current}).`;
    if (el) { el.textContent = t; el.style.color = u.error ? "var(--warn)" : u.available ? "var(--ok)" : ""; }
  } catch (e) { if (el) el.textContent = e.message; }
};
async function pfDraw() {
  const l = (await api("GET", "/api/profiles").catch(() => [])) || [];
  const el = $("#pflist"); if (!el) return;
  el.innerHTML = l.length ? `<table><tbody>${l.map(p => `<tr><td><b>${esc(p.name)}</b><div class="mu xs">${(p.devices || []).length} equipamentos · salvo ${dt(p.saved).slice(0, 16)}</div></td><td style="text-align:right;white-space:nowrap"><button class="btn red" onclick="pfLoad('${jsq(p.name)}')">CARREGAR</button> <a class="btn ghost" href="/api/profiles/${encodeURIComponent(p.name)}/export">EXPORTAR</a> <button class="btn ghost" onclick="pfDel('${jsq(p.name)}')">✕</button></td></tr>`).join("")}</tbody></table>` : `<div class="mu sm">Nenhum perfil salvo.</div>`;
}
window.pfSave = async () => { const n = $("#pfn").value.trim(); if (!n) { toast("Dê um nome ao perfil.", "warn"); return; } await api("POST", "/api/profiles", { name: n }); toast("Perfil salvo (configuração já salva).", "ok"); pfDraw(); };
window.pfLoad = async n => { if (!(await confirmBox("CARREGAR PERFIL", `Trocar equipamentos, regras, vigilância, pares, rotas, AUX e agenda pelos do perfil <b>${esc(n)}</b>?`, "CARREGAR"))) return; await api("POST", "/api/profiles/load", { name: n }); toast("Perfil carregado.", "ok"); staticRendered = ""; setTimeout(render, 500); };
window.pfDel = async n => { if (!(await confirmBox("APAGAR PERFIL", `Apagar o perfil <b>${esc(n)}</b>?`, "APAGAR"))) return; await api("DELETE", "/api/profiles/" + encodeURIComponent(n)); pfDraw(); };
window.pfImport = async f => { try { const p = JSON.parse(await f.text()); await api("POST", "/api/profiles/import", p); toast("Perfil importado.", "ok"); pfDraw(); } catch (e) { toast("Arquivo inválido: " + esc(e.message), "crit"); } };
window.sysQuit = async () => {
  if (!(await confirmBox("ENCERRAR O MONITOR", "Parar o RACK MONITOR neste computador? Os alertas e a gravação de eventos param até abrir de novo. O job atual continua quando reabrir.", "ENCERRAR"))) return;
  await api("POST", "/api/system/shutdown").catch(() => { });
  document.body.innerHTML = `<div style="display:flex;height:100vh;align-items:center;justify-content:center;flex-direction:column;gap:10px;font-family:var(--f)"><img src="logo.png" style="width:90px;opacity:.6"><b style="letter-spacing:.14em">RACK MONITOR ENCERRADO</b><span style="color:#888">Abra pelo ícone do programa para voltar.</span></div>`;
};
window.mailTest = async () => { try { await api("POST", "/api/email/test", CFG.email || {}); toast("E-mail de teste enviado.", "ok"); } catch (e) { toast(esc(e.message), "crit"); } };
window.askNotif = async () => {
  if (!("Notification" in window)) { toast("Este navegador não suporta notificações aqui (use localhost ou o Telegram).", "warn"); return; }
  const p = await Notification.requestPermission();
  $("#nst").textContent = p === "granted" ? "ativadas ✓" : "bloqueadas pelo navegador";
  if (p === "granted") new Notification("RACK MONITOR BY DON", { body: "Notificações ativadas.", icon: "icon-192.png" });
};
window.tgTest = async () => { try { await api("POST", "/api/telegram/test", CFG.telegram || {}); toast("Mensagem de teste enviada.", "ok"); } catch (e) { toast(esc(e.message), "crit"); } };
function auxItem(id, k) { const x = (CFG.auxCfg || []).find(y => y.device === id && y.aux === k); return { aux: k, label: x?.label || "", mode: x?.mode || "monitor" }; }
async function pushAux(id, items, msg) {
  try {
    const r = await api("POST", "/api/auxcfg/bulk", { device: id, items });
    CFG.auxCfg = r.auxCfg || [];
    if (msg) toast(msg, "ok");
    return r;
  } catch (e) { toast("Não salvou: " + esc(e.message), "crit"); }
}
window.setAuxCfg = async (id, k, field, val) => {
  const it = auxItem(id, k); it[field] = val;
  await pushAux(id, [it], `AUX ${k + 1} salvo${field === "mode" ? " — " + { monitor: "monitorado", ignore: "ignorado", lock: "travado na fonte atual" }[val] : ""}.`);
};
window.bulkAux = async (id, mode) => {
  const d = dev(id), live = d?.atem; if (!live) return;
  const ks = Object.keys(live.aux || {}).map(Number);
  let items;
  if (mode === "unnamed") {
    items = ks.filter(k => !auxItem(id, k).label && !auxMesa(d, k)).map(k => Object.assign(auxItem(id, k), { mode: "ignore" }));
    if (!items.length) { toast(`Nenhum AUX sem nome: todos os ${ks.length} têm nome na mesa ou no monitor. Use IGNORAR TODOS ou escolha um por um.`, "warn"); return; }
  } else items = ks.map(k => Object.assign(auxItem(id, k), { mode }));
  if (mode === "ignore" && !(await confirmBox("IGNORAR TODOS OS AUX", `Ignorar os <b>${ks.length} AUX</b> de ${esc(d.name)}? Eles somem da tela, da linha do tempo, do relatório e do Companion. Depois é só marcar <b>Monitorar</b> nos que interessam.`, "IGNORAR TODOS"))) return;
  const r = await pushAux(id, items, `${items.length} AUX de ${esc(d.name)} → ${{ monitor: "monitorados", ignore: "ignorados", unnamed: "ignorados (sem nome)" }[mode]}. Já está valendo.`);
  if (r) drawConfig();
};
window.compTest = async (label, idx) => {
  const b = modal("TESTE · LEITURA PELO COMPANION", `<div class="empty">Consultando o Companion…</div>`);
  const r = await api("GET", "/api/companion/test?label=" + encodeURIComponent(label)).catch(e => ({ error: e.message }));
  const nm = { status: "Transporte (REC/PARADO)", tc: "Timecode", slot: "Slot ativo", clip: "Clipe", format: "Formato", codec: "Codec", remain: "Tempo restante", remain1: "Restante slot 1", remain2: "Restante slot 2" };
  let conns = [];
  try { conns = JSON.parse(r.connections || "[]"); } catch (e) { }
  if (!label) r.vars = (r.vars || []).map(v => Object.assign(v, { value: "" }));
  b.innerHTML = r.error ? `<div class="empty">${esc(r.error)}</div>` : !r.reachable ? `<div class="slotnote crit" style="margin:0">Companion não respondeu em <b>${esc(r.base)}</b>.<small>Confira IP e porta em CONFIGURAÇÃO → COMPANION (o padrão é a porta 8000) e se a API HTTP está ligada no Companion.</small></div>`
    : `<div class="hint" style="margin-bottom:8px">Companion em <b>${esc(r.base)}</b> · conexão <b>${esc(label)}</b></div>
      <table><tbody>${r.vars.map(v => `<tr><td><b>${esc(nm[v.key] || v.key)}</b></td><td class="mu xs" style="font-family:var(--mono)">${esc(v.variable || "—")}</td><td>${v.value ? `<span class="tag ok">${esc(v.value)}</span>` : `<span class="tag">não encontrado</span>`}</td></tr>`).join("")}</tbody></table>
      ${r.vars.find(v => v.key === "status")?.value ? `<div class="slotnote ok">✓ Funciona. Agora clique em SALVAR CONFIGURAÇÃO.</div>` : !label ? `<div class="slotnote warn">Escolha a conexão abaixo (ou escreva o nome na configuração).</div>` : `<div class="slotnote crit">Nenhum dado para "${esc(label)}".<small>O nome tem que ser igual ao "Label" da conexão no Companion (maiúsculas/minúsculas contam).</small></div>`}
      ${!r.vars.find(v => v.key === "tc")?.value ? `<div class="hint" style="margin-top:6px">Para ter TIMECODE: no Companion, edite a conexão → <b>Timecode Variables</b> = <b>Polling</b> (ou Notifications em firmware 7+).</div>` : ""}
      ${conns.length ? `<h2 style="margin-top:12px">CONEXÕES HYPERDECK NO SEU COMPANION ${idx !== undefined ? `<span class="mu xs" style="letter-spacing:.04em">clique para usar</span>` : ""}</h2><div class="row">${conns.filter(c => /hyperdeck/i.test((c.moduleId || "") + c.label)).map(c => `<button class="btn ${c.label === label ? "red" : ""}" ${idx !== undefined ? `onclick="CFG.devices[${idx}].compLabel='${jsq(c.label)}';drawConfig();compTest('${jsq(c.label)}',${idx})"` : ""}>${esc(c.label)}</button>`).join(" ")}</div>` : `<div class="hint" style="margin-top:10px">Dica: o nome é o <b>Label</b> da conexão no Companion (aba Connections), por exemplo <code>hyperdeck_5_PRODUZ</code>.</div>`}`;
};
window.addDev = () => { CFG.devices.push({ name: "Novo equipamento", kind: "hyperdeck", host: CFG._prefix + ".", port: 9993, enabled: true, group: "" }); drawConfig(); };
window.toggleWatch = (id, on) => { CFG.recWatch = (CFG.recWatch || []).filter(x => x !== id); if (on) CFG.recWatch.push(id); };
window.saveCfg = async () => {
  const body = Object.assign({}, CFG); delete body._pinSet; delete body._local; delete body._dir; delete body._prefix;
  if (CFG.newPin === undefined) delete body.newPin;
  body.pairs = (body.pairs || []).filter(p => p.primary && p.backup && p.primary !== p.backup);
  try { await api("PUT", "/api/config", body); toast("Configuração salva e aplicada.", "ok"); staticRendered = ""; setTimeout(render, 400); }
  catch (e) { toast(esc(e.message), "crit"); }
};
window.scan = async () => {
  const b = modal("PROCURAR EQUIPAMENTOS NA REDE", `<div class="row"><input id="sp" value="${esc(CFG._prefix)}" style="width:160px"><span class="mu">.1 – .254</span><button class="btn red" id="sgo">PROCURAR</button></div><div class="hint" style="margin-top:8px">Procura HyperDeck (9993), VideoHub (9990), ATEM (UDP 9910), Companion (8000) e <b>qualquer aparelho Blackmagic pelo MAC</b> (painéis ATEM, Camera Control Panel, câmeras, conversores). Somente leitura; leva ~10 s.</div><div id="sres" style="margin-top:12px"></div>`);
  $("#sgo", b).onclick = async () => {
    $("#sres", b).innerHTML = `<div class="empty">Procurando…</div>`;
    const res = await api("POST", "/api/scan", { prefix: $("#sp", b).value });
    const known = new Set(CFG.devices.map(d => d.host + ":" + d.port));
    $("#sres", b).innerHTML = res.length ? `<table><tbody>${res.map((f, i) => `<tr><td class="num"><b>${esc(f.host)}</b></td><td>${esc(kindName[f.kind] || f.kind)} :${f.port}</td><td class="mu xs">${esc(f.info)}</td><td>${known.has(f.host + ":" + f.port) ? `<span class="tag ok">CADASTRADO</span>` : `<button class="btn" data-i="${i}">ADICIONAR</button>`}</td></tr>`).join("")}</tbody></table>` : `<div class="empty">Nada encontrado nessa faixa.</div>`;
    b.querySelectorAll("button[data-i]").forEach(x => x.onclick = () => {
      const f = res[+x.dataset.i];
      CFG.devices.push({ name: f.kind === "ping" ? `Painel/aparelho Blackmagic ${f.host}` : `${kindName[f.kind] || f.kind} ${f.host}`, group: f.kind === "ping" ? "Painéis" : "", kind: f.kind, host: f.host, port: f.port, enabled: true, group: "" });
      x.outerHTML = `<span class="tag ok">ADICIONADO</span>`; drawConfig();
    });
  };
};

// ---------------- TALLY ----------------
function vTally() {
  if (params.length >= 2) {
    const d = dev(params[0]), src = +params[1], me = +(params[2] || 0);
    const a = d?.atem, m = a?.mes?.[me];
    let cls = "off", st = "SEM SINAL";
    if (d?.online && m) {
      if (m.program === src || (a.tally?.[src] & 1)) { cls = "pgm"; st = "NO AR"; }
      else if (m.preview === src || (a.tally?.[src] & 2)) { cls = "pvw"; st = "PREVIEW"; }
      else { cls = ""; st = ""; }
    }
    const nm = a?.inputs?.[src]?.long || "Fonte " + src;
    $("#view").innerHTML = `<div class="tally ${cls}" ondblclick="location.hash='#/tally'"><div class="nm">${esc(a?.inputs?.[src]?.short || src)}</div><div class="st">${st || "&nbsp;"}</div><div class="ft">${esc(nm)} · ${esc(d?.name || "")} · M/E ${me + 1} · toque duas vezes para sair</div></div>`;
    return;
  }
  const L = devs("atem").filter(d => d.atem);
  $("#view").innerHTML = `<h2>TALLY EM QUALQUER CELULAR / TABLET</h2><div class="hint" style="margin-bottom:14px">Abra no celular do câmera o endereço <b>http://IP-DESTE-PC:${location.port || 80}/#/tally/…</b> (mesma rede). Vermelho = no ar, verde = preview. Toque em uma fonte abaixo para abrir.</div>
  ${L.map(d => `<div class="card" style="margin-bottom:12px"><h2>${esc(d.name)}</h2>${Object.keys(d.atem.mes || {}).map(me => `<div class="mu xs" style="margin:6px 0">M/E ${+me + 1}</div><div class="srcs">${Object.values(d.atem.inputs).filter(i => i.id > 0 && i.id < 1000).sort((x, y) => x.id - y.id).map(i => `<a class="src ${d.atem.mes[me].program === i.id ? "p" : d.atem.mes[me].preview === i.id ? "v" : ""}" style="text-decoration:none" href="#/tally/${d.id}/${i.id}/${me}">${esc(i.short)}<br><small style="font-weight:400">${esc(i.long)}</small></a>`).join("")}</div>`).join("")}</div>`).join("") || `<div class="empty">Nenhuma ATEM conectada.</div>`}`;
}

// ---------------- TELÃO ----------------
function vWall() {
  const al = S.alerts.filter(a => !a.acked);
  $("#view").innerHTML = `${al.length ? `<div class="wallalerts alerts">${al.slice(0, 4).map(alertCard).join("")}</div>` : `<div class="okbig" style="margin-bottom:14px"><b>TUDO OK</b></div>`}
    <div class="wall">${S.devices.filter(d => d.enabled).map(tile).join("")}</div>
    <div class="row" style="margin-top:10px"><span class="mu xs">TELÃO · atualização ao vivo · ${esc(S.station)}</span><span class="spacer"></span><a class="btn ghost" href="#/painel">SAIR DO TELÃO</a></div>`;
}

// ---------------- início ----------------
if ("serviceWorker" in navigator && window.isSecureContext) navigator.serviceWorker.register("sw.js").catch(() => { });
(async function init() {
  try { soundOn = localStorage.getItem("rd_sound") === "1"; } catch (e) { }
  if (soundOn) { soundOn = false; } // navegador exige um clique para liberar áudio
  WHO = await api("GET", "/api/whoami").catch(() => WHO);
  setInterval(async () => { WHO = await api("GET", "/api/whoami").catch(() => WHO); }, 15000);
  const ev = await api("GET", "/api/events?min=trace").catch(() => []);
  EV = ev;
  route();
  connect();
})();
