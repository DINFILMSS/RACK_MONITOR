// RACK MONITOR BY DON — painel web · JOBS, relatórios, resumo, inventário de cartões e relatório de I/O
"use strict";
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
