// RACK MONITOR BY DON — painel web · CONFIGURAÇÃO
"use strict";
// ---------------- CONFIGURAÇÃO ----------------
let CFG = null, showPorts = false;
async function vConfig() {
  if (staticRendered === "cfg") return;
  staticRendered = "cfg";
  const r = await api("GET", "/api/config"); CFG = r.config; CFG._pinSet = r.pinSet; CFG._adminSet = r.adminPinSet; CFG._tgSet = r.telegramTokenSet; CFG._emSet = r.emailPassSet; CFG._local = r.local; CFG._dir = r.dataDir;
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
    <div class="form" style="margin-top:8px"><label>TOKEN DO BOT<input type="password" value="" placeholder="${c._tgSet ? "•••• (salvo — digite para trocar)" : "cole o token do @BotFather"}" onchange="CFG.telegram=Object.assign(CFG.telegram||{}, {token:this.value.trim()})"></label><label>CHAT ID (pessoa ou grupo)<input value="${esc(c.telegram?.chatId || "")}" onchange="CFG.telegram=Object.assign(CFG.telegram||{}, {chatId:this.value.trim()})"></label></div>
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
    <div class="form" style="margin-top:8px">${[["host", "SERVIDOR (ex.: smtp.gmail.com)"], ["port", "PORTA (587 ou 465)"], ["user", "USUÁRIO"], ["pass", "SENHA / SENHA DE APP"], ["from", "REMETENTE"], ["to", "DESTINATÁRIOS (vírgula)"]].map(([k, l]) => `<label>${l}<input ${k === "pass" ? `type="password" placeholder="${c._emSet ? "•••• (salva — digite para trocar)" : ""}"` : k === "port" ? 'type="number"' : ""} value="${k === "pass" ? "" : esc(c.email?.[k] ?? "")}" onchange="CFG.email=Object.assign(CFG.email||{},{${k}:${k === "port" ? "+this.value" : "this.value.trim()"}})"></label>`).join("")}</div>
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
      <label>PIN PARA LIBERAR (obrigatório para formatar)<input type="password" placeholder="${c._pinSet ? "•••• (definido — digite para trocar)" : "sem PIN"}" onchange="CFG.newPin=this.value"></label>
      <label>SENHA DE ADMINISTRADOR (alterar a configuração por outro aparelho)<input type="password" placeholder="${c._adminSet ? "•••• (definida — digite para trocar)" : "sem senha: só este computador altera"}" onchange="CFG.newAdminPin=this.value"></label></div>
      <div class="hint" style="margin-top:8px">Sem senha de administrador, a configuração só pode ser alterada <b>neste computador</b>; tablets e celulares continuam vendo tudo. Padrão de fábrica: <b>somente leitura</b>. Mesmo permitidos, os comandos ficam travados até alguém clicar em 🔒 COMANDOS, e a liberação expira sozinha. Tudo é registrado na linha do tempo.</div>`
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
        <label class="chk"><input type="checkbox" ${CFG.updateAuto ? "checked" : ""} onchange="CFG.updateAuto=this.checked">Instalar sozinho quando sair versão nova (só sem REC, com a vigilância desarmada e longe dos horários da agenda)</label>
        <label class="chk"><input type="checkbox" ${CFG.updateBeta ? "checked" : ""} onchange="CFG.updateBeta=this.checked">Receber versões de teste (beta) — só em um computador de testes</label>
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
  const body = Object.assign({}, CFG); ["_pinSet", "_adminSet", "_tgSet", "_emSet", "_local", "_dir", "_prefix"].forEach(k => delete body[k]);
  if (CFG.newPin === undefined) delete body.newPin;
  if (CFG.newAdminPin === undefined) delete body.newAdminPin;
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
