// RACK MONITOR BY DON — painel web · telas de operação: PAINEL, ATEM, GRAVAÇÕES, MATRIZ, LINHA DO TEMPO
"use strict";
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
</div>${a && dskList(a).length ? `<div class="dskrow"><span class="lbl">DSK</span>${dskList(a).map(x => `<div class="kbox ${x.on ? "on" : ""} ${x.trans ? "trans" : ""}"><b>DSK ${x.k + 1}</b><span class="kst">${x.trans ? "EM TRANSIÇÃO" : x.on ? "NO AR" : "FORA"}</span>${x.tie ? `<span class="knext">TIE</span>` : ""}<div class="kf">${esc(x.fill || "—")}</div>${x.key ? `<div class="kk">key: ${esc(x.key)}</div>` : ""}</div>`).join("")}</div>` : ""}`;
    if (!d.online || !a) return `<section class="atem card">${head}<div class="empty">${esc(d.status)} — ${esc(d.detail)}</div></section>`;
    const nm = s => a.inputs?.[s]?.long || "Fonte " + s;
    const sh = s => a.inputs?.[s]?.short || s;
    const ext = Object.values(a.inputs || {}).filter(i => i.id > 0 && i.id < 1000).sort((x, y) => x.id - y.id);
    const others = Object.values(a.inputs || {}).filter(i => i.id === 0 || (i.id >= 1000 && i.id < 4000) || i.id === 6000).sort((x, y) => x.id - y.id);
    const bus = [...ext, ...others];
    const big = bus.length > 20;
    const mes = Object.entries(a.mes || {}).sort((x, y) => x[0] - y[0]).map(([i, m]) => `
      <div class="me"><div class="row"><span class="lbl">M/E ${+i + 1}</span>${m.ftb ? `<span class="tag crit">FADE TO BLACK</span>` : ""}${m.inTransition ? `<span class="tag warn">EM TRANSIÇÃO</span>` : ""}
<span class="spacer"></span><span class="mu xs">no ar há ${valid(m.programSince) ? ago(m.programSince) : "—"}</span></div>
        <div class="busrow"><div class="bus pvw"><small>PREVIEW</small><b>${esc(nm(m.preview))}</b><div class="s">fonte ${m.preview}</div></div><div class="bus pgm"><small>PROGRAM</small><b>${esc(nm(m.program))}</b><div class="s">fonte ${m.program}</div></div></div>
        <div class="tbar"><i style="width:${m.inTransition ? m.transPos / 100 : 0}%"></i></div>
        ${uskList(a, m).length ? `<div class="uskrow"><span class="lbl">KEYS</span>${uskList(a, m).map(u => `<div class="kbox ${u.on ? "on" : ""}"><b>USK ${u.k + 1}</b><span class="kst">${u.on ? "NO AR" : "FORA"}</span>${u.next ? `<span class="knext" title="Entra/sai no próximo CUT/AUTO">${u.on ? "SAI" : "ENTRA"} NO PRÓXIMO</span>` : ""}${u.type ? `<div class="kt">${esc(u.type)}</div>` : ""}<div class="kf">${esc(u.fill || (u.type ? "" : "—"))}</div>${u.key ? `<div class="kk">key: ${esc(u.key)}</div>` : ""}</div>`).join("")}</div>` : ""}
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
    <div class="form"><label>${rest ? "MÍDIA" : "SLOT"}<select id="fs_s">${rest ? `<option value="">lendo mídias do HyperDeck…</option>` : (slots.filter(([i, x]) => x.status === "mounted").map(([i, x]) => `<option value="${i}" ${+i !== k.activeSlot ? "selected" : ""}>Slot ${i} · ${esc(x.volume || "sem nome")} · ${hms(x.remainSec)} livres${+i === k.activeSlot ? " · ATIVO" : ""}</option>`).join("") || `<option value="">nenhum cartão montado</option>`)}</select></label>
    <label>SISTEMA DE ARQUIVOS<select id="fs_f"><option value="0">exFAT (Windows e Mac)</option><option value="1">HFS+ (Mac)</option></select></label>
    <label>NOME DO CARTÃO <span class="xs mu2">(exFAT: até 11 letras)</span><input id="fs_n" maxlength="11" value="${esc(d.name.replace(/hyper\s*deck/i, "HD").replace(/[^\w-]+/g, "_").replace(/^_+|_+$/g, "").slice(0, 11))}"></label></div>
    <div class="form" style="margin-top:12px"><label>PIN DE COMANDOS<input id="fs_p" type="password" autocomplete="off" placeholder="obrigatório para formatar"></label>
    <label style="flex:2"><span class="xs mu">PARA CONFIRMAR, DIGITE <b style="color:var(--red)">FORMATAR</b></span><input id="fs_c" autocomplete="off"></label></div>
    <div class="row" style="margin-top:14px;justify-content:flex-end"><button class="btn" onclick="closeModal()">CANCELAR</button><button class="btn red big" id="fs_go" disabled>⌫ FORMATAR AGORA</button></div>`);
  let media = [];
  if (rest) api("GET", "/api/deckmedia/" + id).then(L => {
    media = L || [];
    $("#fs_s", b).innerHTML = media.length ? media.map(m => `<option value="${esc(m.deviceName)}" ${m.activeDisk ? "" : "selected"}>${esc(m.volume || m.deviceName)} · ${esc(m.deviceName)}${m.activeDisk ? " · ATIVA (gravando aqui)" : ""}${m.remainingRecordTime ? " · " + hms(m.remainingRecordTime) + " livres" : ""}</option>`).join("") : `<option value="">nenhuma mídia montada</option>`;
  }).catch(e => { $("#fs_s", b).innerHTML = `<option value="">não leu as mídias</option>`; toast(esc(e.message), "crit"); });
  $("#fs_f", b).onchange = e => { const n = $("#fs_n", b); n.maxLength = e.target.value === "1" ? 27 : 11; n.value = n.value.slice(0, n.maxLength); };
  $("#fs_c", b).oninput = e => $("#fs_go", b).disabled = e.target.value.trim().toUpperCase() !== "FORMATAR";
  $("#fs_go", b).onclick = async () => {
    const sel = $("#fs_s", b).value;
    if (rest && !sel) { toast("Escolha a mídia.", "warn"); return; }
    if (!rest && !sel) { toast("Nenhum cartão montado para formatar.", "warn"); return; }
    if (!$("#fs_p", b).value.trim()) { toast("Digite o PIN de comandos.", "warn"); return; }
    const slot = rest ? 0 : +sel, lbl = rest ? (media.find(m => m.deviceName === sel)?.volume || sel) : "Slot " + slot;
    $("#fs_go", b).disabled = true; $("#fs_go", b).textContent = "FORMATANDO…";
    try { await api("POST", "/api/control", { device: id, action: "format", a: slot, b: +$("#fs_f", b).value, name: $("#fs_n", b).value, media: rest ? sel : "", pin: $("#fs_p", b).value.trim() }); closeModal(); toast(`${esc(lbl)} de ${esc(d.name)} formatado.`, "ok"); }
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
