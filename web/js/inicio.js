// RACK MONITOR BY DON — painel web · TALLY, TELÃO e início (carrega por último)
"use strict";
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
  const loadedVer = WHO.version || "";
  // o monitor foi atualizado (ou trocado de versão): recarrega a página para usar a tela nova
  const verCheck = async () => {
    WHO = await api("GET", "/api/whoami").catch(() => WHO);
    if (loadedVer && WHO.version && WHO.version !== loadedVer) {
      toast(`Monitor atualizado para a V${esc(WHO.version)} — recarregando a tela…`, "ok");
      setTimeout(() => location.reload(), 1500);
    }
  };
  setInterval(verCheck, 15000);
  window.addEventListener("focus", verCheck);
  const ev = await api("GET", "/api/events?min=trace").catch(() => []);
  EV = ev;
  route();
  connect();
})();
