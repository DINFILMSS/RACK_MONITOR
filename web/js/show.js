// RACK MONITOR BY DON — painel web · RELÓGIO/RUNDOWN, PÓS-PRODUÇÃO, CHECKLIST e SAÚDE
"use strict";
// ---------------- RELÓGIO / RUNDOWN ----------------
const mmss = s => { const neg = s < 0; s = Math.abs(Math.round(s)); const h = Math.floor(s / 3600); return (neg ? "−" : "") + (h ? h + ":" + pad(Math.floor(s / 60) % 60) : Math.floor(s / 60)) + ":" + pad(s % 60); };
let rdEdit = null;
// estado calculado do relógio/roteiro (usado no RELÓGIO e no quadro da tela PAINEL)
function rdCalc() {
  const r = S.rundown || { items: [], current: -1 };
  const items = r.items || [];
  const now = Date.now();
  const started = r.current >= 0 && valid(r.startedAt), ended = valid(r.ended);
  const cur = started && r.current < items.length ? items[r.current] : null;
  const prog = started ? ((ended ? new Date(r.ended) : now) - new Date(r.startedAt)) / 1000 : 0;
  const inBlock = cur ? (now - new Date(r.blockAt)) / 1000 : 0;
  const free = cur && !cur.dur; // bloco sem duração = cronômetro progressivo
  const left = cur ? (free ? inBlock : cur.dur - inBlock) : 0;
  const delay = started ? prog - (items.slice(0, Math.max(0, r.current)).reduce((a, x) => a + x.dur, 0) + inBlock) : 0;
  const next = started && r.current + 1 < items.length ? items[r.current + 1] : null;
  const pace = started && !ended ? (Math.abs(delay) < 15 ? "NO TEMPO" : (delay > 0 ? "ATRASADO " : "ADIANTADO ") + mmss(Math.abs(delay))) : "—";
  const paceColor = Math.abs(delay) < 15 ? "var(--ok)" : delay > 0 ? "var(--red)" : "var(--warn)";
  return { r, items, started, ended, cur, prog, inBlock, free, left, delay, next, pace, paceColor };
}
function vClock() {
  const tela = params[0] === "tela";
  const d = new Date();
  const { r, items, started, ended, cur, prog, free, left, delay, next } = rdCalc();
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
    $("#view").innerHTML = `<div id="clklive"></div><div class="card" style="margin-top:14px"><h2>ROTEIRO (RUNDOWN) DESTE JOB <span class="r"><button class="btn" onclick="rdCopy()" title="Copiar blocos e tempos do roteiro de um job anterior">⧉ COPIAR DE OUTRO DIA</button><button class="btn" onclick="rdEdit.push({title:'NOVO BLOCO',dur:300});drawRd()">+ BLOCO</button><button class="btn red" onclick="rdSave()">SALVAR ROTEIRO</button></span></h2>
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

// ---- quadro do relógio na tela PAINEL (liga/desliga em cada aparelho) ----
const painelClockOn = () => { try { return localStorage.getItem("rd_painelClock") !== "0"; } catch (e) { return true; } };
window.painelClockSet = on => { try { localStorage.setItem("rd_painelClock", on ? "1" : "0"); } catch (e) { } render(); };
function miniClockBody() {
  const d = new Date();
  const { items, started, ended, cur, prog, inBlock, free, left, next, pace, paceColor, r } = rdCalc();
  const st = free ? "" : cur && left < 0 ? "over" : cur && left < 30 ? "soon" : "";
  const pct = cur && cur.dur ? Math.min(100, Math.max(0, inBlock / cur.dur * 100)) : 0;
  const title = cur ? `BLOCO ${r.current + 1}/${items.length} · ${esc(cur.title)}` : ended ? "PROGRAMA ENCERRADO" : items.length ? `AGUARDANDO INÍCIO · ${items.length} bloco${items.length > 1 ? "s" : ""}` : "SEM ROTEIRO";
  return `<div class="mclk">
    <div class="mclk-now"><small>HORA</small><b class="num">${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}</b></div>
    <div class="mclk-blk ${st}"><small>${title}</small><b class="num">${cur ? mmss(left) : "--:--"}</b><span>${cur ? (free ? "tempo no bloco" : left < 0 ? "ESTOURADO" : "restante") : ""}</span>${cur && cur.dur ? `<i class="mclk-bar"><i style="width:${pct}%"></i></i>` : ""}</div>
  </div>
  <div class="mclk-info"><span><small>NO AR HÁ</small><b class="num">${started ? mmss(prog) : "--:--"}</b></span><span><small>RITMO</small><b class="num" style="color:${started && !ended ? paceColor : "inherit"}">${pace}</b></span><span class="mclk-next"><small>PRÓXIMO</small><b>${next ? esc(next.title) + ` <span class="num mu">${next.dur ? mmss(next.dur) : "livre"}</span>` : "—"}</b></span></div>
  <div class="row" style="margin-top:8px">${!started || ended ? (items.length ? `<button class="btn red" onclick="api('POST','/api/rundown/start').catch(e=>toast(esc(e.message),'warn'))">▶ INICIAR PROGRAMA</button>` : `<a class="btn" href="#/relogio">MONTAR ROTEIRO</a>`) : `<button class="btn" onclick="rd('prev')" title="Bloco anterior">◀</button><button class="btn red" style="flex:1" onclick="rd('next')">${next ? "PRÓXIMO ▶ " + esc(next.title) : "■ ENCERRAR PROGRAMA"}</button>`}</div>`;
}
function miniClock() {
  if (!painelClockOn()) return "";
  return `<h2 style="margin-top:18px">RELÓGIO · ROTEIRO <span class="r"><a class="btn ghost" href="#/relogio">ABRIR</a><button class="btn ghost" title="Esconder o relógio neste aparelho" onclick="painelClockSet(false)">✕</button></span></h2>
    <div class="card" id="mclk">${miniClockBody()}</div>`;
}
// atualiza só o quadro a cada meio segundo (sem redesenhar a tela inteira)
setInterval(() => { if (view === "painel") { const el = document.getElementById("mclk"); if (el && !pointerDown) el.innerHTML = miniClockBody(); } }, 500);
// copiar o roteiro de um job anterior (mesmo evento em vários dias)
window.rdCopy = async () => {
  let L;
  try { L = await api("GET", "/api/rundowns"); } catch (e) { toast(esc(e.message), "crit"); return; }
  if (!L.length) { toast("Nenhum job anterior com roteiro para copiar.", "warn"); return; }
  const b = modal("COPIAR ROTEIRO DE OUTRO DIA", `<div class="hint" style="margin:0 0 10px">Copia só os <b>blocos e tempos</b>. Depois é só ajustar o que mudou — salva sozinho.</div>
    <input id="rcq" placeholder="Buscar por job, cliente ou local…" style="width:100%;margin-bottom:10px">
    <div id="rcl" class="rclist"></div>`);
  const draw = q => {
    q = (q || "").toLowerCase();
    $("#rcl", b).innerHTML = L.map((r, i) => ({ r, i })).filter(({ r }) => !q || [r.jobName, r.client, r.venue].join(" ").toLowerCase().includes(q)).map(({ r, i }) => `
      <div class="rcitem"><div class="rch"><b>${esc(r.jobName)}</b><span class="mu sm">${esc(dt(r.opened))}${r.client ? " · " + esc(r.client) : ""}${r.venue ? " · " + esc(r.venue) : ""}</span></div>
        <div class="mu sm">${r.items.length} bloco${r.items.length > 1 ? "s" : ""}${r.total ? " · " + hms(r.total) + " planejados" : ""}</div>
        <div class="rcprev">${r.items.slice(0, 8).map(x => `<span>${esc(x.title)}${x.dur ? ` <i>${hms(x.dur)}</i>` : ""}</span>`).join("")}${r.items.length > 8 ? `<span class="mu">+${r.items.length - 8}</span>` : ""}</div>
        <div class="row" style="justify-content:flex-end;margin-top:8px"><button class="btn" onclick="rdCopyApply(${i},'add')">+ ADICIONAR NO FIM</button><button class="btn red" onclick="rdCopyApply(${i},'replace')">⧉ USAR ESTE ROTEIRO</button></div></div>`).join("") || `<div class="empty">Nada encontrado.</div>`;
  };
  $("#rcq", b).oninput = e => draw(e.target.value);
  draw("");
  window.rdCopyApply = async (i, mode) => {
    const src = L[i].items.map(x => ({ title: x.title, dur: x.dur }));
    const r = S.rundown || {}, running = r.current >= 0 && !valid(r.ended);
    const has = (rdEdit || []).filter(x => x.title.trim()).length;
    if (mode === "replace" && (has || running)) {
      closeModal();
      const ok = await confirmBox("TROCAR O ROTEIRO", `Trocar o roteiro atual (${has} bloco${has === 1 ? "" : "s"}) pelo de <b>${esc(L[i].jobName)}</b> (${src.length} blocos)?${running ? `<br><br><b style="color:var(--warn)">O programa está rodando.</b> O relógio continua; só a lista de blocos muda.` : ""}`, "TROCAR");
      if (!ok) return;
    } else closeModal();
    rdEdit = mode === "add" ? [...(rdEdit || []), ...src] : src;
    drawRd();
    await rdSave(true);
    toast(`Roteiro de ${esc(L[i].jobName)} ${mode === "add" ? "adicionado no fim" : "copiado"}: ${src.length} blocos.`, "ok");
  };
};
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
