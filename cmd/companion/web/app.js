'use strict';
const $ = s => document.querySelector(s);
const $$ = s => Array.from(document.querySelectorAll(s));
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));

async function api(path, body, method) {
  const opt = {method: method || (body ? 'POST' : 'GET'), headers: {}};
  if (body) { opt.body = JSON.stringify(body); opt.headers['Content-Type'] = 'application/json'; }
  const r = await fetch('/api/' + path, opt);
  const j = await r.json().catch(() => ({error: 'risposta non valida'}));
  if (r.status === 401 && j.login) { location.href = '/login.html'; }
  if (!r.ok || j.error) throw new Error(j.error || ('HTTP ' + r.status));
  return j;
}
function toast(msg, kind) {
  const t = document.createElement('div'); t.className = 'toast ' + (kind || ''); t.textContent = msg;
  $('#toasts').appendChild(t); setTimeout(() => t.remove(), kind === 'err' ? 8000 : 4000);
}
function dlg(html, okLabel) {
  return new Promise(res => {
    $('#dlg-body').innerHTML = html; $('#dlg-ok').textContent = okLabel || 'OK';
    window._dlgRes = res; $('#dlg').showModal();
  });
}
function dlgClose(ok) { $('#dlg').close(); window._dlgRes && window._dlgRes(ok); }

// ---------- navigazione ----------
function go(v) {
  $$('nav a').forEach(a => a.classList.toggle('active', a.dataset.view === v));
  $$('.view').forEach(s => s.classList.toggle('active', s.dataset.view === v));
  try { if (v !== 'dash' && liveOn) liveToggle(); } catch (e) {} // la vista live sta solo nella Dashboard
  if (v === 'dats') loadDats();
  if (v === 'bios') loadBios(false);
  if (v === 'games') loadSystems();
  if (v === 'settings') { loadConfig(); cabLoad(); haLoad(); vbLoad(); tsLoad(); wkLoad(); sgLoad(); ssLoad(); }
  if (v === 'roms') datWarning();
  if (v === 'look') lkLoad();
  if (v === 'check') rlLoad();
  if (v === 'emu') emuLoad();
  if (v === 'test') { sxLoad(); pfLoad(); }
  if (v === 'addons') { adLoad(false); asLoad(); if (!fhLoaded) fhLoad(); }
  if (v === 'backup') bkLoad();
  if (v === 'plancia') plStart(); else plStop();
  if (v === 'look') stockLoad();
  try { localStorage.setItem('view', v); } catch (e) {}
  const cur = $(`nav a[data-view="${v}"]`); if (cur && $('#menu-cur')) $('#menu-cur').textContent = cur.textContent.trim();
  $('#side').classList.remove('menu-open');
  if (location.hash !== '#' + v) history.replaceState(null, '', '#' + v);
  window.scrollTo(0, 0);
}
function toggleMenu() { $('#side').classList.toggle('menu-open'); }
$$('nav a').forEach(a => a.onclick = () => go(a.dataset.view));
// link diretti alle schede: …/#look, …/#shell, …
window.addEventListener('hashchange', () => { const v = location.hash.slice(1); if (v && $(`nav a[data-view="${v}"]`)) go(v); });

// ---------- heartbeat + stato ----------
setInterval(() => fetch('/api/ping').catch(() => {}), 2000);
fetch('/api/ping').then(r => r.json()).then(j => $('#ver').textContent = 'v' + j.version).catch(() => {});

let cfg = null;
async function refreshStatus() {
  try {
    const s = await api('status');
    $('#dash-host').textContent = s.host + (s.hostname ? '  ·  ' + s.hostname : '') + (s.ips && s.ips.length ? '  ·  ' + s.ips.join(', ') : '');
    $('#dot-ssh').className = 'dot ' + (s.ssh_online ? 'on' : 'off');
    $('#dot-es').className = 'dot ' + (s.es_online ? 'on' : 'off');
    $('#st-state').textContent = (s.ssh_online ? (s.running ? 'In gioco' : (s.es_online ? 'Nel menu' : 'Acceso')) : 'Spento / non raggiungibile') + (s.via_remote ? ' · via Tailscale' : '');
    $('#st-run').textContent = s.running ? (s.running.name + ' (' + s.running.system + ')') : (s.ssh_online ? '' : (s.ssh_error || ''));
    $('#st-up').textContent = s.uptime || '—';
    $('#st-ver').textContent = s.version ? 'Batocera ' + s.version : '';
    $('#st-disk').textContent = s.disk_free || '—';
    $('#st-diskbar').style.width = (s.disk_pct || 0) + '%';
    $('#st-temp').textContent = s.temp_c ? s.temp_c.toFixed(0) + ' °C' : '—';
    $('#st-roms').textContent = s.rom_total ? s.rom_total + ' ROM' : '';
    $('#st-reconn').hidden = !!s.ssh_online;
    statusEvery(s.ssh_online ? 10000 : 4000); // spento: riprova più spesso
  } catch (e) {
    $('#dot-ssh').className = 'dot off'; $('#dot-es').className = 'dot off';
    statusEvery(4000);
  }
}
let _statusTimer = null, _statusMs = 0;
function statusEvery(ms) {
  if (ms === _statusMs) return;
  _statusMs = ms; clearInterval(_statusTimer); _statusTimer = setInterval(refreshStatus, ms);
}
refreshStatus(); statusEvery(10000);
async function reconnect() {
  const btns = $$('#reconn-btn, #st-reconn button'); btns.forEach(b => { b.disabled = true; b.textContent = '⏳ Cerco…'; });
  try {
    const r = await api('reconnect', {});
    toast(r.ok ? 'Cabinato trovato' + (r.via_remote ? ' (via Tailscale)' : '') : (r.error || 'Non trovato'), r.ok ? 'ok' : 'err');
  } catch (e) { toast(e.message, 'err'); }
  btns.forEach(b => { b.disabled = false; b.textContent = '🔄 Cerca il cabinato'; });
  refreshStatus();
}
let CAB_SYSTEMS = null; // tutti i sistemi definiti su Batocera (es_systems.cfg), anche le cartelle vuote
api('info').then(i => { if (i && i.systems) { CAB_SYSTEMS = Object.keys(i.systems).sort(); } }).catch(() => {}).then(() => refreshStatus());

// ---------- comandi ----------
async function cmd(action, arg) {
  try {
    const r = await api('cmd', {action, arg});
    if (action === 'screenshot' && r.url) {
      $('#shot-wrap').hidden = false; $('#shot').src = r.url + '?' + Date.now(); $('#shot-path').textContent = r.file;
      toast('Screenshot salvato: ' + r.file, 'ok');
    } else toast('Fatto: ' + action, 'ok');
    setTimeout(refreshStatus, 1500);
  } catch (e) { toast(e.message, 'err'); }
}
async function confirmCmd(action, q) { if (await dlg('<p>' + esc(q) + '</p>', 'Sì')) cmd(action); }
async function notifyPrompt() {
  if (await dlg('<p>Messaggio da mostrare sullo schermo del cabinato:</p><input id="dlg-in" class="in" value="Ciao dal PC!">', 'Invia')) cmd('notify', $('#dlg-in').value);
}
async function openWhat(what) { try { await api('open', {what}); } catch (e) { toast(e.message, 'err'); } }

// ---------- schermo live ----------
let liveOn = false;
function liveToggle() {
  const img = $('#live'), b = $('#live-btn');
  if (liveOn) { img.src = ''; liveCursor(img).stop(); $('#live-wrap').hidden = true; b.textContent = '👁 Mostra schermo'; b.classList.add('primary'); liveOn = false; $('#live-state').textContent = ''; return; }
  liveOn = true; $('#live-wrap').hidden = false; b.textContent = '⏹ Chiudi'; b.classList.remove('primary'); $('#live-state').textContent = 'collegamento…';
  clearInterval(window._liveProbe); window._liveProbe = setInterval(() => { if (liveOn && img.naturalWidth > 0) $('#live-state').textContent = 'in diretta'; }, 1000);
  img.onerror = () => { if (liveOn) { $('#live-state').textContent = 'stream interrotto (X non attivo? riprova)'; } };
  img.src = '/live?fps=' + $('#live-fps').value + '&w=' + $('#live-w').value + '&t=' + Date.now();
  liveCursor(img).start();
  wakeScreen();
}
window.addEventListener('beforeunload', () => { if (liveOn) $('#live').src = ''; });
// non lasciare la cattura accesa quando non la si guarda (finestra ridotta / altra scheda)
document.addEventListener('visibilitychange', () => { if (document.hidden && liveOn) liveToggle(); });
buildPad($('#pad'));
bindKeyboard($('#live-stage'), () => liveOn && $('#live-kb').checked);
$('#live-stage').addEventListener('click', () => $('#live-stage').focus());
function liveDetach() {
  if (liveOn) liveToggle();
  const w = window.open('/live.html?fps=' + $('#live-fps').value + '&w=' + $('#live-w').value, 'cablive', 'popup=yes,width=1000,height=860');
  if (!w) toast('Finestra bloccata dal browser', 'err');
}

// upload dal telefono → analisi
async function uploadAndAnalyze(files) {
  if (!files || !files.length) return;
  const fd = new FormData(); for (const f of files) fd.append('files', f);
  toast('Carico ' + files.length + ' file…');
  try { const r = await fetch('/api/upload', {method: 'POST', body: fd}).then(r => r.json()); if (r.error) throw new Error(r.error); analyze(r.paths); }
  catch (e) { toast(e.message, 'err'); }
}
function quitApp() { if (confirm('Chiudere Batocera Companion?')) { fetch('/api/quit', {method: 'POST'}); setTimeout(() => window.close(), 500); } }

// ---------- registrazione ----------
let recSince = null, recTimer = null;
function recRender(st) {
  const b = $('#rec-btn');
  if (st.recording) {
    b.textContent = '⏹ Ferma registrazione'; b.classList.remove('primary'); b.classList.add('danger');
    recSince = st.since ? new Date(st.since) : (recSince || new Date());
    if (!recTimer) recTimer = setInterval(() => { const s = Math.floor((Date.now() - recSince) / 1000); $('#rec-state').textContent = '● REC ' + String(Math.floor(s / 60)).padStart(2, '0') + ':' + String(s % 60).padStart(2, '0'); }, 1000);
  } else {
    b.textContent = '⏺ Registra schermo'; b.classList.add('primary'); b.classList.remove('danger');
    clearInterval(recTimer); recTimer = null; recSince = null;
    $('#rec-state').textContent = st.last ? 'ultima: ' + st.last + ' (' + fmtSize(st.last_size) + ')' + (st.tool === 'fallback' ? ' · ffmpeg' : '') : '';
  }
}
async function recToggle() {
  const b = $('#rec-btn'); b.disabled = true;
  try {
    const cur = await api('record');
    const st = await api('record', cur.recording ? {action: 'stop'} : {action: 'start', quality: $('#rec-q').value});
    recRender(st);
    if (!st.recording && st.last) { toast('Registrazione salvata: ' + st.last, 'ok'); recList(); }
  } catch (e) { toast(e.message, 'err'); }
  b.disabled = false;
}
async function recList() {
  try {
    const l = await api('record', {action: 'list'}); const t = $('#rec-table'); t.hidden = false;
    t.querySelector('tbody').innerHTML = l.length ? l.map(r => `<tr><td class="mono">${esc(r.name)}</td><td>${fmtSize(r.size)}</td><td>${esc(r.time)}</td><td style="text-align:right"><button class="btn sm" onclick="recDownload('${esc(r.name)}')">⬇ Scarica sul PC</button> <button class="btn sm ghost" onclick="recDelete('${esc(r.name)}')">🗑</button></td></tr>`).join('') : '<tr><td colspan="4" class="muted">Nessuna registrazione</td></tr>';
  } catch (e) { toast(e.message, 'err'); }
}
async function recDownload(name) {
  try {
    const r = await api('record', {action: 'download', file: name}); $('#rec-job').hidden = false;
    await pollJob(r.job, bindJobUI('rec')); $('#rec-job').hidden = true;
    toast('Salvato in ' + r.local, 'ok');
  } catch (e) { toast(e.message, 'err'); $('#rec-job').hidden = true; }
}
async function recDelete(name) {
  if (!await dlg('<p>Eliminare <code>' + esc(name) + '</code> dal cabinato?</p>', 'Elimina')) return;
  try { await api('record', {action: 'delete', file: name}); recList(); } catch (e) { toast(e.message, 'err'); }
}
api('record').then(recRender).catch(() => {});
setInterval(() => { if ($('.view.active').dataset.view === 'dash') api('record').then(recRender).catch(() => {}); }, 15000);

// ---------- job polling ----------
function pollJob(id, onTick) {
  return new Promise((res, rej) => {
    const t = setInterval(async () => {
      try {
        const j = await api('job?id=' + id);
        onTick && onTick(j);
        if (j.status !== 'running') { clearInterval(t); j.status === 'done' ? res(j) : rej(new Error(j.error || j.status)); }
      } catch (e) { clearInterval(t); rej(e); }
    }, 600);
  });
}
function bindJobUI(prefix) {
  const t0 = Date.now(); let p0 = null, t1 = 0, lastEta = null;
  return j => {
    const bar = $('#' + prefix + '-bar'), step = $('#' + prefix + '-step'), log = $('#' + prefix + '-log'), pct = $('#' + prefix + '-pct');
    if (bar) { bar.classList.toggle('indet', j.progress < 0); bar.style.width = j.progress < 0 ? '' : Math.round(j.progress * 100) + '%'; }
    let extra = '';
    if (j.progress >= 0) {
      // percentuale e stima del tempo mancante (dal ritmo osservato finora)
      if (p0 === null && j.progress > 0) { p0 = j.progress; t1 = Date.now(); }
      const el = (Date.now() - (t1 || t0)) / 1000, adv = j.progress - (p0 || 0);
      if (el > 4 && adv > 0.005 && j.progress < 1) {
        const rem = el * (1 - j.progress) / adv; lastEta = lastEta === null ? rem : lastEta * 0.7 + rem * 0.3;
        const r = Math.round(lastEta), txt = r < 60 ? r + ' s' : r < 3600 ? Math.round(r / 60) + ' min' : Math.floor(r / 3600) + ' h ' + String(Math.round((r % 3600) / 60)).padStart(2, '0') + ' min';
        extra = Math.round(j.progress * 100) + '% · ~' + txt + ' rimanenti';
      } else extra = Math.round(j.progress * 100) + '%';
    }
    if (pct) pct.textContent = extra;
    if (step) step.textContent = (j.step || '') + (extra && !pct && !/mancano|\d+%/.test(j.step || '') ? '  ·  ' + extra : '');
    if (log && j.log) { log.textContent = j.log.join('\n'); log.scrollTop = log.scrollHeight; }
  };
}

// ---------- ROM ----------
let romSession = null, romItems = [];
async function datWarning() {
  try {
    const d = await api('dats');
    const arc = d.filter(x => x.kind === 'arcade' && x.present).length;
    const con = d.filter(x => x.kind === 'console' && x.present).length;
    $('#dat-warning').innerHTML = arc === 0
      ? '⚠️ Nessun database arcade presente: i giochi MAME/FBNeo verranno copiati senza verifica. <a href="#" onclick="go(\'dats\');return false">Scarica i database</a>.'
      : `Database pronti: ${arc} arcade, ${con} console.`;
  } catch (e) {}
}
// ---------- scelta file: finestra di Windows sul PC, selettore del telefono da iPhone/altri dispositivi ----------
const IS_LOCAL = ['127.0.0.1', 'localhost', '::1'].includes(location.hostname);
const PICK_ACCEPT = {zip: '.zip,application/zip', video: 'video/*,.mp4,.mkv', image: 'image/*,.png,.jpg', dat: '.dat,.xml,.zip', bios: '', files: '', folder: ''};
function pickFiles(kind) {
  if (IS_LOCAL) return api('pick?kind=' + kind);
  // il click sull'input va fatto subito (iOS vuole il gesto dell'utente): nessun await prima
  return new Promise((resolve, reject) => {
    const inp = document.createElement('input');
    inp.type = 'file'; inp.style.display = 'none';
    inp.multiple = ['bios', 'files', 'folder'].includes(kind);
    if (PICK_ACCEPT[kind]) inp.accept = PICK_ACCEPT[kind];
    document.body.appendChild(inp);
    const done = v => { inp.remove(); resolve(v); };
    inp.addEventListener('cancel', () => done({paths: []}));
    inp.onchange = async () => {
      const files = [...inp.files];
      if (!files.length) return done({paths: []});
      const tot = files.reduce((a, f) => a + f.size, 0);
      toast(`Carico ${files.length > 1 ? files.length + ' file' : files[0].name} (${fmtSize(tot)})…`);
      const fd = new FormData(); for (const f of files) fd.append('files', f);
      try {
        const r = await fetch('/api/upload', {method: 'POST', body: fd}).then(r => r.json());
        if (r.error) throw new Error(r.error);
        inp.remove(); resolve({paths: r.paths || []});
      } catch (e) { inp.remove(); reject(e); }
    };
    inp.click();
  });
}

async function pick(kind) {
  try {
    const r = await pickFiles(kind);
    if (!r.paths || !r.paths.length) return;
    analyze(r.paths);
  } catch (e) { toast(e.message, 'err'); }
}
function analyzePath() {
  const p = $('#rom-path').value.trim().replace(/^"|"$/g, '');
  if (!p) return toast('Incolla un percorso', 'err');
  analyze([p]);
}
async function analyze(paths) {
  $('#rom-job').hidden = false; $('#rom-result').hidden = true;
  $('#rom-job-title').textContent = 'Analisi di ' + paths.length + ' elementi';
  try {
    const r = await api('roms/analyze', {paths});
    const j = await pollJob(r.job, bindJobUI('rom-job'));
    romSession = j.result.session; romItems = j.result.items;
    renderRoms(j.result.summary);
    $('#rom-job').hidden = true;
  } catch (e) { toast(e.message, 'err'); $('#rom-job-step').textContent = 'Errore: ' + e.message; }
}
const SYSTEMS = ['mame','fbneo','neogeo','nes','fds','snes','n64','gb','gbc','gba','virtualboy','nds','megadrive','mastersystem','gamegear','sega32x','sg1000','segacd','saturn','dreamcast','naomi','atomiswave','psx','ps2','psp','pcengine','supergrafx','pcenginecd','atari2600','atari5200','atari7800','lynx','ngp','ngpc','wswan','wswanc','colecovision','pokemini','gamecube','wii','3do','amiga500','c64','msx1','ports'];
let romSummary = null;
function renderRoms(summary) {
  const tb = $('#rom-table tbody'); tb.innerHTML = '';
  const flt = ($('#rom-filter') || {}).value || 'all';
  let shown = 0;
  for (const it of romItems) {
    if (flt === 'green' && it.confidence !== 'green') continue;
    if (flt === 'yellow' && it.confidence !== 'yellow') continue;
    if (flt === 'red' && it.confidence !== 'red') continue;
    if (flt === 'problems' && it.confidence === 'green') continue;
    shown++;
    const tr = document.createElement('tr'); tr.dataset.id = it.id; if (it.skip) tr.classList.add('skipped');
    const all = (CAB_SYSTEMS || SYSTEMS).slice();
    const first = (it.system_options && it.system_options.length ? it.system_options : []).slice();
    if (it.system && !first.includes(it.system)) first.unshift(it.system);
    const rest = all.filter(s => !first.includes(s));
    const opt = s => `<option ${s === it.system ? 'selected' : ''}>${s}</option>`;
    const sysSel = `<select class="in sel-sm" onchange="romSet('${it.id}',{system:this.value})"><option value="">— scegli —</option>${first.map(opt).join('')}${first.length && rest.length ? '<option disabled>──────</option>' : ''}${rest.map(opt).join('')}</select>`;
    let coreCell = esc(it.core || '');
    if (it.core_matches && it.core_matches.length) {
      coreCell = `<select class="in sel-sm" onchange="romSet('${it.id}',{core:this.value})">${it.core_matches.map(c => `<option value="${c.core}" ${c.core === it.core ? 'selected' : ''} ${c.complete ? '' : 'disabled'}>${c.core}${c.complete ? '' : ' (incompleto ' + c.have + '/' + c.total + ')'}</option>`).join('')}</select>`;
      if (it.per_game_core) coreCell += '<span class="tag">per-gioco</span>';
    }
    const action = {copy: 'copia', rebuild: 'ricostruisci', extract: 'estrai', chd: 'CHD', skip: 'salta'}[it.action] || it.action;
    const notes = (it.notes || []).map(n => `<div>• ${esc(n)}</div>`).join('') + (it.missing && it.missing.length ? `<div class="miss">mancano: ${esc(it.missing.slice(0, 5).join(', '))}${it.missing.length > 5 ? ' …(+' + (it.missing.length - 5) + ')' : ''}</div>` : '');
    tr.innerHTML = `<td><input type="checkbox" ${it.skip || it.action === 'skip' ? '' : 'checked'} ${it.action === 'skip' ? 'disabled' : ''} onchange="romSet('${it.id}',{skip:!this.checked})" title="includi"> <span class="sem ${it.confidence}"></span></td>
      <td class="mono" title="${esc(it.sources.join('\n'))}">${esc(it.display_name)}${it.sources.length > 1 ? ' <span class="tag">+' + (it.sources.length - 1) + '</span>' : ''}<div class="muted">${fmtSize(it.size)}</div></td>
      <td>${esc(it.game_name || '')}</td><td>${sysSel}</td><td>${coreCell}</td><td>${action}${it.exists ? '<span class="tag err">sovrascrive</span>' : ''}</td>
      <td class="mono">${esc(it.system ? it.system + '/' + it.dest_name : '')}</td><td class="notes">${notes}</td>`;
    tb.appendChild(tr);
  }
  if (summary) romSummary = summary;
  const sm = romSummary || {total: romItems.length};
  $('#rom-summary').textContent = `${sm.total} elementi · ${sm.green || 0} ok · ${sm.yellow || 0} da verificare · ${sm.red || 0} non copiabili` + (flt !== 'all' ? ` · mostrati ${shown}` : '');
  if (shown === 0 && romItems.length) { const tr = document.createElement('tr'); tr.innerHTML = '<td colspan="8" class="hint" style="text-align:center;padding:14px">Nessun elemento in questa categoria.</td>'; tb.appendChild(tr); }
  $('#rom-result').hidden = false;
  $('#rom-apply').disabled = !romItems.some(i => !i.skip && i.action !== 'skip' && i.system);
}
async function romSet(id, patch) {
  try {
    const it = await api('roms/set', Object.assign({session: romSession, id}, patch));
    romItems = romItems.map(x => x.id === id ? it : x);
    renderRoms(summ(romItems));
  } catch (e) { toast(e.message, 'err'); }
}
function summ(items) { const m = {total: items.length}; items.forEach(i => m[i.confidence] = (m[i.confidence] || 0) + 1); return m; }
function fmtSize(b) { if (!b) return ''; const u = ['B', 'KB', 'MB', 'GB']; let i = 0; while (b >= 1024 && i < 3) { b /= 1024; i++; } return b.toFixed(i ? 1 : 0) + ' ' + u[i]; }
async function applyRoms() {
  const n = romItems.filter(i => !i.skip && i.action !== 'skip' && i.system).length;
  const over = romItems.filter(i => !i.skip && i.exists).length;
  if (!await dlg(`<p>Copio <b>${n}</b> giochi sul cabinato${over ? ` (<b>${over}</b> verranno sovrascritti)` : ''}.</p><p class="hint">I file .cue/.bin convertiti in CHD richiedono qualche minuto ciascuno.</p>`, 'Copia')) return;
  $('#rom-job').hidden = false; $('#rom-job-title').textContent = 'Copia sul cabinato'; $('#rom-job-log').textContent = '';
  try {
    const r = await api('roms/apply', {session: romSession});
    await pollJob(r.job, bindJobUI('rom-job'));
    toast('Copia completata', 'ok'); $('#rom-result').hidden = true; refreshStatus();
  } catch (e) { toast(e.message, 'err'); }
}

// ---------- CHECK-UP ----------
let lastReport = null, checkFilter = 'all';
// ---------- emulatori per sistema ----------
let emuSys = null, emuData = null;
async function emuLoad() {
  try {
    const r = await api('emu');
    $('#emu-hw').innerHTML = `PC del cabinato: <b>${esc(r.cpu || 'sconosciuto')}</b> · ${esc(r.tier_desc)}`;
    $('#emu-systems').innerHTML = r.systems.map(s => `<div class="sysrow ${s.games ? '' : 'empty'} ${s.name === emuSys ? 'on' : ''}" data-sys="${esc(s.name)}" onclick="emuOpen('${esc(s.name)}')"><span>${esc(s.fullname || s.name)}<br><span class="n">${esc(s.name)}${s.custom ? ' · ' + s.custom + ' personalizzate' : ''}</span></span><span class="n">${s.games ? s.games + ' giochi' : '—'}</span></div>`).join('');
  } catch (e) { $('#emu-systems').textContent = 'Non raggiungibile: ' + e.message; }
}
async function emuOpen(sys) {
  emuSys = sys; $$('#emu-systems .sysrow').forEach(el => el.classList.toggle('on', el.dataset.sys === sys));
  $('#emu-form').innerHTML = '<div class="hint">Carico…</div>';
  try { emuData = await api('emu?system=' + encodeURIComponent(sys)); } catch (e) { $('#emu-form').innerHTML = esc(e.message); return; }
  const d = emuData; const reco = {}; (d.recommended || []).forEach(r => reco[r.key] = r);
  const lab = (st, v) => { if (st.type === 'bool') return v === '' ? 'automatico' : (v === '1' ? 'sì' : 'no'); const o = (st.options || []).find(o => o.value === v); return o ? o.label : (v || 'automatico'); };
  let h = `<div class="top"><h2 style="margin:0">${esc(d.fullname || sys)} <span class="muted" style="font-size:13px">${esc(sys)} · ${d.games} giochi</span></h2>
    <div class="btns"><button class="btn" onclick="emuApplyReco()">✨ Applica tutti i consigliati</button><button class="btn primary" onclick="emuSave()">💾 Salva</button></div></div>
    ${d.per_game_overrides ? `<div class="hint" style="margin-top:6px">${d.per_game_overrides} giochi di questo sistema hanno impostazioni proprie (per gioco) che prevalgono su queste.</div>` : ''}`;
  for (const st of d.settings) {
    const rc = reco[st.key];
    let ctl;
    if (st.type === 'bool') ctl = `<select class="in" data-k="${st.key}"><option value="">Automatico${st.global ? ' (globale: ' + (st.global === '1' ? 'sì' : 'no') + ')' : ''}</option><option value="1" ${st.value === '1' ? 'selected' : ''}>Sì</option><option value="0" ${st.value === '0' ? 'selected' : ''}>No</option></select>`;
    else ctl = `<select class="in" data-k="${st.key}">${(st.options || []).map(o => `<option value="${esc(o.value)}" ${o.value === st.value ? 'selected' : ''}>${esc(o.label)}${o.value === '' && st.global ? ' (globale: ' + esc(st.global) + ')' : ''}</option>`).join('')}${st.value && !(st.options || []).some(o => o.value === st.value) ? `<option value="${esc(st.value)}" selected>${esc(st.value)} (attuale)</option>` : ''}</select>`;
    h += `<div class="eset"><div class="top"><div><b>${esc(st.label)}</b><div class="cur">attuale: ${esc(lab(st, st.value))}</div></div>${ctl}</div>
      <div class="desc">${esc(st.desc)}</div>${st.perf ? `<div class="perf">Prestazioni: ${esc(st.perf)}</div>` : ''}
      ${rc ? `<div class="reco">✔ Consigliato per questo PC: <b>${esc(lab(st, rc.value))}</b> — ${esc(rc.reason)} ${rc.value !== st.value ? `<button class="btn sm" onclick="emuSet('${st.key}','${esc(rc.value)}')">usa</button>` : '<span class="muted">(già impostato)</span>'}</div>` : ''}</div>`;
  }
  $('#emu-form').innerHTML = h;
}
function emuSet(k, v) { const el = $(`#emu-form [data-k="${k}"]`); if (el) el.value = v; }
function emuApplyReco() { (emuData.recommended || []).forEach(r => emuSet(r.key, r.value)); toast('Valori consigliati impostati: premi Salva per scriverli sul cabinato', 'ok'); }
async function emuSave() {
  const values = {}; let changed = 0;
  for (const st of emuData.settings) { const el = $(`#emu-form [data-k="${st.key}"]`); if (!el) continue; if (el.value !== st.value) { values[st.key] = el.value; changed++; } }
  if (!changed) return toast('Nessuna modifica', 'ok');
  try { const r = await api('emu', {system: emuSys, values}); toast(`${changed} impostazioni salvate`, 'ok'); await dlg('<p><b>Salvato in batocera.conf</b></p><pre class="log">' + esc((r.log || []).join('\n')) + '</pre><p class="hint">Valgono dal prossimo avvio di un gioco di questo sistema.</p>', 'OK'); emuOpen(emuSys); emuLoad(); }
  catch (e) { toast(e.message, 'err'); }
}

// ---------- libreria ROM ----------
let rlReport = null, rlOk = new Set();
async function stRun() {
  $('#st-job').hidden = false; $('#st-out').innerHTML = '';
  try {
    const r = await api('stutter', {});
    const res = (await pollJob(r.job, jj => { $('#st-step').textContent = jj.step || ''; })).result;
    const ico = {ok: '✅', info: 'ℹ️', warn: '⚠️', error: '❌'};
    $('#st-out').innerHTML = `<div class="hint" style="margin-bottom:8px"><b>${esc(res.summary)}</b></div>` + res.items.map(it => `<div class="finding ${it.level}"><i></i><div class="body"><span class="t">${esc(it.title)}</span>${it.detail ? `<div class="d">${esc(it.detail)}</div>` : ''}${it.fix ? `<div class="m" style="font-size:12px"><code>${esc(it.fix)}</code></div>` : ''}</div><div class="act" style="padding:10px 10px 0 0">${it.fix ? `<button class="btn sm" onclick="stFix(this, ${JSON.stringify(it.fix).replace(/"/g, '&quot;')})">🔧 Sistema</button>` : ''}</div></div>`).join('');
  } catch (e) { toast(e.message, 'err'); }
  $('#st-job').hidden = true;
}
async function stFix(btn, cmd) {
  btn.disabled = true;
  try { const r = await api('shell', {cmd}); toast(r.exit ? 'Comando terminato con codice ' + r.exit : 'Fatto', r.exit ? 'err' : 'ok'); if (r.stdout || r.stderr) console.log(r.stdout, r.stderr); }
  catch (e) { toast(e.message, 'err'); }
  btn.disabled = false;
}
async function rlLoad() {
  try { const r = await api('romlib'); rlOk = new Set(r.ok_flags || []); if (r.available) { rlReport = r.report; rlRender(new Date(r.at)); } } catch (e) {}
}
async function rlRun() {
  $('#rl-job').hidden = false; $('#rl-log').textContent = ''; $('#rl-out').innerHTML = '';
  try {
    const r = await api('romlib', {});
    const res = await pollJob(r.job, jj => { $('#rl-step').textContent = jj.step || ''; $('#rl-log').textContent = (jj.log || []).join('\n'); });
    rlReport = res.result.report; rlRender(new Date(res.result.at)); toast('Analisi completata', 'ok');
    if (lastReport) runCheckup();
  } catch (e) { toast(e.message, 'err'); }
  $('#rl-job').hidden = true;
}
function rlRender(at) {
  const rep = rlReport; if (!rep) return;
  rep.systems = rep.systems || []; rep.discs = rep.discs || []; rep.overrides = rep.overrides || []; rep.duplicates = rep.duplicates || [];
  rep.systems.forEach(s => { s.files = s.files || []; });
  const noM3U = rep.discs.filter(g => !g.has_m3u).length, badCores = rep.overrides.filter(o => !o.installed).length;
  $('#rl-fix-md').hidden = rep.discs.length === 0; $('#rl-fix-cores').hidden = badCores === 0;
  $('#rl-fix-bios').hidden = !rep.systems.some(s => s.files.some(f => f.set));
  const st = (n, cls) => n ? `<span class="tag ${cls}">${n}</span>` : '<span class="muted">0</span>';
  let h = `<div class="hint">Ultima analisi: ${at.toLocaleString()} · ${esc(rep.summary)}</div>
    <table class="tbl" style="margin-top:8px"><thead><tr><th>Sistema</th><th>Giochi</th><th>Riconosciuti</th><th>Set incompleti</th><th>Sconosciuti</th><th>Est. ignorata</th><th></th></tr></thead><tbody>`;
  for (const s of rep.systems) {
    if (!s.total && !s.hidden_ext) continue;
    h += `<tr><td><b>${esc(s.name)}</b>${s.verified ? '' : ' <span class="muted" title="nessun DAT per questo sistema">(non verificabile)</span>'}</td><td>${s.total}</td><td>${st(s.ok, 'ok')}</td><td>${st(s.incomplete, 'err')}${(n => n ? ` <span class="tag ok" title="provati a mano e funzionanti">${n} ok</span>` : '')(s.files.filter(f => (f.status === 'incomplete' || f.status === 'unknown' || f.status === 'bad') && rlOk.has('/userdata/roms/' + s.name + '/' + f.path)).length)}</td><td>${st(s.unknown, 'miss')}</td><td>${st(s.hidden_ext, 'miss')}</td><td>${(s.incomplete || s.unknown || s.hidden_ext || s.bad) ? `<button class="btn sm ghost" onclick="rlDetail('${esc(s.name)}')">dettagli</button>` : ''}</td></tr>`;
  }
  h += '</tbody></table>';
  if (rep.discs.length) h += `<p style="margin-top:10px"><b>Multi-disco:</b> ${rep.discs.length} giochi (${noM3U} senza .m3u)</p><ul class="hint">` + rep.discs.map(g => `<li>${esc(g.system)}: ${esc(g.title)} — ${g.files.length} dischi ${g.has_m3u ? '<span class="tag ok">.m3u ok</span>' : '<span class="tag err">dischi separati</span>'}</li>`).join('') + '</ul>';
  if (badCores) h += `<p style="margin-top:10px"><b class="err">Core non installati:</b></p><ul class="hint">` + rep.overrides.filter(o => !o.installed).map(o => `<li>${esc(o.System)}/${esc(o.File)} → <code>${esc(o.Value)}</code></li>`).join('') + '</ul>';
  const vars = rep.variants || [];
  if (vars.length) {
    const nDel = vars.reduce((a, v) => a + v.files.filter(f => !f.keep).length, 0);
    h += `<p style="margin-top:10px"><b>Stesso gioco in più versioni</b> (${vars.length} giochi, ${nDel} copie in più): revisioni, regioni, beta, bootleg. Per ciascuno è proposta la copia migliore da tenere (dump riconosciuto, revisione più alta, Italia/Europa/World prima di USA e Giappone, niente beta/proto/hack/bootleg); cambia la scelta se preferisci un'altra versione.</p>
      <table class="tbl" id="rl-vars"><thead><tr><th style="width:30px"><input type="checkbox" checked onchange="$$('#rl-vars .var-ck').forEach(c => c.checked = this.checked)"></th><th>Gioco</th><th>Versioni</th></tr></thead><tbody>` + vars.slice(0, 300).map((v, i) => `<tr><td><input type="checkbox" class="var-ck" data-i="${i}" checked></td><td><b>${esc(v.title)}</b><div class="muted" style="font-size:11px">${esc(v.system)}</div></td><td>${v.files.map((f, k) => `<label class="chk" style="display:flex;gap:6px;align-items:center;margin:2px 0"><input type="radio" name="var-keep-${i}" value="${k}" ${f.keep ? 'checked' : ''}> <span class="mono" style="font-size:12px">${esc(f.path)}</span> <span class="muted" style="font-size:11px">${esc(f.why)}</span></label>`).join('')}<div class="muted" style="font-size:11px">la versione selezionata resta, le altre vanno nel cestino</div></td></tr>`).join('') + `</tbody></table><div class="btns" style="margin-top:8px"><button class="btn" onclick="rlDelVariants()">🗑 Togli le versioni in più selezionate</button></div>` + (vars.length > 300 ? '<div class="hint">(mostrati i primi 300)</div>' : '');
  }
  const dups = rep.dup_pairs || [];
  if (dups.length) h += `<p style="margin-top:10px"><b>Duplicati</b> (${dups.length} file identici, stesso CRC): per ogni coppia scegli quale copia togliere; va nel cestino del cabinato.</p><table class="tbl" id="rl-dups"><thead><tr><th style="width:30px"><input type="checkbox" onchange="$$('#rl-dups .dup-ck').forEach(c => c.checked = this.checked)"></th><th>Togli</th><th>Tieni</th></tr></thead><tbody>` + dups.slice(0, 200).map((d, i) => `<tr><td><input type="checkbox" class="dup-ck" data-i="${i}" checked></td><td class="mono"><select class="in dup-sel" data-i="${i}" style="font-size:12px;max-width:100%"><option value="${esc(d.b)}">${esc(d.b)}</option><option value="${esc(d.a)}">${esc(d.a)}</option></select></td><td class="mono muted dup-keep" data-i="${i}">${esc(d.a)}</td></tr>`).join('') + `</tbody></table><div class="btns" style="margin-top:8px"><button class="btn" onclick="rlDelDups()">🗑 Togli i duplicati selezionati</button></div>` + (dups.length > 200 ? '<div class="hint">(mostrati i primi 200)</div>' : '');
  $('#rl-out').innerHTML = h;
  $$('#rl-dups .dup-sel').forEach(sel => sel.onchange = () => { const d = (rlReport.dup_pairs || [])[+sel.dataset.i]; $(`#rl-dups .dup-keep[data-i="${sel.dataset.i}"]`).textContent = sel.value === d.a ? d.b : d.a; });
}
function rlDetail(sys) {
  const s = rlReport.systems.find(x => x.name === sys); if (!s) return;
  const rows = s.files.filter(f => f.status !== 'ok' && f.status !== 'aux');
  const lab = {unknown: 'sconosciuto', incomplete: 'set incompleto', bad: 'file danneggiato', hidden_ext: 'estensione ignorata'};
  const full = f => '/userdata/roms/' + sys + '/' + f.path;
  rows.sort((a, b) => (rlOk.has(full(a)) ? 1 : 0) - (rlOk.has(full(b)) ? 1 : 0));
  const nOk = rows.filter(f => rlOk.has(full(f))).length;
  dlg(`<p><b>${esc(sys)}</b>: ${rows.length} file da controllare${nOk ? ` · ${nOk} già provati e funzionanti` : ''}</p><p class="hint">▶ Prova avvia il gioco sul cabinato così com'è: se parte e gira, segnalo ✓ Funziona e resta memorizzato (anche dopo una nuova analisi); se non parte, 🗑 lo sposta nel cestino del cabinato.</p><div style="max-height:60vh;overflow:auto"><table class="tbl"><thead><tr><th>File</th><th>Stato</th><th>Note</th><th></th></tr></thead><tbody>${rows.map(f => `<tr data-rl="${esc(full(f))}" class="${rlOk.has(full(f)) ? 'rl-ok' : ''}"><td class="mono">${esc(f.path)}</td><td><span class="tag ${f.status === 'incomplete' ? 'err' : 'miss'}">${lab[f.status] || f.status}</span> <span class="tag ok rl-oktag" ${rlOk.has(full(f)) ? '' : 'hidden'}>funziona</span></td><td class="hint">${esc(f.name || '')} ${esc(f.detail || '')}${f.missing ? ` (mancano ${f.missing})` : ''}</td><td class="nowrap"><button class="btn sm" title="Avvia sul cabinato" onclick="rlTry(this, '${esc(full(f))}')">▶ Prova</button> <button class="btn sm ghost" title="Perché non parte?" onclick="gameDiag('${esc(sys)}', '${esc(full(f))}')">🩺</button> <button class="btn sm ghost" title="Chiudi il gioco sul cabinato" onclick="cmd('emukill')">⏹</button> <button class="btn sm ${rlOk.has(full(f)) ? 'primary' : 'ghost'}" title="Segna come provato e funzionante" onclick="rlFlag(this, '${esc(full(f))}')">✓ Funziona</button> <button class="btn sm ghost" title="Sposta nel cestino del cabinato" onclick="rlTrash(this, '${esc(sys)}', '${esc(full(f))}')">🗑</button></td></tr>`).join('')}</tbody></table></div>`, 'Chiudi');
}
async function gameDiag(sys, path) {
  const name = path.split('/').pop();
  const box = `<p><b>Perché non parte? — ${esc(name)}</b> <span class="muted">(${esc(sys)})</span></p><div id="diag-out"><div class="jobline"><span id="diag-step">Controllo set, core e BIOS, poi avvio il gioco per 12 s…</span></div><div class="bar big"><i class="indet"></i></div></div>`;
  const done = dlg(box, 'Chiudi');
  try {
    const r = await api('games/diag', {system: sys, path});
    const res = (await pollJob(r.job, jj => { const el = $('#diag-step'); if (el) el.textContent = jj.step || ''; })).result;
    const ico = {ok: '✅', info: 'ℹ️', warn: '⚠️', error: '❌'};
    const el = $('#diag-out'); if (!el) return;
    el.innerHTML = `<div class="hint" style="margin-bottom:8px"><b>${esc(res.summary)}</b></div>` + (res.items || []).map(it => `<div class="finding ${it.level}"><i></i><div class="body"><span class="t">${ico[it.level] || ''} ${esc(it.title)}</span>${it.detail ? `<div class="d">${esc(it.detail)}</div>` : ''}</div><div class="act" style="padding:10px 10px 0 0">${it.fix ? `<button class="btn sm" title="${esc(it.fix)}" onclick="stFix(this, ${JSON.stringify(it.fix).replace(/"/g, '&quot;')})">🔧 Sistema</button>` : ''}</div></div>`).join('');
  } catch (e) { const el = $('#diag-out'); if (el) el.innerHTML = '<div class="finding error"><i></i><div class="body"><span class="t">' + esc(e.message) + '</span></div></div>'; }
  await done;
}
async function rlFlag(btn, path) {
  const ok = !rlOk.has(path);
  try {
    const r = await api('romlib/flag', {path, ok}); rlOk = new Set(r.ok_flags || []);
    const tr = btn.closest('tr'); tr.classList.toggle('rl-ok', ok); tr.querySelector('.rl-oktag').hidden = !ok; btn.classList.toggle('primary', ok); btn.classList.toggle('ghost', !ok);
    toast(ok ? 'Segnato come funzionante' : 'Segno tolto', 'ok');
    if (lastReport) runCheckup();
  } catch (e) { toast(e.message, 'err'); }
}
async function rlTry(btn, path) {
  btn.disabled = true;
  try { await api('cmd', {action: 'launch', arg: path}); toast('Avviato sul cabinato: guarda lo schermo. ⏹ per chiuderlo.', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
  setTimeout(() => btn.disabled = false, 3000);
}
async function rlTrash(btn, sys, path) {
  const tr = btn.closest('tr');
  btn.disabled = true;
  try {
    await api('cmd', {action: 'emukill'}).catch(() => {});
    const r = await api('games/delete', {paths: [path], permanent: false, system: sys});
    const res = await pollJob(r.job, () => {});
    if (res.errors && res.errors.length) { toast(res.errors.join('; '), 'err'); btn.disabled = false; return; }
    toast('Nel cestino: ' + path.split('/').pop(), 'ok');
    tr.style.opacity = .4; tr.querySelectorAll('button').forEach(b => b.disabled = true);
    const sysRep = rlReport.systems.find(x => x.name === sys);
    if (sysRep) { const i = sysRep.files.findIndex(f => '/userdata/roms/' + sys + '/' + f.path === path); if (i >= 0) { const st = sysRep.files[i].status; sysRep.files.splice(i, 1); if (sysRep[st] > 0) sysRep[st]--; sysRep.total--; } }
  } catch (e) { toast(e.message, 'err'); btn.disabled = false; }
}
async function rlDelVariants() {
  const vars = rlReport.variants || [];
  const bySys = {};
  $$('#rl-vars .var-ck').forEach(ck => {
    if (!ck.checked) return;
    const i = +ck.dataset.i, v = vars[i];
    const keep = +($(`input[name="var-keep-${i}"]:checked`) || {value: 0}).value;
    v.files.forEach((f, k) => { if (k !== keep) (bySys[v.system] = bySys[v.system] || []).push('/userdata/roms/' + v.system + '/' + f.path); });
  });
  const n = Object.values(bySys).reduce((a, b) => a + b.length, 0); if (!n) return toast('Niente da togliere', 'err');
  if (!await dlg(`<p>Sposto nel cestino del cabinato <b>${n}</b> versioni in più (con le loro grafiche). Per ogni gioco resta la versione selezionata.</p>`, 'Togli')) return;
  $('#rl-job').hidden = false; $('#rl-log').textContent = '';
  let moved = 0;
  try {
    for (const [sys, paths] of Object.entries(bySys)) {
      const r = await api('games/delete', {paths, permanent: false, system: sys});
      const res = await pollJob(r.job, jj => { $('#rl-step').textContent = sys + ': ' + (jj.step || ''); $('#rl-log').textContent += (jj.log || []).join('\n'); });
      moved += res.moved || 0;
      if (res.errors && res.errors.length) toast(res.errors.join('; '), 'err');
    }
    toast(moved + ' file nel cestino', 'ok'); rlRun();
  } catch (e) { toast(e.message, 'err'); $('#rl-job').hidden = true; }
}
async function rlDelDups() {
  const dups = rlReport.dup_pairs || [];
  const bySys = {};
  $$('#rl-dups .dup-ck').forEach(ck => { if (!ck.checked) return; const d = dups[+ck.dataset.i]; const del = $(`#rl-dups .dup-sel[data-i="${ck.dataset.i}"]`).value; const sys = del.split('/')[0]; (bySys[sys] = bySys[sys] || []).push('/userdata/roms/' + del); });
  const n = Object.values(bySys).reduce((a, b) => a + b.length, 0); if (!n) return toast('Nessun duplicato selezionato', 'err');
  if (!await dlg(`<p>Sposto nel cestino del cabinato <b>${n}</b> file duplicati (con le loro grafiche). L'altra copia resta.</p>`, 'Togli')) return;
  $('#rl-job').hidden = false; $('#rl-log').textContent = '';
  let moved = 0;
  try {
    for (const [sys, paths] of Object.entries(bySys)) {
      const r = await api('games/delete', {paths, permanent: false, system: sys});
      const res = await pollJob(r.job, jj => { $('#rl-step').textContent = sys + ': ' + (jj.step || ''); $('#rl-log').textContent += (jj.log || []).join('\n'); });
      moved += res.moved || 0;
      if (res.errors && res.errors.length) toast(res.errors.join('; '), 'err');
    }
    toast(moved + ' file nel cestino', 'ok'); rlRun();
  } catch (e) { toast(e.message, 'err'); $('#rl-job').hidden = true; }
}
async function rlFix(action) {
  const q = action === 'bios' ? 'Per ogni cartella arcade (fbneo, mame, neogeo, cps…) cerco i BIOS richiesti dai set presenti (es. neogeo.zip per i giochi Neo Geo, pgm.zip per i PGM) che mancano nella cartella, e li copio da /userdata/bios o da un\'altra cartella ROM dove già ci sono. I BIOS copiati vengono nascosti dalla lista di EmulationStation. Procedo?' : action === 'multidisc' ? 'Creo gli .m3u mancanti (dove il sistema li supporta) e nascondo i singoli dischi dalla lista; per i sistemi senza .m3u resta visibile solo il disco 1. Procedo?' : 'Tolgo da batocera.conf le impostazioni per gioco che puntano a core non installati (il gioco tornerà al core predefinito del sistema). Prima salvo una copia di batocera.conf. Procedo?';
  if (!await dlg(`<p>${q}</p>`, 'Procedi')) return;
  $('#rl-job').hidden = false; $('#rl-log').textContent = '';
  try { const r = await api('romlib/fix', {action}); const res = await pollJob(r.job, jj => { $('#rl-step').textContent = jj.step || ''; $('#rl-log').textContent = (jj.log || []).join('\n'); }); toast('Fatto', 'ok'); if (action === 'bios') { $('#rl-job').hidden = false; await dlg('<p><b>BIOS nelle cartelle ROM</b></p><pre class="log">' + esc($('#rl-log').textContent) + '</pre>', 'OK'); if (res.result && res.result.copied) rlRun(); else $('#rl-job').hidden = true; } else rlRun(); }
  catch (e) { toast(e.message, 'err'); $('#rl-job').hidden = true; }
}

async function runCheckup() {
  $('#check-job').hidden = false; $('#check-list').innerHTML = ''; $('#check-score').hidden = true;
  try {
    const r = await api('checkup');
    const j = await pollJob(r.job, jj => $('#check-step').textContent = jj.step || '');
    lastReport = j.result; renderCheck(); renderDashSummary();
  } catch (e) { toast(e.message, 'err'); $('#check-step').textContent = 'Errore: ' + e.message; return; }
  $('#check-job').hidden = true;
}
function renderCheck() {
  const rep = lastReport; if (!rep) return;
  $('#check-score').hidden = false; $('#check-score b').textContent = rep.score;
  const areas = ['all', ...new Set(rep.findings.map(f => f.area))];
  $('#check-filters').innerHTML = areas.map(a => `<span class="chip ${a === checkFilter ? 'on' : ''}" onclick="checkFilter='${a}';renderCheck()">${a === 'all' ? 'tutti' : a}</span>`).join('');
  const list = $('#check-list'); list.innerHTML = '';
  for (const f of rep.findings) {
    if (checkFilter !== 'all' && f.area !== checkFilter) continue;
    const d = document.createElement('div'); d.className = 'finding ' + f.level; d.id = 'f-' + f.id;
    d.innerHTML = `<i></i><div class="body"><div><span class="area">${esc(f.area)}</span><span class="t">${esc(f.title)}</span></div>${f.detail ? `<div class="d">${esc(f.detail)}</div>` : ''}${f.manual ? `<div class="m">${esc(f.manual)}</div>` : ''}</div>
      <div class="act">${f.fix ? `<button class="btn sm primary" onclick="applyFix('${f.id}')">🔧 ${esc(f.fix.label)}</button><span class="hint" style="max-width:220px;text-align:right">${esc(f.fix.description || '')}</span>` : ''}</div>`;
    list.appendChild(d);
  }
}
async function applyFix(id) {
  const f = lastReport.findings.find(x => x.id === id); if (!f || !f.fix) return;
  if (!await dlg(`<p><b>${esc(f.fix.label)}</b></p><pre class="log">${esc(f.fix.commands.join('\n'))}</pre><p class="hint">Prima viene salvata una copia di batocera.conf in /userdata/system/companion_backup.${f.fix.needs_restart_es ? ' EmulationStation verrà riavviato.' : ''}</p>`, 'Applica')) return;
  try {
    const r = await api('checkup/fix', {commands: f.fix.commands, needs_restart_es: f.fix.needs_restart_es});
    const el = $('#f-' + CSS.escape(id));
    if (el) { el.classList.remove(f.level); el.classList.add(r.ok ? 'ok' : 'error'); const l = document.createElement('div'); l.className = 'fixlog'; l.textContent = (r.log || []).join('\n'); el.appendChild(l); el.querySelector('.act').innerHTML = r.ok ? '<span class="tag ok">applicato</span>' : '<span class="tag err">errore</span>'; }
    toast(r.ok ? 'Correzione applicata' : 'Errore applicando la correzione', r.ok ? 'ok' : 'err');
  } catch (e) { toast(e.message, 'err'); }
}
function renderDashSummary() {
  const rep = lastReport; if (!rep) return;
  const top = rep.findings.filter(f => f.level === 'error' || f.level === 'warn').slice(0, 6);
  $('#dash-summary').innerHTML = `<h2>Ultimo check-up: <span class="score">${rep.score}<span>/100</span></span></h2>
    <div class="hint">${rep.counts.error || 0} errori · ${rep.counts.warn || 0} avvisi · ${rep.counts.info || 0} suggerimenti · ${rep.counts.ok || 0} ok</div>
    ${top.map(f => `<div class="finding ${f.level}" style="margin-top:8px"><i></i><div class="body"><span class="area">${esc(f.area)}</span><span class="t">${esc(f.title)}</span></div><div class="act"><button class="btn sm" onclick="go('check')">apri</button></div></div>`).join('')}`;
}

// ---------- GIOCHI ----------
let games = [];
async function loadSystems() {
  try {
    const s = await api('systems'); const sel = $('#g-sys'); const cur = sel.value;
    sel.innerHTML = s.map(x => `<option value="${esc(x.name)}">${esc(x.fullname || x.name)} (${x.count})</option>`).join('');
    if (cur) sel.value = cur;
    loadGames();
  } catch (e) { toast('EmulationStation non risponde: ' + e.message, 'err'); }
}
const gSel = new Set();
async function loadGames() {
  const sys = $('#g-sys').value; if (!sys) return;
  gSel.clear(); $('#g-all').checked = false;
  try { games = await api('games?system=' + encodeURIComponent(sys)); renderGames(); } catch (e) { toast(e.message, 'err'); }
  gamesTrashInfo();
}
function renderGames() {
  if ($('#g-mame')) $('#g-mame').hidden = $('#g-sys').value !== 'mame';
  const q = $('#g-filter').value.toLowerCase(); const tb = $('#g-table tbody'); tb.innerHTML = '';
  const list = games.filter(g => !q || (g.name || '').toLowerCase().includes(q) || (g.path || '').toLowerCase().includes(q));
  $('#g-count').textContent = list.length + ' giochi';
  for (const g of list.slice(0, 500)) {
    const tr = document.createElement('tr'); const jp = JSON.stringify(g.path).replace(/"/g, '&quot;');
    tr.innerHTML = `<td><input type="checkbox" class="g-ck" ${gSel.has(g.path) ? 'checked' : ''}></td><td>${esc(g.name)}${g.hidden ? ' <span class="tag miss" title="nascosto in EmulationStation">nascosto</span>' : ''}</td><td class="mono muted" title="${esc(g.path || '')}">${esc((g.path || '').split('/').pop())}</td><td class="nowrap"><button class="btn sm" onclick="cmd('launch',${jp})">▶ Avvia</button> <button class="btn sm ghost" title="Perché non parte? Avvia il gioco, legge i log di Batocera e confronta il set con i DAT" onclick="gameDiag('${esc($('#g-sys').value)}',${jp})">🩺</button> <button class="btn sm ghost" title="Scheda: nome, descrizione, immagini" onclick="gameEdit('${esc(g.id)}')">✎</button> <button class="btn sm ghost" title="Elimina" onclick="gamesDelete([${jp}])">🗑</button></td>`;
    tr.querySelector('.g-ck').onchange = e => { e.target.checked ? gSel.add(g.path) : gSel.delete(g.path); gamesSelUI(); };
    tr.ondblclick = e => { if (e.target.tagName !== 'INPUT') cmd('launch', g.path); };
    tb.appendChild(tr);
  }
  gamesSelUI();
}
function gamesSelUI() { const n = gSel.size; $('#g-del').disabled = !n; $('#g-del').textContent = n ? `🗑 Elimina selezionati (${n})` : '🗑 Elimina selezionati'; }
function gamesAll(on) { $$('#g-table .g-ck').forEach(c => { c.checked = on; }); const q = $('#g-filter').value.toLowerCase(); games.filter(g => !q || (g.name || '').toLowerCase().includes(q) || (g.path || '').toLowerCase().includes(q)).forEach(g => on ? gSel.add(g.path) : gSel.delete(g.path)); gamesSelUI(); }
async function gamesDelete(paths) {
  paths = paths || [...gSel]; if (!paths.length) return;
  const perm = $('#g-perm').checked;
  const names = paths.slice(0, 8).map(p => '<li>' + esc(p.split('/').pop()) + '</li>').join('') + (paths.length > 8 ? `<li>… e altri ${paths.length - 8}</li>` : '');
  if (!await dlg(`<p>${perm ? '<b style="color:var(--err)">Elimino DEFINITIVAMENTE</b>' : 'Sposto nel cestino del cabinato'} ${paths.length} gioc${paths.length === 1 ? 'o' : 'hi'}:</p><ul class="hint">${names}</ul><p class="hint">Vengono tolti anche i file collegati (bin di un .cue, dischi di un .m3u) e immagini/video dello scraping, e la voce sparisce da EmulationStation.${perm ? '' : ' Il cestino è <code>/userdata/system/companion_trash</code>: puoi svuotarlo da qui quando sei sicuro.'}</p>`, perm ? 'Elimina per sempre' : 'Sposta nel cestino')) return;
  $('#g-job').hidden = false; $('#g-log').textContent = '';
  let ok = false;
  try {
    const r = await api('games/delete', {paths, permanent: perm, system: $('#g-sys').value}); const res = await pollJob(r.job, bindJobUI('g'));
    if (res.errors && res.errors.length) await dlg(`<p><b>${res.moved} file ${perm ? 'eliminati' : 'nel cestino'}</b>, ma ci sono stati problemi:</p><pre class="log">${esc(res.errors.join('\n'))}</pre>`, 'OK');
    else { toast(`${res.moved} file ${perm ? 'eliminati' : 'nel cestino'}`, 'ok'); ok = true; }
  } catch (e) { await dlg(`<p>Eliminazione fallita:</p><pre class="log">${esc(e.message)}</pre><pre class="log">${esc($('#g-log').textContent)}</pre>`, 'OK'); }
  if (ok) $('#g-job').hidden = true;
  gSel.clear(); loadGames();
}
async function gamesTrashInfo() { try { const t = await api('games/trash'); $('#g-trash').textContent = t.count ? `Cestino: ${t.count} file, ${fmtSize(t.size)}` : 'Cestino vuoto'; } catch (e) { $('#g-trash').textContent = ''; } }
async function gamesTrashEmpty() { if (!await dlg('<p>Svuoto il cestino del cabinato: i giochi eliminati finora spariscono <b>per sempre</b>.</p>', 'Svuota')) return; try { await api('games/trash', {action: 'empty'}); toast('Cestino svuotato', 'ok'); } catch (e) { toast(e.message, 'err'); } gamesTrashInfo(); }

// ---------- scheda gioco / scraper ----------
const KEYQ_ = () => (window.BC_KEY ? '&key=' + encodeURIComponent(window.BC_KEY) : '');
async function gameEdit(id) {
  const sys = $('#g-sys').value;
  let m;
  try { m = await api('games/meta?system=' + encodeURIComponent(sys) + '&id=' + encodeURIComponent(id)); } catch (e) { return toast(e.message, 'err'); }
  const f = (k, label, type) => `<label>${label}<input class="in" data-k="${k}" type="${type || 'text'}" value="${esc(m[k] || '')}"></label>`;
  const date = (m.releasedate && m.releasedate !== 'not-a-date-time') ? m.releasedate.slice(0, 4) + '-' + m.releasedate.slice(4, 6) + '-' + m.releasedate.slice(6, 8) : '';
  const accept = k => k === 'video' ? '.mp4,.webm,.mkv,.avi' : k === 'manual' ? '.pdf,.cbz' : '.png,.jpg,.jpeg,.gif,.webp,.bmp';
  const pics = [['thumbnail', 'Copertina'], ['image', 'Schermata'], ['titleshot', 'Titolo'], ['marquee', 'Logo'], ['fanart', 'Sfondo'], ['boxback', 'Retro'], ['video', 'Video'], ['manual', 'Manuale']].map(([k, l]) => {
    const pic = m[k] ? (k === 'video' ? `<div class="nopic">🎬 video presente</div>` : k === 'manual' ? `<div class="nopic">📄 manuale presente</div>` : `<img src="/media?system=${encodeURIComponent(sys)}&id=${encodeURIComponent(id)}&kind=${k}&t=${Date.now()}${KEYQ_()}">`) : `<div class="nopic">—</div>`;
    return `<figure class="pick" title="Clicca per caricare un file dal PC (${accept(k)})" onclick="this.querySelector('input').click()">${pic}<figcaption>${l} <span class="muted">· carica…</span></figcaption><input type="file" hidden accept="${accept(k)}" onchange="gameMediaUpload('${esc(sys)}','${esc(id)}','${k}',this.files[0])"></figure>`;
  }).join('');
  $('#gdlg-body').innerHTML = `<h2 style="margin-top:0">${esc(m.name || '')}</h2>
    <div class="hint mono">${esc((m.path || '').split('/').pop())}</div>
    <div class="gpics">${pics}</div>
    <div class="btns" style="margin:6px 0 10px"><button class="btn" onclick="gamesScrape(['${esc(id)}'])">🖼 Scarica immagini per questo gioco</button></div>
    <div class="gmeta" id="gmeta">
      <label class="full">Nome visualizzato <div class="btns"><input class="in grow" data-k="name" value="${esc(m.name || '')}">${m.suggested_name && m.suggested_name !== m.name ? `<button class="btn sm" title="Proposto: ${esc(m.suggested_name)}" onclick="$('#gmeta [data-k=name]').value='${esc(m.suggested_name).replace(/'/g, '&#39;')}'">Usa "${esc(m.suggested_name)}"</button>` : ''}</div></label>
      <label class="full">Descrizione<textarea class="in" data-k="desc">${esc(m.desc || '')}</textarea></label>
      ${f('genre', 'Genere')}${f('developer', 'Sviluppatore')}${f('publisher', 'Editore')}
      <label>Data di uscita<input class="in" data-k="releasedate" type="date" value="${date}"></label>
      ${f('players', 'Giocatori (es. 1-2)')}
      <label>Voto (0–1, es. 0.8)<input class="in" data-k="rating" value="${esc(m.rating && m.rating !== '0.000000' ? (+m.rating).toFixed(2) : '')}"></label>
      <label class="chk full"><input type="checkbox" data-k="favorite" ${m.favorite === 'true' ? 'checked' : ''}> Preferito</label>
      <label class="chk full"><input type="checkbox" data-k="hidden" ${m.hidden === 'true' ? 'checked' : ''}> Nascosto in EmulationStation</label>
    </div>
    <div class="btns right" style="margin-top:12px"><button class="btn" onclick="$('#gdlg').close()">Chiudi</button><button class="btn primary" onclick="gameSave('${esc(sys)}','${esc(id)}')">💾 Salva sul cabinato</button></div>`;
  $('#gdlg').showModal();
}
async function gameMediaUpload(sys, id, kind, file) {
  if (!file) return;
  const fd = new FormData(); fd.append('system', sys); fd.append('id', id); fd.append('kind', kind); fd.append('file', file, file.name);
  toast('Carico ' + file.name + '…');
  try {
    const r = await fetch('/api/games/media/set', {method: 'POST', body: fd}).then(r => r.json());
    if (r.error) throw new Error(r.error);
    toast('Grafica salvata sul cabinato', 'ok');
    // rimango sulla scheda: ricarico solo le anteprime
    const scroll = $('#gdlg-body').scrollTop; await gameEdit(id); $('#gdlg-body').scrollTop = scroll;
  } catch (e) { toast(e.message, 'err'); }
}
async function gameSave(sys, id) {
  const meta = {};
  $$('#gmeta [data-k]').forEach(el => { const k = el.dataset.k; meta[k] = el.type === 'checkbox' ? (el.checked ? 'true' : 'false') : el.value.trim(); });
  if (meta.rating) { const r = parseFloat(meta.rating.replace(',', '.')); meta.rating = isNaN(r) ? '' : Math.max(0, Math.min(1, r > 1 ? r / 10 : r)).toFixed(6); }
  if (!meta.releasedate) meta.releasedate = 'not-a-date-time';
  try { await api('games/meta', {system: sys, id, meta}); toast('Salvato', 'ok'); $('#gdlg').close(); loadGames(); } catch (e) { toast(e.message, 'err'); }
}
async function gamesScrape(ids) {
  const sys = $('#g-sys').value; if (!sys) return;
  if (!ids) { ids = [...gSel].map(p => (games.find(g => g.path === p) || {}).id).filter(Boolean); }
  const scope = ids.length ? `${ids.length} gioc${ids.length === 1 ? 'o selezionato' : 'hi selezionati'}` : `<b>tutti</b> i giochi di ${esc(sys)} (${games.length})`;
  let ssOk = false; try { ssOk = !!(await api('screenscraper')).ready; } catch (e) {}
  const ok = await dlg(`<p><b>Scraper</b> per ${scope}.</p>
    <label>Sorgente<select id="sc-src" class="in" onchange="document.getElementById('sc-ssonly').hidden=this.value!=='screenscraper'">
      <option value="libretro">libretro-thumbnails — gratis, senza account (copertina, schermata, titolo)</option>
      <option value="screenscraper" ${ssOk ? 'selected' : 'disabled'}>ScreenScraper — account${ssOk ? '' : ' (configura in Impostazioni)'}: tutto, descrizioni comprese</option>
    </select></label>
    <label class="chk"><input type="checkbox" id="sc-thumb" checked> Copertina</label>
    <label class="chk"><input type="checkbox" id="sc-image" checked> Schermata di gioco</label>
    <label class="chk"><input type="checkbox" id="sc-title"> Schermata del titolo</label>
    <div id="sc-ssonly" ${ssOk ? '' : 'hidden'}>
      <label class="chk"><input type="checkbox" id="sc-meta" checked> Descrizione, genere, anno, sviluppatore, giocatori, voto</label>
      <label class="chk"><input type="checkbox" id="sc-marquee" checked> Logo (marquee)</label>
      <label class="chk"><input type="checkbox" id="sc-video"> Video</label>
      <label class="chk"><input type="checkbox" id="sc-fanart"> Sfondo (fanart)</label>
      <label class="chk"><input type="checkbox" id="sc-manual"> Manuale</label>
    </div>
    <label class="chk"><input type="checkbox" id="sc-over"> Sovrascrivi quello già presente</label>`, 'Avvia');
  if (!ok) return;
  const source = $('#sc-src').value;
  const kinds = []; if ($('#sc-thumb').checked) kinds.push('thumbnail'); if ($('#sc-image').checked) kinds.push('image'); if ($('#sc-title').checked) kinds.push('titleshot');
  let meta = false;
  if (source === 'screenscraper') { meta = $('#sc-meta').checked; if ($('#sc-marquee').checked) kinds.push('marquee'); if ($('#sc-video').checked) kinds.push('video'); if ($('#sc-fanart').checked) kinds.push('fanart'); if ($('#sc-manual').checked) kinds.push('manual'); }
  const rename = false, overwrite = $('#sc-over').checked;
  if (!kinds.length && !meta) return;
  if ($('#gdlg').open) $('#gdlg').close();
  $('#g-job').hidden = false; $('#g-log').textContent = '';
  try {
    const r = await api('games/scrape', {system: sys, ids, kinds, rename, overwrite, source, meta}); const res = await pollJob(r.job, bindJobUI('g'));
    toast(`Scraper: ${res.result.found} giochi aggiornati`, 'ok');
  } catch (e) { toast(e.message, 'err'); }
  loadGames();
}

async function gamesRename() {
  const sys = $('#g-sys').value; if (!sys) return;
  const ids = [...gSel].map(p => (games.find(g => g.path === p) || {}).id).filter(Boolean);
  let r;
  try { r = await api('games/rename', {system: sys, ids, apply: false}); } catch (e) { return toast(e.message, 'err'); }
  const ch = r.rows.filter(x => x.Changed);
  if (!ch.length) return dlg(`<p>Nessun nome da cambiare${ids.length ? ' tra i selezionati' : ''}: sono già a posto.</p>`, 'OK');
  const ok = await dlg(`<p><b>Nomi da sistemare: ${ch.length}</b> su ${r.rows.length} ${ids.length ? 'selezionati' : 'giochi di ' + esc(sys)}</p>
    <div style="max-height:55vh;overflow:auto"><table class="tbl"><thead><tr><th>Adesso</th><th>Diventa</th><th>Fonte</th></tr></thead><tbody>${ch.map(x => `<tr><td class="mono">${esc(x.Old)}</td><td><b>${esc(x.New)}</b></td><td class="hint">${esc(x.Source)}</td></tr>`).join('')}</tbody></table></div>
    <p class="hint">Cambia solo il nome visualizzato nel gamelist, non i file.</p>`, `Rinomina ${ch.length}`);
  if (!ok) return;
  try { const a = await api('games/rename', {system: sys, ids, apply: true}); toast(`${a.changed} nomi aggiornati`, 'ok'); loadGames(); } catch (e) { toast(e.message, 'err'); }
}
async function gamesMultidisc() {
  const sys = $('#g-sys').value; if (!sys) return;
  if (!await dlg(`<p><b>Multi-disco</b> in <b>${esc(sys)}</b>: raggruppo i file "(Disc 1)", "(Disc 2)"… dello stesso gioco, creo l'.m3u se manca (dove Batocera lo supporta) e nascondo i singoli CD, così in lista resta una voce sola. Dove l'.m3u non è supportato resta visibile solo il primo disco.</p><p class="hint">Per tornare indietro: seleziona i CD e premi "Mostra".</p>`, 'Procedi')) return;
  $('#g-job').hidden = false; $('#g-log').textContent = '';
  try { const r = await api('games/multidisc', {system: sys}); const res = await pollJob(r.job, bindJobUI('g')); toast(`${res.result.groups} giochi multi-disco, ${res.result.hidden} dischi nascosti`, 'ok'); }
  catch (e) { toast(e.message, 'err'); }
  setTimeout(loadGames, 2500);
}
async function gamesHide(on) {
  const ids = [...gSel].map(p => (games.find(g => g.path === p) || {}).id).filter(Boolean);
  if (!ids.length) return toast('Seleziona prima i giochi', 'err');
  const sys = $('#g-sys').value; let n = 0;
  for (const id of ids) { try { await api('games/meta', {system: sys, id, meta: {hidden: on ? 'true' : 'false'}}); n++; } catch (e) { toast(e.message, 'err'); } }
  toast(`${n} giochi ${on ? 'nascosti' : 'visibili'} in EmulationStation`, 'ok'); gSel.clear(); loadGames();
}

// ---------- set MAME: verifica / conversione alla versione del cabinato ----------
async function gamesMame(mode) {
  const sel = [...gSel].map(p => (p.split('/').pop() || '').replace(/\.zip$/i, '').toLowerCase()).filter(Boolean);
  if (!mode) {
    const ok = await dlg(`<p><b>Set MAME alla versione del cabinato</b></p>
      <p class="hint">Sul cabinato leggo la lista del MAME installato (<code>mame -listxml</code>) e confronto ogni zip della cartella <code>mame</code>${sel.length ? ` (solo i ${sel.length} selezionati)` : ''}. Gli zip che hanno i file giusti ma con nomi/struttura di una versione vecchia vengono <b>ricostruiti</b> usando i file già presenti (riconosciuti per CRC, anche da altri zip, es. cloni o set uniti); i file originali finiscono in <code>companion_trash/mame_old</code>. Quelli a cui manca qualcosa vengono solo elencati, con i file mancanti.</p>
      <p class="hint">Prima faccio solo la <b>verifica</b> (nessuna modifica): poi decidi se convertire.</p>`, 'Verifica');
    if (!ok) return;
    mode = 'check';
  }
  $('#g-job').hidden = false; $('#g-log').textContent = '';
  let res;
  try { const r = await api('games/mame-rebuild', {mode, names: sel}); res = await pollJob(r.job, bindJobUI('g')); }
  catch (e) { return toast(e.message, 'err'); }
  const li = (arr, f) => arr.length ? '<ul class="hint" style="max-height:180px;overflow:auto">' + arr.slice(0, 200).map(f).join('') + (arr.length > 200 ? `<li>… e altri ${arr.length - 200}</li>` : '') + '</ul>' : '<div class="hint">nessuno</div>';
  const conv = res.converted || [], inc = res.incomplete || [], unk = res.unknown || [], dev = res.devices || [], errs = res.errors || [];
  const html = `<p><b>MAME ${esc(res.version || '')}</b> sul cabinato · già a posto: <b>${(res.ok || []).length}</b></p>
    <p><b>${mode === 'convert' ? 'Convertiti' : 'Da convertire'}: ${conv.length}</b></p>${li(conv, c => `<li><code>${esc(c.name)}</code> ${esc(c.desc || '')} (${c.files} file)</li>`)}
    <p><b>Incompleti (mancano file): ${inc.length}</b> — vanno presi da un set MAME ${esc((res.version || '').split(' ')[0])}</p>${li(inc, c => `<li><code>${esc(c.name)}</code> ${esc(c.desc || '')}: mancano ${c.missing.length}/${c.needed} — <span class="mono">${esc(c.missing.slice(0, 6).join(', '))}${c.missing.length > 6 ? '…' : ''}</span></li>`)}
    ${dev.length ? `<p><b>BIOS / parent / device collegati: ${dev.length}</b></p>${li(dev, d => `<li><code>${esc(d.name)}</code> ${esc(d.desc || '')}: ${esc(d.status)}</li>`)}` : ''}
    ${unk.length ? `<p><b>Sconosciuti a questo MAME: ${unk.length}</b> (nome non nel database: rinominati o di altra versione)</p>${li(unk, n => `<li><code>${esc(n)}</code></li>`)}` : ''}
    ${errs.length ? `<p><b>Errori: ${errs.length}</b></p>${li(errs, e => `<li>${esc(e)}</li>`)}` : ''}`;
  if (mode === 'check' && conv.length) {
    const go = await dlg(html + `<p>Converto ora i <b>${conv.length}</b> set ricostruibili?</p>`, `Converti ${conv.length} set`);
    if (go) return gamesMame('convert');
  } else {
    await dlg(html, 'OK');
  }
  if (mode === 'convert') loadGames();
}

// ---------- DAT ----------
async function loadDats() {
  try {
    const d = await api('dats');
    const row = x => `<tr><td><b>${esc(x.name)}</b><div class="hint">${esc(x.note || '')}</div></td>
      <td>${x.present ? `<span class="tag ok">presente</span> <span class="muted">${fmtSize(x.size)} · ${x.updated ? new Date(x.updated).toLocaleDateString('it-IT') : ''}${x.version ? ' · ' + esc(x.version) : ''}${x.machines ? ' · ' + x.machines + ' set' : ''}</span>` : '<span class="tag miss">assente</span>'}${x.error ? `<div class="hint" style="color:var(--err)">${esc(x.error)}</div>` : ''}</td>
      <td style="text-align:right">${x.generate ? `<button class="btn sm" onclick="datGenerate()">${x.present ? 'Rigenera' : 'Genera dal cabinato'}</button>` : `<button class="btn sm" onclick="datDownload(['${x.id}'])">${x.present ? 'Aggiorna' : 'Scarica'}</button>`}</td></tr>`;
    $('#dat-arcade tbody').innerHTML = d.filter(x => x.kind === 'arcade').map(row).join('');
    $('#dat-console tbody').innerHTML = d.filter(x => x.kind === 'console').map(row).join('');
  } catch (e) { toast(e.message, 'err'); }
}
async function datDownload(ids) {
  $('#dat-job').hidden = false; $('#dat-log').textContent = '';
  try { const r = await api('dats/download', {ids}); await pollJob(r.job, bindJobUI('dat')); toast('Database aggiornati', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
  $('#dat-job').hidden = true; loadDats();
}
function datDownloadRecommended() { datDownload(['fbneo', 'mame', 'mame2015', 'mame2003_plus', 'nes', 'snes', 'megadrive', 'mastersystem', 'gb', 'gbc', 'gba', 'pcengine', 'psx', 'n64']); }
async function datGenerate() {
  $('#dat-job').hidden = false; $('#dat-log').textContent = '';
  try { const r = await api('dats/generate'); await pollJob(r.job, bindJobUI('dat')); toast('DAT MAME generato', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
  $('#dat-job').hidden = true; loadDats();
}
async function datImport() {
  try {
    const d = await api('dats'); const r = await pickFiles('dat'); if (!r.paths || !r.paths.length) return;
    const opts = d.map(x => `<option value="${x.id}">${esc(x.name)}</option>`).join('');
    if (!await dlg(`<p>A quale database corrisponde <code>${esc(r.paths[0].split(/[\\/]/).pop())}</code>?</p><select id="dlg-in" class="in">${opts}</select>`, 'Importa')) return;
    const res = await api('dats/import', {id: $('#dlg-in').value, path: r.paths[0]});
    toast('Importato: ' + res.machines + ' set', 'ok'); loadDats();
  } catch (e) { toast(e.message, 'err'); }
}

// ---------- BIOS ----------
async function loadBios(refresh) {
  try {
    const info = await api('info' + (refresh ? '?refresh=1' : ''));
    const tb = $('#bios-table tbody'); tb.innerHTML = '';
    const list = info.bios_missing || [];
    $('#bios-summary').textContent = list.length ? list.length + ' file mancanti o errati' : 'Tutti i BIOS necessari sono presenti ✓';
    for (const b of list) {
      const tr = document.createElement('tr');
      tr.innerHTML = `<td>${esc(b.system)}</td><td class="mono">${esc(b.path)}</td><td><span class="tag ${b.status === 'MISSING' ? 'miss' : 'err'}">${esc(b.status)}</span></td><td class="muted">${esc(b.detail)}</td>`;
      tb.appendChild(tr);
    }
  } catch (e) { toast(e.message, 'err'); }
}
async function biosUpload(kind) {
  try {
    const r = await pickFiles(kind); if (!r.paths || !r.paths.length) return;
    if (kind === 'folder' && IS_LOCAL && !await dlg(`<p>Copio <b>tutto il contenuto</b> di <code>${esc(r.paths[0])}</code> (sottocartelle comprese) in <code>/userdata/bios</code> sul cabinato.</p><p class="hint">I file già presenti con la stessa dimensione vengono saltati, a meno che non spunti "sovrascrivi".</p>`, 'Copia tutto')) return;
    $('#bios-job').hidden = false; $('#bios-log').textContent = '';
    const j = await api('bios/upload', {paths: r.paths, overwrite: $('#bios-ow').checked}); const res = await pollJob(j.job, bindJobUI('bios'));
    toast(res && res.copied != null ? `BIOS: ${res.copied} copiati, ${res.skipped} già presenti` : 'BIOS copiati', 'ok'); $('#bios-job').hidden = true; loadBios(true);
  } catch (e) { toast(e.message, 'err'); $('#bios-job').hidden = true; }
}

// ---------- ASPETTO ----------
let lkFiles = {video: '', bootlogo: '', eslogo: ''};
async function lkLoad(withThemes) {
  try {
    const st = await api('look' + (withThemes ? '?themes=1' : ''));
    const files = st.splash_files && st.splash_files.length ? st.splash_files.join(', ') : 'nessun file personalizzato (video Batocera di serie)';
    $('#look-splash-state').innerHTML = `In <code>/userdata/splash</code>: ${esc(files)}<br>Durata: ${esc(st.splash_length || 'auto')} · Audio: ${st.splash_sound === '0' ? 'no' : 'sì'} · Scritta versione: ${st.splash_subtitle === '0' ? 'no' : 'sì'} · boot-logo: ${st.bootlogo_custom ? 'personalizzato' : 'originale'} · logo caricamento ES: ${st.eslogo_custom ? 'personalizzato' : 'originale'}${st.ffprobe ? '' : ' · (ffprobe assente: imposta la durata a mano)'}`;
    $('#lk-curtheme').textContent = st.current_theme || '—';
    const sel = $('#lk-theme'); sel.innerHTML = (st.installed || []).map(t => `<option ${t === st.current_theme ? 'selected' : ''}>${esc(t)}</option>`).join('');
    if (withThemes) { lkAvail = st.available || []; $('#lk-search').hidden = false; lkRenderAvail(); }
  } catch (e) { $('#look-splash-state').textContent = 'Cabinato non raggiungibile: ' + e.message; $('#lk-avail-hint').textContent = e.message; }
}
let lkAvail = [];
function lkRenderAvail() {
  const q = ($('#lk-search').value || '').toLowerCase(); const t = $('#lk-avail'); t.hidden = false;
  const list = lkAvail.filter(x => !q || x.name.toLowerCase().includes(q) || (x.author || '').toLowerCase().includes(q));
  $('#lk-avail-hint').textContent = `${list.length} temi (catalogo ufficiale updates.batocera.org)`;
  t.innerHTML = list.map(x => `<div class="theme">${x.screenshot ? `<img loading="lazy" src="${esc(x.screenshot)}" alt="">` : '<div style="aspect-ratio:16/9;background:#000"></div>'}<div class="b"><div class="n">${esc(x.name)} ${x.installed ? '<span class="tag ok">installato</span>' : ''}</div><div class="m">${esc(x.author || '')} · ${x.size_mb || '?'} MB · ${esc(x.updated || '')}</div><div class="a">${x.installed ? `<button class="btn sm ghost" onclick="lkTheme('remove','${esc(x.folder)}')">🗑 Rimuovi</button><button class="btn sm" onclick="lkUse('${esc(x.folder)}')">Usa</button>` : `<button class="btn sm primary" onclick="lkTheme('install','${esc(x.name)}')">⬇ Installa</button>`}<a class="btn sm ghost" href="#" onclick="openUrl('${esc(x.url)}');return false">GitHub</a></div></div></div>`).join('') || '<div class="muted">Nessun tema trovato</div>';
}
function lkUse(folder) { const sel = $('#lk-theme'); for (const o of sel.options) if (o.value === folder || o.value === folder + '-master' || o.value === folder + '-main') { sel.value = o.value; lkSetTheme(); return; } toast('Tema non trovato tra quelli installati: ricarica la pagina', 'err'); }
async function lkPick(kind) {
  try {
    const r = await pickFiles(kind === 'video' ? 'video' : 'image'); if (!r.paths || !r.paths.length) return;
    lkFiles[kind] = r.paths[0]; $('#lk-' + kind).value = r.paths[0];
  } catch (e) { toast(e.message, 'err'); }
}
async function lkRunJob(path, body) {
  $('#look-job').hidden = false; $('#look-log').textContent = '';
  try { const r = await api(path, body); await pollJob(r.job, bindJobUI('look')); toast('Fatto', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
  $('#look-job').hidden = true; lkLoad();
}
function lkApplySplash() {
  if (!lkFiles.video && !lkFiles.bootlogo && !lkFiles.eslogo) return toast('Scegli almeno un file', 'err');
  lkRunJob('look/splash', {video: lkFiles.video, boot_logo: lkFiles.bootlogo, es_logo: lkFiles.eslogo, length: parseInt($('#lk-length').value) || 0, sound: $('#lk-sound').checked, subtitle: $('#lk-subtitle').checked, replace: $('#lk-replace').checked});
}
async function lkResetSplash() {
  if (!await dlg('<p>Ripristino la schermata di avvio originale di Batocera (video di serie, boot-logo e logo di caricamento ES originali). I tuoi file restano nel backup.</p>', 'Ripristina')) return;
  lkRunJob('look/splash/reset', {splash: true, boot_logo: true, es_logo: true});
}
async function lkSetTheme() {
  const name = $('#lk-theme').value; if (!name) return;
  if (!await dlg(`<p>Imposto il tema <b>${esc(name)}</b> e riavvio EmulationStation (se c'è un gioco in corso viene chiuso).</p>`, 'Applica')) return;
  try { await api('look/theme', {action: 'set', name}); toast('Tema impostato, ES in riavvio', 'ok'); setTimeout(lkLoad, 4000); } catch (e) { toast(e.message, 'err'); }
}
function lkLoadAvailable() { $('#lk-avail-hint').textContent = 'chiedo al cabinato…'; lkLoad(true); }
async function lkTheme(action, name) {
  if (action === 'remove') {
    if (!await dlg(`<p>Rimuovere il tema <b>${esc(name)}</b> dal cabinato?</p>`, 'Rimuovi')) return;
    try { await api('look/theme', {action, name}); toast('Tema rimosso', 'ok'); } catch (e) { toast(e.message, 'err'); }
    lkLoad(true); return;
  }
  await lkRunJob('look/theme', {action, name}); lkLoad(true);
}
async function lkInstallURL() {
  const url = $('#lk-ghurl').value.trim(); if (!url) return toast('Incolla l\'indirizzo GitHub del tema', 'err');
  lkRunJob('look/theme', {action: 'install', url});
}
async function lkUploadTheme() {
  try { const r = await pickFiles('zip'); if (!r.paths || !r.paths.length) return; await lkRunJob('look/theme', {action: 'upload', path: r.paths[0]}); } catch (e) { toast(e.message, 'err'); }
}
async function openUrl(u) { try { await api('open', {what: 'url', url: u}); } catch (e) {} }

// ---------- ADD-ON ----------
let adData = null, adSrc = 'all', adCat = 'all';
async function adLoad(refresh) {
  $('#ad-hint').textContent = 'leggo lo store dal cabinato e l\'elenco BUA…';
  try {
    adData = await api('addons' + (refresh ? '?refresh=1' : ''));
    const n = (adData.official || []).length + (adData.bua || []).length;
    $('#ad-hint').innerHTML = `${(adData.bua || []).length} add-on non ufficiali · ${(adData.official || []).length} pacchetti store${adData.bua_base ? ' · base BUA installata' : ''}${adData.bua_error ? ' · <span style="color:var(--err)">BUA: ' + esc(adData.bua_error) + '</span>' : ''}<br><span class="muted">${esc(adData.warning)}</span>`;
    const cats = [...new Set([...(adData.bua || []), ...(adData.official || [])].map(a => a.category))].sort();
    $('#ad-cat').innerHTML = '<option value="all">Tutte</option>' + cats.map(c => `<option value="${esc(c)}">${esc(c)}</option>`).join('');
    $('#ad-cat').value = cats.includes(adCat) ? adCat : 'all';
    adRender();
  } catch (e) { $('#ad-hint').textContent = 'Errore: ' + e.message; }
}
function adRender() {
  if (!adData) return;
  adSrc = $('#ad-src').value; adCat = $('#ad-cat').value;
  const q = $('#ad-search').value.toLowerCase(), rec = $('#ad-rec').checked, inst = $('#ad-inst').checked;
  let list = [...(adData.bua || []), ...(adData.official || [])];
  list = list.filter(a => (adSrc === 'all' || a.source === adSrc) && (adCat === 'all' || a.category === adCat) && (!rec || a.recommended) && (!inst || a.installed) && (!q || a.name.toLowerCase().includes(q) || (a.description || '').toLowerCase().includes(q)));
  list.sort((a, b) => (b.recommended - a.recommended) || (b.installed - a.installed) || a.name.localeCompare(b.name));
  $('#ad-list').innerHTML = list.map(a => `<div class="theme"><div class="b"><div class="n">${a.recommended ? '⭐ ' : ''}${esc(a.name)} ${a.installed ? '<span class="tag ok">installato</span>' : ''}<span class="tag">${a.source === 'bua' ? 'BUA' : 'store'}</span></div><div class="m">${esc(a.category)}${a.version ? ' · ' + esc(a.version) : ''}</div><div class="hint" style="font-size:12px">${esc(a.description || '')}</div><div class="a">${a.installed ? `<button class="btn sm ghost" onclick="adAct('remove','${a.source}','${esc(a.name)}')">🗑 Rimuovi</button>` : `<button class="btn sm primary" onclick="adAct('install','${a.source}','${esc(a.name)}')">⬇ Installa</button>`}${a.url ? `<a class="btn sm ghost" href="#" onclick="openUrl('${esc(a.url)}');return false">script</a>` : ''}</div></div></div>`).join('') || '<div class="muted">Nessun add-on con questi filtri (togli ⭐ consigliati per vederli tutti)</div>';
}
async function adAct(action, source, name) {
  if (action === 'remove' && !await dlg(`<p>Rimuovere <b>${esc(name)}</b> dal cabinato?</p>`, 'Rimuovi')) return;
  if (action === 'install' && source === 'bua' && !await dlg(`<p>Installo <b>${esc(name)}</b> dagli add-on non ufficiali.</p><p class="hint">Lo script viene scaricato da GitHub ed eseguito sul cabinato come root; la prima volta installa anche la base BUA (gestore symlink in /userdata/system/services). Serve internet sul cabinato.</p>`, 'Installa')) return;
  $('#ad-job').hidden = false; $('#ad-log').textContent = ''; $('#ad-job-title').textContent = (action === 'install' ? 'Installo ' : 'Rimuovo ') + name;
  try { const r = await api('addons/action', {action, source, name}); await pollJob(r.job, bindJobUI('ad')); toast('Fatto: ' + name, 'ok'); }
  catch (e) { toast(e.message, 'err'); }
  adLoad(false);
}

// ---------- SETUP APP CON UN CLIC ----------
async function asLoad() {
  try {
    const r = await api('appsetup');
    const st = r.steam, ch = r.chiaki;
    const row = (ok, txt) => `<div class="hint" style="font-size:12px">${ok ? '✅' : '⬜'} ${txt}</div>`;
    const psnUrl = 'https://auth.api.sonyentertainmentnetwork.com/2.0/oauth/authorize?service_entity=urn:service-entity:psn&response_type=code&client_id=ba495a24-818c-472b-b12d-ff231c1b5745&redirect_uri=https://remoteplay.dl.playstation.net/remoteplay/redirect&scope=psn:clientapp%20referenceDataService:countryConfig.read%20pushNotification:webSocket.desktop.connect%20sessionManager:remotePlaySession.system.update&request_locale=it_IT&ui=pr&service_logo=ps&layout_type=popup&smcid=remoteplay&prompt=always&PlatformPrivacyWs1=minimal&';
    $('#as-list').innerHTML = `
      <div class="theme"><div class="b"><div class="n">Steam ${st.configured ? '<span class="tag ok">configurato</span>' : ''}</div>
        ${row(st.installed, 'Steam installato (Flathub)')}${row(st.rootcheck === 'removed', "blocco 'non come root' tolto")}${row(st.launcher && st.entry, 'avvio dalla sezione Steam (portal + log su file)')}${row(st.launcher, 'hotkey + start chiude Steam')}${row(st.padmap, 'plancia mappata (leva = levetta analogica)')}${row(st.logged_in, 'account Steam collegato')}
        <div class="a"><button class="btn sm primary" onclick="asRun('steam')">${st.configured ? '🔁 Riconfigura' : '⚙ Configura Steam'}</button></div></div></div>
      <div class="theme"><div class="b"><div class="n">Chiaki (PS5 Remote Play) ${ch.configured ? '<span class="tag ok">configurato</span>' : ''}</div>
        ${row(ch.installed, 'Chiaki installato (BUA)')}${row(ch.persistent, 'configurazione persistente ai riavvii')}${row(ch.registered, 'PS5 registrata' + (ch.hosts && ch.hosts.length ? ': ' + esc(ch.hosts.join(', ')) : ''))}
        <div class="a"><button class="btn sm primary" onclick="asRun('chiaki')">${ch.configured ? '🔁 Riconfigura' : '⚙ Configura Chiaki'}</button><a class="btn sm ghost" href="#" onclick="openUrl('${psnUrl}');return false" title="Apre il login PSN nel browser del PC: dopo il login copia l'indirizzo della pagina e incollalo in Chiaki → PSN Login con Ctrl+V nella vista live">🔑 Login PSN (per l'Account ID)</a></div></div></div>`;
  } catch (e) { $('#as-list').innerHTML = '<div class="muted">' + esc(e.message) + '</div>'; }
}
async function asRun(app) {
  $('#as-job').hidden = false; $('#as-log').textContent = ''; $('#as-job-title').textContent = app === 'steam' ? 'Configuro Steam' : 'Configuro Chiaki';
  try { const r = await api('appsetup', {app}); await pollJob(r.job, bindJobUI('as')); toast('Fatto', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
  asLoad();
}

// ---------- FLATHUB ----------
let fhLoaded = false;
function fhRender(apps) {
  $('#fh-list').innerHTML = (apps || []).map(a => `<div class="theme"><div class="b"><div class="n">${a.icon ? `<img src="${esc(a.icon)}" style="width:28px;height:28px;vertical-align:middle;border-radius:6px;margin-right:6px">` : ''}${a.recommended ? '⭐ ' : ''}${esc(a.name)} ${a.installed ? '<span class="tag ok">installato' + (a.version ? ' ' + esc(a.version) : '') + '</span>' : ''}</div><div class="m">${esc(a.id)}${a.developer ? ' · ' + esc(a.developer) : ''}${a.installs ? ' · ' + a.installs.toLocaleString('it') + ' installazioni/mese' : ''}</div><div class="hint" style="font-size:12px">${esc(a.summary || '')}${a.recommended ? '<br><b>' + esc(a.recommended) + '</b>' : ''}</div><div class="a">${a.installed ? `<button class="btn sm ghost" onclick="fhAct('remove','${esc(a.id)}','${esc(a.name)}')">🗑 Rimuovi</button>` : `<button class="btn sm primary" onclick="fhAct('install','${esc(a.id)}','${esc(a.name)}')">⬇ Installa</button>`}<a class="btn sm ghost" href="#" onclick="openUrl('https://flathub.org/apps/${esc(a.id)}');return false">scheda</a></div></div></div>`).join('') || '<div class="muted">Nessun risultato</div>';
}
async function fhLoad() {
  $('#fh-hint').textContent = 'leggo i flatpak installati sul cabinato e i consigliati…';
  try { const r = await api('flathub'); fhRender(r.apps); $('#fh-hint').textContent = (r.apps || []).length + ' app' + (r.cabinet_error ? ' · cabinato: ' + r.cabinet_error : '') + (r.error ? ' · ' + r.error : ''); fhLoaded = true; }
  catch (e) { $('#fh-hint').textContent = 'Errore: ' + e.message; }
}
async function fhSearch() {
  const q = $('#fh-search').value.trim(); if (!q) return fhLoad();
  $('#fh-hint').textContent = 'cerco "' + q + '" su Flathub…';
  try { const r = await api('flathub?q=' + encodeURIComponent(q)); fhRender(r.apps); $('#fh-hint').textContent = (r.apps || []).length + ' risultati per "' + q + '"' + (r.error ? ' · ' + r.error : ''); }
  catch (e) { $('#fh-hint').textContent = 'Errore: ' + e.message; }
}
async function fhAct(action, id, name) {
  if (action === 'remove' && !await dlg(`<p>Rimuovere <b>${esc(name)}</b> dal cabinato?</p>`, 'Rimuovi')) return;
  if (action === 'install' && !await dlg(`<p>Installo <b>${esc(name)}</b> da Flathub sul cabinato.</p><p class="hint">Il download può essere di centinaia di MB (Steam circa 1 GB più l'aggiornamento al primo avvio). Serve internet sul cabinato.</p>`, 'Installa')) return;
  $('#fh-job').hidden = false; $('#fh-log').textContent = ''; $('#fh-job-title').textContent = (action === 'install' ? 'Installo ' : 'Rimuovo ') + name;
  try { const r = await api('flathub/action', {Action: action, ID: id, Name: name}); await pollJob(r.job, bindJobUI('fh')); toast('Fatto: ' + name, 'ok'); }
  catch (e) { toast(e.message, 'err'); }
  $('#fh-search').value ? fhSearch() : fhLoad();
}

// ---------- SCREENSCRAPER ----------
let ssReady = false;
async function ssLoad() {
  try {
    const r = await api('screenscraper');
    $('#ss-user').value = r.user || ''; $('#ss-devid').value = r.devid || ''; $('#ss-region').value = r.region || ''; $('#ss-lang').value = r.lang || '';
    $('#ss-pass').placeholder = r.has_pass ? '(invariata)' : ''; $('#ss-devpass').placeholder = r.has_devpass ? '(invariata)' : '';
    ssReady = !!r.ready;
    $('#ss-status').innerHTML = (r.user ? '✅ account salvato nel Companion' : '⬜ nessun account salvato') + (r.ready ? ' · ✅ credenziali sviluppatore: scraper dal Companion disponibile' : ' · ⬜ senza devid lo scraper dal Companion non è disponibile') + (r.es_source === 'ScreenScraper' && r.es_user ? ' · ✅ Batocera usa ScreenScraper con l\'utente ' + esc(r.es_user) : ' · ⬜ Batocera non usa ancora questo account');
  } catch (e) { $('#ss-status').textContent = 'Errore: ' + e.message; }
}
async function ssSave() {
  try { await api('screenscraper', {action: 'save', User: $('#ss-user').value, Pass: $('#ss-pass').value, DevID: $('#ss-devid').value, DevPass: $('#ss-devpass').value, Region: $('#ss-region').value, Lang: $('#ss-lang').value}); $('#ss-pass').value = ''; $('#ss-devpass').value = ''; toast('Salvato', 'ok'); ssLoad(); }
  catch (e) { toast(e.message, 'err'); }
}
async function ssTest() {
  try { const r = await api('screenscraper', {action: 'test'}); const i = r.info || {}; toast(`Account OK: ${i.id || ''} · livello ${i.niveau || '?'} · ${i.requeststoday || 0}/${i.maxrequestsperday || '?'} richieste oggi`, 'ok'); }
  catch (e) { toast(e.message, 'err'); }
}
async function ssToES() {
  if (!await dlg('<p>Scrivo utente e password ScreenScraper nelle impostazioni di EmulationStation e imposto ScreenScraper come sorgente dello scraper di Batocera.</p><p class="hint">EmulationStation viene riavviato (non farlo con un gioco in corso). Poi dal menu del cabinato: Scraper → "Scrapea ora".</p>', 'Procedi')) return;
  try { await api('screenscraper', {action: 'es'}); toast('Fatto: EmulationStation riavviato con il tuo account ScreenScraper', 'ok'); ssLoad(); }
  catch (e) { toast(e.message, 'err'); }
}

// ---------- SINDEN ----------
function sgPreview() {
  const c = $('#sg-preview'); if (!c) return; const ctx = c.getContext('2d'); const W = c.width, H = c.height;
  ctx.fillStyle = '#2a3550'; ctx.fillRect(0, 0, W, H);
  ctx.fillStyle = '#4a5a80'; for (let i = 0; i < 6; i++) ctx.fillRect(20 + i * 38, 60 + (i % 2) * 30, 24, 70);
  const size = $('#sg-borderssize').value, col = {white: '#fff', red: '#e33', green: '#3d3', blue: '#36f'}[$('#sg-borderscolor').value] || '#fff';
  const inner = size === 'thin' ? 1 : 2, outer = size === 'big' ? 1 : 0;
  const o = Math.ceil(W * outer / 100), t = Math.max(Math.ceil(W * inner / 100), 1);
  if (o) { ctx.strokeStyle = '#000'; ctx.lineWidth = o * 2; ctx.strokeRect(0, 0, W, H); }
  ctx.strokeStyle = col; ctx.lineWidth = t * 2; ctx.strokeRect(o, o, W - 2 * o, H - 2 * o);
  if ($('#sg-bordersmode').value === 'hidden') { ctx.fillStyle = 'rgba(0,0,0,.6)'; ctx.fillRect(0, 0, W, H); ctx.fillStyle = '#fff'; ctx.font = '13px sans-serif'; ctx.fillText('bordi nascosti: la pistola non può mirare', 12, H / 2); }
}
async function sgLoad() {
  $('#sg-status').textContent = 'controllo le pistole sul cabinato…';
  try {
    const r = await api('sinden');
    const c = r.conf || {};
    $('#sg-borderssize').value = c.borderssize || 'medium'; $('#sg-bordersmode').value = c.bordersmode || 'auto'; $('#sg-borderscolor').value = c.borderscolor || 'white'; $('#sg-recoil').value = c.recoil || 'none';
    let st = '';
    if (r.guns && r.guns.length) st = '🔫 ' + r.guns.map(g => esc(g.name) + ' (' + g.dev + (g.sinden ? ', Sinden' : '') + ')').join(', ') + ' — pronte';
    else if (r.raw_sinden) st = '⚠️ Sinden collegata ma non ancora configurata da Batocera (servizio in avvio? scollega e ricollega la USB)';
    else if (r.usb) st = '⚠️ Sinden vista sulla USB (' + esc(r.usb) + ') ma nessun dispositivo pistola: ricollegala';
    else st = '— nessuna pistola collegata adesso. Quando la colleghi premi "Rileva pistole".';
    if (!r.support) st += ' · ⚠️ supporto Sinden non trovato in questa Batocera';
    $('#sg-status').innerHTML = st;
    sgPreview();
  } catch (e) { $('#sg-status').textContent = 'Errore: ' + e.message; }
}
async function sgSave() {
  try {
    const r = await api('sinden', {conf: {borderssize: $('#sg-borderssize').value, bordersmode: $('#sg-bordersmode').value, borderscolor: $('#sg-borderscolor').value, recoil: $('#sg-recoil').value}});
    toast('Impostazioni pistole salvate' + (r.note ? ' · ' + r.note : ''), 'ok');
  } catch (e) { toast(e.message, 'err'); }
}
['sg-borderssize', 'sg-bordersmode', 'sg-borderscolor'].forEach(id => document.addEventListener('change', e => { if (e.target.id === id) sgPreview(); }));

// ---------- BACKUP ----------
let bkJob = null, bkSizes = {};
function bkFmtDate(d) { try { return new Date(d).toLocaleString('it-IT'); } catch (e) { return d; } }
async function bkLoad() {
  try {
    const r = await api('backup');
    $('#bk-dir').value = r.dir || '';
    $('#bk-cats').innerHTML = r.cats.map(c => `<label class="chk" title="${esc(c.desc)}"><input type="checkbox" data-cat="${c.id}" ${c.default ? 'checked' : ''}><b>${esc(c.label)}</b><span class="hint">${esc(c.desc)}</span><span class="tag" id="bk-size-${c.id}"></span></label>`).join('');
    if (r.last) {
      const tot = Object.values(r.last.bytes || {}).reduce((a, b) => a + b, 0);
      const fmt = n => n >= 1 << 30 ? (n / (1 << 30)).toFixed(1) + ' GB' : Math.round(n / (1 << 20)) + ' MB';
      $('#bk-last').innerHTML = `Ultimo backup in questa cartella: <b>${bkFmtDate(r.last.date)}</b> · ${(r.last.categories || []).join(', ')} · ${fmt(tot)}`;
      (r.last.categories || []).forEach(id => { const el = $(`#bk-size-${id}`); if (el && r.last.bytes && r.last.bytes[id] != null) el.textContent = fmt(r.last.bytes[id]); });
    } else $('#bk-last').textContent = r.dir ? 'Nessun backup ancora in questa cartella.' : '';
  } catch (e) { toast(e.message, 'err'); }
}
function bkTarget() {
  const usb = $('#bk-target').value === 'usb';
  $('#bk-dir-wrap').hidden = usb; $('#bk-pick-wrap').hidden = usb; $('#bk-usb-wrap').hidden = !usb; $('#bk-usb-btn').hidden = !usb; $('#bk-usb-hint').hidden = !usb;
  $('#bk-mirror').parentElement.querySelector('.hint').textContent = usb ? '(con rsync --delete)' : '(altrimenti il backup conserva anche i giochi cancellati)';
  if (usb) bkUsbLoad();
}
async function bkUsbLoad() {
  const sel = $('#bk-usb'); sel.innerHTML = '<option>cerco dischi…</option>';
  try {
    const r = await api('backup/usb');
    sel.innerHTML = (r.drives || []).map(d => `<option value="${esc(d.mount)}">${esc(d.label)} · ${esc(d.size)} (liberi ${esc(d.free)}) · ${esc(d.fs)}</option>`).join('') || '<option value="">nessun disco USB montato: collegalo al cabinato e premi Aggiorna</option>';
    const um = r.unmounted || [];
    $('#bk-unmounted').innerHTML = um.length ? '<div class="hint" style="margin-top:6px">Dischi collegati ma <b>non montati</b> da Batocera:</div>' + um.map(u => `<div class="finding warn" style="margin-top:6px"><i></i><div class="body"><span class="t">${esc(u.label || u.dev)}</span> <span class="muted">${esc(u.dev)} · ${esc(u.size)} · ${esc(u.fs) || 'filesystem non riconosciuto'}${u.mounted_at ? ' · montato in ' + esc(u.mounted_at) : ''}</span></div><div class="act" style="padding:8px 10px 0 0"><button class="btn sm" onclick="bkMount('${esc(u.dev)}')">🔌 Monta</button></div></div>`).join('') + '' : '';
    $('#bk-unmounted').innerHTML += `<details style="margin-top:6px"><summary class="muted" style="cursor:pointer;font-size:12px">Cosa vede il cabinato (dischi, partizioni, USB, kernel)</summary><pre class="log" style="max-height:220px">${esc(r.raw || '')}
${esc(r.dmesg || '')}</pre></details>`;
  } catch (e) { sel.innerHTML = '<option value="">errore: ' + esc(e.message) + '</option>'; }
}
async function bkMount(dev) {
  try {
    const r = await api('backup/usb', {action: 'mount', mount: dev});
    toast('Montato in ' + r.mount + (r.readonly ? ' (SOLO LETTURA: il disco è "sporco", su Windows fai chkdsk /f ed espellilo in sicurezza)' : ''), r.readonly ? 'err' : 'ok');
  } catch (e) { await dlg('<p><b>Montaggio non riuscito</b></p><pre class="log">' + esc(e.message) + '</pre><p class="hint">Cause tipiche per NTFS: disco non espulso in sicurezza da Windows o "Avvio rapido" di Windows attivo (lascia il volume in stato ibernato). Su Windows: tasto destro sul disco → Proprietà → Strumenti → Controlla, oppure <code>chkdsk X: /f</code>, poi "Rimozione sicura". In alternativa formattalo in exFAT.</p>', 'OK'); }
  bkUsbLoad();
}
function bkCats() { return $$('#bk-cats input[type=checkbox]').filter(i => i.checked).map(i => i.dataset.cat); }
async function bkPick() {
  try { const r = await pickFiles('folder'); if (r.paths && r.paths[0]) { $('#bk-dir').value = r.paths[0]; await api('backup', {action: 'scan', dir: r.paths[0], cats: ['bios']}).catch(() => {}); bkLoad(); } }
  catch (e) { toast(e.message, 'err'); }
}
async function bkScan() {
  const cats = bkCats(); if (!cats.length) return toast('Scegli almeno una categoria', 'err');
  const usb = $('#bk-target').value === 'usb';
  if (usb && !$('#bk-usb').value) return toast('Scegli il disco USB', 'err');
  $('#bk-job').hidden = false; $('#bk-log').textContent = ''; $('#bk-job-title').textContent = 'Calcolo dimensioni';
  try {
    const r = usb ? await api('backup/usb', {action: 'scan', mount: $('#bk-usb').value, cats}) : await api('backup', {action: 'scan', dir: $('#bk-dir').value.trim(), cats});
    const res = await pollJob(r.job, bindJobUI('bk'));
    const sizes = (res && res.result) || res || {};
    Object.entries(sizes).forEach(([id, v]) => { const el = $(`#bk-size-${id}`); if (el && v.human) el.textContent = v.human + (v.todo_files ? ' · da copiare ' + v.todo_human : ' · aggiornato'); });
  } catch (e) { toast(e.message, 'err'); }
}
async function bkRun(action) {
  const cats = bkCats(); if (!cats.length) return toast('Scegli almeno una categoria', 'err');
  if ($('#bk-target').value === 'usb') return bkRunUsb(action, cats);
  const dir = $('#bk-dir').value.trim(); if (!dir) return toast('Scegli la cartella sul PC', 'err');
  if (action === 'restore' && !await dlg(`<p>Riporto sul cabinato i file del backup in <b>${esc(dir)}</b> per: <b>${cats.join(', ')}</b>.</p><p class="hint">Sovrascrive sul cabinato i file diversi dal backup. I file presenti solo sul cabinato restano. Se ripristini la configurazione, dopo riavvia il cabinato senza toccare i menu di EmulationStation.</p>`, 'Ripristina')) return;
  $('#bk-job').hidden = false; $('#bk-log').textContent = ''; $('#bk-job-title').textContent = action === 'backup' ? 'Backup in corso' : 'Ripristino in corso'; $('#bk-cancel').hidden = false;
  try {
    const r = await api('backup', {action, dir, cats, mirror: $('#bk-mirror').checked}); bkJob = r.job;
    await pollJob(r.job, bindJobUI('bk'));
    toast(action === 'backup' ? 'Backup completato' : 'Ripristino completato', 'ok');
  } catch (e) { toast(e.message, 'err'); }
  $('#bk-cancel').hidden = true; bkJob = null; bkLoad();
}
async function bkRunUsb(action, cats) {
  const mount = $('#bk-usb').value; if (!mount) return toast('Scegli il disco USB', 'err');
  if (action === 'restore' && !await dlg(`<p>Riporto sul cabinato i file del backup in <b>${esc(mount)}/batocera-backup</b> per: <b>${cats.join(', ')}</b>.</p><p class="hint">Sovrascrive sul cabinato i file diversi dal backup. Se ripristini la configurazione, dopo riavvia il cabinato senza toccare i menu di EmulationStation.</p>`, 'Ripristina')) return;
  $('#bk-job').hidden = false; $('#bk-log').textContent = ''; $('#bk-job-title').textContent = (action === 'backup' ? 'Backup su ' : 'Ripristino da ') + mount; $('#bk-cancel').hidden = false;
  try {
    const r = await api('backup/usb', {action, mount, cats, mirror: $('#bk-mirror').checked}); bkJob = r.job;
    await pollJob(r.job, bindJobUI('bk'));
    toast(action === 'backup' ? 'Backup completato' : 'Ripristino completato', 'ok');
  } catch (e) { toast(e.message, 'err'); }
  $('#bk-cancel').hidden = true; bkJob = null;
}
async function bkCancel() { if (bkJob) { try { await api('job/cancel', {id: bkJob}); } catch (e) {} } }

// ---------- SHELL ----------
let shHist = [], shHi = 0, shBusy = false;
function shq(c) { $('#sh-cmd').value = c; shGrow(); runShell(); }
function shGrow() { const t = $('#sh-cmd'); t.style.height = 'auto'; t.style.height = t.scrollHeight + 'px'; }
function shOut(text, cls) { const out = $('#sh-out'); const sp = document.createElement('span'); if (cls) sp.className = cls; sp.textContent = text; out.appendChild(sp); const term = $('#sh-term'); term.scrollTop = term.scrollHeight; }
function shKey(e) {
  const t = e.target;
  if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); runShell(); return; }
  const single = !t.value.includes('\n');
  if (e.key === 'ArrowUp' && single && shHist.length) { e.preventDefault(); shHi = Math.max(0, shHi - 1); t.value = shHist[shHi]; shGrow(); }
  else if (e.key === 'ArrowDown' && single && shHist.length) { e.preventDefault(); shHi = Math.min(shHist.length, shHi + 1); t.value = shHist[shHi] || ''; shGrow(); }
  else if (e.key === 'l' && e.ctrlKey) { e.preventDefault(); shClear(); }
}
function shClear() { $('#sh-out').textContent = ''; $('#sh-cmd').focus(); }
async function shCopy() { try { await navigator.clipboard.writeText($('#sh-out').textContent); toast('Output copiato', 'ok'); } catch (e) { toast('Copia non riuscita: ' + e.message, 'err'); } }
async function runShell() {
  const t = $('#sh-cmd'); const c = t.value.replace(/\s+$/, ''); if (!c.trim() || shBusy) return;
  if (shHist[shHist.length - 1] !== c) shHist.push(c); shHi = shHist.length;
  t.value = ''; shGrow();
  c.split('\n').forEach((l, i) => { shOut(i ? '> ' : 'root@batocera:~# ', 't-prompt'); shOut(l + '\n', 't-cmd'); });
  shBusy = true; $('#sh-term').classList.add('busy');
  try {
    const r = await api('shell', {cmd: c});
    if (r.stdout) shOut(r.stdout.endsWith('\n') ? r.stdout : r.stdout + '\n');
    if (r.stderr) shOut(r.stderr.endsWith('\n') ? r.stderr : r.stderr + '\n', 't-err');
    if (r.exit) shOut('(codice di uscita ' + r.exit + ')\n', 't-dim');
  } catch (e) { shOut('ERRORE: ' + e.message + '\n', 't-err'); }
  shOut('\n'); shBusy = false; $('#sh-term').classList.remove('busy'); t.focus();
}

// ---------- IMPOSTAZIONI ----------
async function loadConfig() {
  try {
    const r = await api('config'); cfg = r.config;
    $('#c-host').value = cfg.host; $('#c-remote').value = cfg.remote_host || ''; $('#c-user').value = cfg.ssh_user; $('#c-pass').value = cfg.ssh_pass; $('#c-sshport').value = cfg.ssh_port; $('#c-esport').value = cfg.es_port;
    $('#c-roms').value = cfg.roms_path; $('#c-bios').value = cfg.bios_path; $('#c-shots').value = cfg.screenshot_dir;
    $('#c-pref').value = (cfg.arcade_preference || []).join(', '); $('#c-chd').checked = !!cfg.convert_to_chd;
    $('#c-lan').checked = !!cfg.lan_enabled; $('#c-pin').value = cfg.lan_pin || ''; $('#c-lanport').value = cfg.lan_port || 8765;
    api('lan').then(l => { if (l.active && l.urls && l.urls.length) $('#c-lan-info').innerHTML = `<b>Attivo.</b> Sull'iPhone (stessa rete Wi-Fi) apri Safari su: ${l.urls.map(u => '<code>' + esc(u) + '</code>').join(' oppure ')} → PIN → Condividi → <b>Aggiungi alla schermata Home</b>. Se non risponde, al primo avvio Windows ha chiesto il permesso al firewall: consentilo per le reti private.`; }).catch(() => {});
    $('#c-msg').textContent = 'Dati app: ' + r.data_dir;
  } catch (e) { toast(e.message, 'err'); }
}
async function saveConfig() {
  const c = Object.assign({}, cfg, {
    host: $('#c-host').value.trim(), remote_host: $('#c-remote').value.trim(), ssh_user: $('#c-user').value.trim(), ssh_pass: $('#c-pass').value, ssh_port: +$('#c-sshport').value || 22, es_port: +$('#c-esport').value || 1234,
    roms_path: $('#c-roms').value.trim(), bios_path: $('#c-bios').value.trim(), screenshot_dir: $('#c-shots').value.trim(),
    arcade_preference: $('#c-pref').value.split(/[,\s]+/).filter(Boolean), convert_to_chd: $('#c-chd').checked,
    lan_enabled: $('#c-lan').checked, lan_pin: $('#c-pin').value.trim(), lan_port: +$('#c-lanport').value || 8765,
  });
  try { const r = await api('config', c); if (r.lan_error) toast('Accesso di rete: ' + r.lan_error, 'err'); else toast('Impostazioni salvate', 'ok'); refreshStatus(); loadConfig(); } catch (e) { toast(e.message, 'err'); }
}

// ---------- server sul cabinato ----------
async function cabLoad() {
  const el = $('#cab-state'); if (!el) return;
  try {
    const c = await api('cabinet');
    if (!c.payload) { el.innerHTML = '<span style="color:var(--err)">Questa build non contiene il binario per il cabinato.</span>'; $('#cab-install').disabled = true; return; }
    el.innerHTML = c.installed
      ? `<b>Installato</b> (versione ${esc(c.version || '?')}${c.payload_version && c.version !== c.payload_version ? ' → disponibile ' + esc(c.payload_version) : ''}) · servizio ${c.running ? '<span class="tag ok">in esecuzione</span>' : '<span class="tag err">fermo</span>'} · avvio automatico ${c.enabled ? 'sì' : 'no'}<br>Sull'iPhone apri: <code>${esc(c.url)}</code> → PIN (solo la prima volta) → Condividi → <b>Aggiungi alla schermata Home</b>.` + (c.ha_camera ? `<div style="margin-top:8px"><b>Per Home Assistant</b> (link con chiave, senza PIN — non condividerli):<br>Telecamera MJPEG: <code id="ha-cam-url">${esc(c.ha_camera)}</code> <button class="btn sm ghost" onclick="copyText($('#ha-cam-url').textContent)">copia</button><br>Scheda "Pagina web" (schermo + pad): <code id="ha-page-url">${esc(c.ha_page)}</code> <button class="btn sm ghost" onclick="copyText($('#ha-page-url').textContent)">copia</button></div>` : '')
      : 'Non installato sul cabinato.';
  } catch (e) { el.textContent = 'Cabinato non raggiungibile: ' + e.message; }
}
async function cabInstall() {
  const pin = $('#cab-pin').value.trim(); if (pin.length < 4) return toast('PIN di almeno 4 cifre', 'err');
  if (!await dlg(`<p>Installo il server in <code>/userdata/system/companion</code> sul cabinato e lo registro come servizio Batocera (parte a ogni accensione).</p><p class="hint">Usa l'IP/utente/password SSH di queste impostazioni per parlare con il cabinato da dentro (localhost).</p>`, 'Installa')) return;
  $('#cab-job').hidden = false; $('#cab-log').textContent = '';
  try { const r = await api('cabinet/install', {pin, port: +$('#cab-port').value || 8765}); await pollJob(r.job, bindJobUI('cab')); toast('Server installato sul cabinato', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
  $('#cab-job').hidden = true; cabLoad();
}
async function cabRemove() {
  if (!await dlg('<p>Rimuovo il server dal cabinato (servizio e binario). Le impostazioni e i database in /userdata/system/companion restano.</p>', 'Rimuovi')) return;
  try { await api('cabinet/remove', {}); toast('Rimosso', 'ok'); } catch (e) { toast(e.message, 'err'); } cabLoad();
}
async function lanLogoutAll() { if (!await dlg('<p>Tutti i dispositivi collegati a questo PC dovranno reinserire il PIN.</p>', 'Scollega')) return; try { await api('lan/logout_all', {}); toast('Sessioni azzerate', 'ok'); } catch (e) { toast(e.message, 'err'); } }

function copyText(t) { (navigator.clipboard ? navigator.clipboard.writeText(t) : Promise.reject()).then(() => toast('Copiato', 'ok')).catch(() => prompt('Copia:', t)); }

// ---------- pulsanti volume ----------
const vb = {up: 0, down: 0, name: ''};
async function vbLoad() {
  const el = $('#vb-state'); if (!el) return;
  try {
    const v = await api('volbtn');
    if (v.config) { vb.up = v.config.up; vb.down = v.config.down; vb.name = v.config.name || ''; $('#vb-up').value = 'codice ' + vb.up; $('#vb-down').value = 'codice ' + vb.down; $('#vb-pct').value = v.config.step || 5; $('#vb-max').value = v.config.max || 100; $('#vb-boot').value = v.config.boot != null ? v.config.boot : 50; $('#vb-int').value = v.config.interval != null ? v.config.interval : 1; }
    $('#vb-popup').checked = v.popup !== false;
    el.innerHTML = v.installed ? `<b>Installato</b> · servizio ${v.running ? '<span class="tag ok">in esecuzione</span>' : '<span class="tag err">fermo</span>'} · volume attuale ${esc(v.volume)}%` : 'Non installato.';
  } catch (e) { el.textContent = 'Cabinato non raggiungibile: ' + e.message; }
}
async function vbDetect(which) {
  const inp = $('#vb-' + which); inp.value = 'premi ora il pulsante sul cabinato… (15 s)';
  try {
    const r = await api('volbtn/detect', {});
    vb[which] = r.code; vb.name = r.name || '';
    inp.value = `codice ${r.code} · ${r.name}`;
    toast('Pulsante rilevato', 'ok');
  } catch (e) { inp.value = ''; toast(e.message, 'err'); }
}
async function vbInstall() {
  if (!vb.up || !vb.down) return toast('Rileva prima i due pulsanti', 'err');
  $('#vb-job').hidden = false; $('#vb-log').textContent = '';
  try { const r = await api('volbtn/install', {up: vb.up, down: vb.down, step: +$('#vb-pct').value || 5, max: +$('#vb-max').value || 150, boot: +$('#vb-boot').value, interval: +$('#vb-int').value || 1, popup: $('#vb-popup').checked, name: vb.name}); await pollJob(r.job, bindJobUI('vb')); toast('Pulsanti volume attivi', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
  $('#vb-job').hidden = true; vbLoad();
}
async function vbRemove() { try { await api('volbtn/remove', {}); toast('Rimosso', 'ok'); } catch (e) { toast(e.message, 'err'); } vbLoad(); }

// ---------- plancia → tastiera (Wine) ----------
async function wkLoad() {
  const el = $('#wk-state'); if (!el) return;
  try { const t = await api('winekeys'); $('#wk-swap').checked = !!t.swap; $('#wk-p1').value = t.p1_phys || ''; el.innerHTML = t.installed ? `<b>Installato</b> · servizio ${t.running ? '<span class="tag ok">attivo</span>' : '<span class="tag err">fermo</span>'}` : 'Non installato.'; }
  catch (e) { el.textContent = 'Non raggiungibile: ' + e.message; }
}
async function wkAction(action) {
  try { const r = await api('winekeys', {action, swap: $('#wk-swap').checked, p1_phys: $('#wk-p1').value}); toast(action === 'install' ? 'Installato: ' + (r.status || '') : 'Rimosso', 'ok'); } catch (e) { toast(e.message, 'err'); }
  wkLoad();
}

async function wkDetect() {
  toast('Vai al cabinato e muovi la LEVA del giocatore 1 (hai 60 secondi)', 'ok');
  try { const r = await api('winekeys', {action: 'detect'}); $('#wk-p1').value = r.phys || ''; toast('Rilevato ' + (r.device || '') + ': ora premi "Installa sul cabinato"', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
}

// ---------- Tailscale ----------
let tsInfo = null;
async function tsLoad() {
  const el = $('#ts-state'); if (!el) return;
  try {
    const t = await api('tailscale'); tsInfo = t;
    if (!t.installed) { el.innerHTML = 'Tailscale <b>non installato</b> sul cabinato: installalo dal menu <b>Add-on</b> di Batocera (o dalla pagina Add-on qui), poi torna qui.'; return; }
    const st = t.state || (t.running ? 'Running' : 'Stopped');
    const stIt = {Running: '<span class="tag ok">connesso</span>', NeedsLogin: '<span class="tag err">serve accesso all\'account</span>', Stopped: '<span class="tag err">fermo</span>', NoState: '<span class="tag err">fermo</span>', Starting: '<span class="tag">in avvio</span>'}[st] || esc(st);
    let h = `<b>Installato</b> ${esc(t.version || '')} · stato ${stIt} · avvio automatico <b>${t.enabled ? 'sì' : 'NO'}</b>`;
    if (t.ip) h += `<br>IP Tailscale del cabinato: <code>${esc(t.ip)}</code>` + (t.dns_name ? ` (<code>${esc(t.dns_name)}</code>)` : '') + ` <button class="btn small" onclick="copyText('${esc(t.ip)}')">copia</button>`;
    if (t.app_url) h += `<br>Web app da fuori casa (telefono con Tailscale acceso): <code>${esc(t.app_url)}</code> <button class="btn small" onclick="copyText('${esc(t.app_url)}')">copia</button>`;
    if (t.auth_url) h += `<br>Accesso richiesto: <a href="${esc(t.auth_url)}" target="_blank">${esc(t.auth_url)}</a>`;
    if (t.key_expiry) h += `<br><span class="muted">Scadenza chiave: ${esc(t.key_expiry.slice(0, 10))} — nel pannello Tailscale (Machines → cabinato → Disable key expiry) togli la scadenza, altrimenti ogni ~6 mesi serve un nuovo accesso.</span>`;
    if (t.via_remote) h += `<br><b>Adesso sei collegato via Tailscale.</b>`;
    else if (t.remote_host) h += `<br>Indirizzo remoto salvato in questo Companion: <code>${esc(t.remote_host)}</code>`;
    el.innerHTML = h;
  } catch (e) { el.textContent = 'Non raggiungibile: ' + e.message; }
}
async function tsAction(action) {
  const el = $('#ts-state');
  try {
    el.textContent = 'Eseguo…';
    const r = await api('tailscale/action', {action});
    if (action === 'login') {
      if (r.auth_url) { window.open(r.auth_url, '_blank'); toast('Apri il link e accedi con il tuo account Tailscale', 'ok'); }
      else toast(r.output && r.output.includes('Success') ? 'Già collegato' : 'Nessun link ricevuto: ' + (r.output || '').slice(0, 200), r.output && r.output.includes('Success') ? 'ok' : 'err');
    } else toast('Fatto', 'ok');
  } catch (e) { toast(e.message, 'err'); }
  tsLoad();
}
function tsUseRemote() {
  if (!tsInfo || !tsInfo.ip) return toast('IP Tailscale non disponibile', 'err');
  $('#c-remote').value = tsInfo.ip; saveConfig();
}

// ---------- Home Assistant ----------
async function haLoad() {
  const el = $('#ha-state'); if (!el) return;
  try {
    const h = await api('ha');
    const c = h.config || {};
    if (c.MQTT_HOST && !$('#ha-host').value) { $('#ha-host').value = c.MQTT_HOST; $('#ha-port').value = c.MQTT_PORT || 1883; $('#ha-user').value = c.MQTT_USER || ''; $('#ha-name').value = c.DEVICE_NAME || ''; $('#ha-id').value = c.DEVICE_ID || ''; }
    el.innerHTML = (h.installed ? `<b>Installato</b> · agente ${h.running ? '<span class="tag ok">in esecuzione</span>' : '<span class="tag err">fermo</span>'} · avvio automatico ${h.enabled ? 'sì' : 'no'} · broker ${esc(c.MQTT_HOST || '?')}:${esc(c.MQTT_PORT || '')}` : 'Non installato.')
      + (h.mosquitto ? '' : ' · <span style="color:var(--err)">mosquitto_pub assente su questa Batocera</span>')
      + (h.mac ? ` · MAC cabinato (per Wake-on-LAN): <code>${esc(h.mac)}</code>` : '')
      + (h.log ? `<pre class="log" style="margin-top:6px">${esc(h.log)}</pre>` : '');
  } catch (e) { el.textContent = 'Cabinato non raggiungibile: ' + e.message; }
}
async function haInstall() {
  const body = {host: $('#ha-host').value.trim(), port: +$('#ha-port').value || 1883, user: $('#ha-user').value.trim(), password: $('#ha-pass').value, device_name: $('#ha-name').value.trim(), device_id: $('#ha-id').value.trim()};
  if (!body.host) return toast('Inserisci l\'IP del broker MQTT (Home Assistant)', 'err');
  if (!body.password && !await dlg('<p>Password MQTT vuota: procedo solo se il broker accetta connessioni anonime.</p>', 'Procedi')) return;
  $('#ha-job').hidden = false; $('#ha-log').textContent = '';
  try { const r = await api('ha/install', body); await pollJob(r.job, bindJobUI('ha')); toast('Agente Home Assistant installato', 'ok'); $('#ha-pass').value = ''; }
  catch (e) { await dlg(`<p>Installazione fallita:</p><pre class="log">${esc(e.message)}</pre>`, 'OK'); }
  haLoad();
}
async function haRemove() {
  if (!await dlg('<p>Rimuovo l\'agente Home Assistant dal cabinato (servizio, script e hook di EmulationStation). Il dispositivo MQTT in Home Assistant va poi eliminato a mano.</p>', 'Rimuovi')) return;
  $('#ha-job').hidden = false; $('#ha-log').textContent = '';
  try { const r = await api('ha/remove', {}); await pollJob(r.job, bindJobUI('ha')); toast('Rimosso', 'ok'); } catch (e) { toast(e.message, 'err'); }
  $('#ha-job').hidden = true; haLoad();
}

// avvio
loadConfig().then(() => { if (cfg && !cfg.host) go('settings'); });
try {
  const h = location.hash.slice(1);
  if (h && $(`nav a[data-view="${h}"]`)) go(h);
  else { const v = localStorage.getItem('view'); if (v && v !== 'settings') go(v); }
} catch (e) {}


// ---------- PLANCIA ----------
const ES_LABEL = {a: 'A', b: 'B', x: 'X', y: 'Y', pageup: 'L', pagedown: 'R', select: 'SELECT', start: 'START', hotkey: 'HOTKEY', 'select+hotkey': 'SELECT · HOTKEY', 'start+hotkey': 'START · HOTKEY'};
var plES = null, plData = null, plEdit = false, plTested = new Set();
async function plStart() {
  if (plES) return;
  $('#pl-st').textContent = 'cerco i joystick…';
  try { plData = await api('plancia'); } catch (e) { $('#pl-st').textContent = 'Errore: ' + e.message; return; }
  $('#pl-swap').checked = !!plData.swap;
  plRender();
  const es = new EventSource('/plancia/events' + (window.BC_KEY ? '?key=' + encodeURIComponent(window.BC_KEY) : ''));
  plES = es;
  const log = $('#pl-log');
  es.onmessage = ev => {
    const f = ev.data.split('|');
    if (log) { log.textContent = (log.textContent + ev.data + '\n').split('\n').slice(-60).join('\n'); log.scrollTop = log.scrollHeight; }
    if (f[0] === 'ready') { if (f[1] === '0') { $('#pl-st').textContent = '❌ nessun joystick trovato sul cabinato (controlla l\'encoder USB)'; es.close(); plES = null; } else $('#pl-st').textContent = '✅ in ascolto su ' + f[1] + ' joystick: premi i comandi sul cabinato'; return; }
    if (f[0] === 'dev' || f[0] === 'ping') return;
    if (f[0] === 'err' || f[0] === 'gone') { $('#pl-st').textContent = '⚠️ ' + ev.data; return; }
    const pi = plPlayer(+f[1]);
    if (f[0] === 'b') plLight(pi, 'b' + f[2], +f[3] === 1);
    if (f[0] === 'a') {
      const code = +f[2], v = +f[3];
      if (code === 16 || code === 0) { plLight(pi, 'left', v < (code ? 0 : 100)); plLight(pi, 'right', v > (code ? 0 : 155)); }
      if (code === 17 || code === 1) { plLight(pi, 'up', v < (code ? 0 : 100)); plLight(pi, 'down', v > (code ? 0 : 155)); }
    }
  };
  es.onerror = () => { $('#pl-st').textContent = '⚠️ collegamento interrotto'; };
}
function plStop() { if (plES) { plES.close(); plES = null; } }
function plPlayer(i) { return plData && plData.swap ? (i === 0 ? 1 : i === 1 ? 0 : i) : i; }
function plLight(p, id, on) {
  const el = $(`#pl-p${p}-${id}`); if (!el) return;
  el.classList.toggle('on', on);
  if (on) { el.classList.add('tested'); plTested.add(p + ':' + id); }
}
function plRender() {
  const d = plData, board = $('#pl-board'); if (!d) return;
  const esFor = (dev, idx) => { const m = (d.es || {})[dev.name] || Object.values(d.es || {})[0] || {}; return m['button:' + idx] || ''; };
  const devs = d.devices && d.devices.length ? d.devices : [{name: 'nessun joystick', event: ''}];
  const html = devs.slice(0, 2).map((dev, i) => {
    const p = i, lab = k => d.labels[k] || ('BTN ' + k);
    const btn = (k, cls) => `<div class="pl-btn ${cls || ''}" id="pl-p${p}-b${k}" data-k="${k}" onclick="plRename(${k})"><span class="ok">✓</span><span class="lab">${esc(lab(k))}</span><span class="es">${esc(ES_LABEL[esFor(dev, k)] || esFor(dev, k) || '')}</span></div>`;
    return `<div class="pl-player"><h3>Giocatore ${p + 1} <span class="dev">${esc(dev.name)} ${esc(dev.event || '')}</span></h3>
      <div class="pl-row">
        <div class="pl-stick"><div></div><div class="d" id="pl-p${p}-up">▲</div><div></div><div class="d" id="pl-p${p}-left">◀</div><div class="c"></div><div class="d" id="pl-p${p}-right">▶</div><div></div><div class="d" id="pl-p${p}-down">▼</div><div></div></div>
        <div class="pl-btns">${btn(0)}${btn(1)}${btn(2)}${btn(3)}${btn(4)}${btn(5)}</div>
      </div>
      <div class="pl-sys">${btn(8)}${btn(9)}${[6, 7, 10, 11].filter(k => d.labels[String(k)]).map(k => btn(k)).join('')}</div>
    </div>`;
  }).join('');
  board.innerHTML = html;
  board.classList.toggle('edit', plEdit);
}
function plEditToggle() { plEdit = !plEdit; $('#pl-board').classList.toggle('edit', plEdit); $('#pl-edit-btn').textContent = plEdit ? '✅ Fine rinomina' : '✏️ Rinomina pulsanti'; $('#pl-hint').textContent = plEdit ? 'Clicca un pulsante e scrivi la scritta che ha sulla plancia (es. JAB). Lascia vuoto per il nome predefinito.' : 'Premi ogni comando sul cabinato: quelli già provati restano segnati con ✓. In modalità rinomina clicca un pulsante per cambiarne la scritta.'; }
async function plRename(k) {
  if (!plEdit) return;
  const cur = plData.labels[String(k)] || '';
  const v = prompt('Scritta sulla plancia per il pulsante ' + k + ':', cur); if (v === null) return;
  const labels = Object.assign({}, plData.labels);
  if (v.trim()) labels[String(k)] = v.trim(); else { delete labels[String(k)]; if (plData.defaults[String(k)]) labels[String(k)] = plData.defaults[String(k)]; }
  try { await api('plancia', {labels}); plData.labels = labels; plRender(); toast('Salvato', 'ok'); } catch (e) { toast(e.message, 'err'); }
}
async function plSwap(on) { try { await api('plancia', {swap: on}); plData.swap = on; } catch (e) { toast(e.message, 'err'); } }
function plResetTest() { plTested.clear(); $$('#pl-board .tested').forEach(e => e.classList.remove('tested')); }

// ---------- MODALITÀ STOCK ----------
var stockData = null, stockSel = new Set();
async function stockLoad() {
  try { stockData = await api('stock'); } catch (e) { $('#stock-state').textContent = e.message; return; }
  stockSel = new Set(stockData.selected || []);
  const sel = $('#stock-sys'); const cur = sel.value;
  sel.innerHTML = '<option value="">Tutti i sistemi</option>' + (stockData.systems || []).map(sy => `<option value="${esc(sy.name)}">${esc(sy.fullname || sy.name)} (${sy.games.length})</option>`).join('');
  if (cur) sel.value = cur;
  const kiosk = (stockData.current || {}).UIMode === 'Kiosk';
  $('#stock-kiosk').checked = stockData.active ? kiosk : true;
  $('#stock-state').textContent = stockData.active ? '● ATTIVA' : '○ non attiva';
  $('#stock-on').disabled = false; $('#stock-off').disabled = !stockData.active;
  $('#stock-on').textContent = stockData.active ? '🎬 Aggiorna la selezione' : '🎬 Attiva';
  stockRender();
}
function stockRows() {
  const sys = $('#stock-sys').value, q = ($('#stock-filter').value || '').toLowerCase();
  const rows = [];
  for (const sy of (stockData && stockData.systems) || []) {
    if (sys && sy.name !== sys) continue;
    for (const g of sy.games) {
      if (q && !(g.name || '').toLowerCase().includes(q) && !(g.path || '').toLowerCase().includes(q)) continue;
      rows.push({key: sy.name + '|' + g.path, sys: sy, g});
    }
  }
  return rows;
}
function stockRender() {
  if (!stockData) return;
  const rows = stockRows();
  $('#stock-games').innerHTML = rows.slice(0, 600).map(r => `<label class="chk" title="${esc(r.g.path)}"><input type="checkbox" data-key="${esc(r.key)}" ${stockSel.has(r.key) ? 'checked' : ''} onchange="stockToggle(this)"> ${esc(r.g.name)} <span class="hint">${esc(r.sys.name)}${r.g.hidden ? ' · nascosto' : ''}</span></label>`).join('') || '<div class="hint">Nessun gioco</div>';
  $('#stock-count').textContent = `${stockSel.size} giochi scelti` + (rows.length > 600 ? ` · mostrati 600 di ${rows.length}` : '');
}
function stockToggle(el) { el.checked ? stockSel.add(el.dataset.key) : stockSel.delete(el.dataset.key); $('#stock-count').textContent = `${stockSel.size} giochi scelti`; }
function stockPickAll(on) { for (const r of stockRows()) on ? stockSel.add(r.key) : stockSel.delete(r.key); stockRender(); }
function stockPick(q) { for (const sy of stockData.systems) for (const g of sy.games) if ((g.name || '').toLowerCase().includes(q) || (g.path || '').toLowerCase().includes(q.replace(/ /g, ''))) stockSel.add(sy.name + '|' + g.path); stockRender(); }
async function stockSet(action) {
  const games = [...stockSel];
  if (action === 'on' && !games.length) { toast('Scegli almeno un gioco da lasciare visibile', 'err'); return; }
  if (action === 'on' && !await dlg(`<p>Attivo la modalità stock: in lista restano <b>${games.length}</b> giochi, gli altri vengono nascosti e i sistemi senza giochi scelti spariscono dal menu${$('#stock-kiosk').checked ? '; i menu di configurazione vengono nascosti (Kiosk)' : ''}. EmulationStation si riavvia.</p><p class="hint">Per tornare indietro: "Disattiva" qui.</p>`, 'Attiva')) return;
  try { const r = await api('stock', {action, games, kiosk: $('#stock-kiosk').checked}); toast(action === 'on' ? `Modalità stock attiva: ${r.hidden_games} giochi nascosti, EmulationStation si sta riavviando` : 'Ripristinato: EmulationStation si sta riavviando', 'ok'); setTimeout(stockLoad, 2000); }
  catch (e) { toast(e.message, 'err'); }
}

// ---------- AGGIORNAMENTI ----------
async function upLoad(force) {
  let d;
  try { d = force ? await api('updates', {action: 'refresh'}) : await api('updates'); } catch (e) { if (force) toast(e.message, 'err'); return; }
  const items = (d.items || []).filter(it => !it.dismissed);
  const badge = $('#up-badge'); if (badge) { badge.textContent = items.length; badge.hidden = !items.length; }
  const card = $('#up-card'); if (!card) return;
  card.hidden = !items.length && !force;
  $('#up-when').textContent = d.checked ? '· controllato ' + d.checked : '';
  $('#up-list').innerHTML = items.length ? items.map(it => `<div class="up-item"><span class="ic">${it.kind === 'batocera' ? '🐧' : it.kind === 'flatpak' ? '🧩' : 'ℹ️'}</span><div><div class="t">${esc(it.title)}</div>${it.list ? '<div class="n">' + it.list.map(esc).join(' · ') + '</div>' : ''}<div class="n">${esc(it.note || '')}</div></div>
    <div class="btns">${it.kind === 'flatpak' ? '<button class="btn primary" onclick="upFlatpak()">⬆ Aggiorna</button>' : ''}<button class="btn" data-key="${esc(it.key)}" data-value="${esc(it.value)}" onclick="upDismiss(this.dataset.key, this.dataset.value)" title="Non mostrare più questo avviso finché non cambia">Ignora</button></div></div>`).join('')
    : `<div class="hint">Tutto aggiornato: Batocera ${esc(d.batocera_current || '')} (ultima stabile ${esc(d.batocera_stable || '?')}${d.batocera_beta ? ', beta ' + esc(d.batocera_beta) : ''}), ${esc(d.flatpak_installed || '0')} app Flatpak.</div>`;
}
async function upDismiss(key, value) { try { await api('updates', {action: 'dismiss', key, value}); upLoad(false); } catch (e) { toast(e.message, 'err'); } }
async function upFlatpak() {
  if (!await dlg('<p>Aggiorno tutte le app Flatpak sul cabinato (Steam, Chiaki…). Può richiedere diversi minuti e scarica dati; non farlo con un gioco in corso.</p>', 'Aggiorna')) return;
  $('#up-job').hidden = false; $('#up-log').textContent = '';
  try { const r = await api('updates', {action: 'flatpak-update'}); await pollJob(r.job, bindJobUI('up')); toast('App Flatpak aggiornate', 'ok'); upLoad(true); }
  catch (e) { toast(e.message, 'err'); }
}
setTimeout(() => upLoad(false), 4000);
setInterval(() => upLoad(false), 6 * 3600 * 1000);

// ---------- REPORT PDF ----------
function reportPDF() { toast('Preparo il report (qualche secondo)…', 'ok'); window.open('/report.pdf?refresh=1' + (window.BC_KEY ? '&key=' + encodeURIComponent(window.BC_KEY) : ''), '_blank'); }

// ---------- test: stress e prestazioni ----------
let sxJob = null, pfJob = null;
async function sxLoad() { try { const r = await api('stress'); sxRender(r.runs || []); } catch (e) {} }
function sxChart(runs) {
  const W = 760, H = 260, L = 44, B = 28, T = 12, R = 12;
  const pts = runs.filter(r => r.points && r.points.length);
  if (!pts.length) return '<div class="muted">Nessuna prova ancora. Avvia la prima: i risultati restano salvati e si confrontano qui.</div>';
  const maxT = Math.max(...pts.map(r => r.points[r.points.length - 1][0]));
  let lo = Math.min(...pts.flatMap(r => r.points.map(p => p[1]))) - 3, hi = Math.max(...pts.flatMap(r => r.points.map(p => p[1]))) + 3;
  lo = Math.floor(lo / 5) * 5; hi = Math.ceil(hi / 5) * 5; if (hi - lo < 20) hi = lo + 20;
  const x = t => L + (t / maxT) * (W - L - R), y = v => T + (1 - (v - lo) / (hi - lo)) * (H - T - B);
  const cols = ['#4f8cff', '#ff7a45', '#2ecc71', '#c86bff', '#ffd23f', '#ff4f6d', '#1dd3d0', '#9aa0a6'];
  let g = '';
  for (let v = lo; v <= hi; v += 5) g += `<line x1="${L}" x2="${W - R}" y1="${y(v)}" y2="${y(v)}" stroke="var(--line)"/><text x="${L - 6}" y="${y(v) + 4}" text-anchor="end" font-size="11" fill="var(--fg3)">${v}°</text>`;
  for (let t = 0; t <= maxT; t += 60) g += `<text x="${x(t)}" y="${H - 8}" text-anchor="middle" font-size="11" fill="var(--fg3)">${t / 60} min</text>`;
  g += `<line x1="${x(15)}" x2="${x(15)}" y1="${T}" y2="${H - B}" stroke="var(--fg3)" stroke-dasharray="3 3"/><text x="${x(15) + 4}" y="${T + 10}" font-size="10" fill="var(--fg3)">carico</text>`;
  if (hi >= 85) g += `<line x1="${L}" x2="${W - R}" y1="${y(85)}" y2="${y(85)}" stroke="var(--warn)" stroke-dasharray="4 3"/>`;
  pts.forEach((r, i) => { g += `<polyline fill="none" stroke="${cols[i % cols.length]}" stroke-width="2" points="${r.points.map(p => x(p[0]).toFixed(1) + ',' + y(p[1]).toFixed(1)).join(' ')}"/>`; });
  const legend = pts.map((r, i) => `<span style="display:inline-flex;align-items:center;gap:6px;margin-right:14px"><i style="width:14px;height:3px;background:${cols[i % cols.length]};display:inline-block"></i>${esc(r.label)} <span class="muted">max ${r.max_temp}°</span></span>`).join('');
  return `<svg viewBox="0 0 ${W} ${H}" style="width:100%;max-width:${W}px;display:block">${g}</svg><div style="margin-top:6px;font-size:13px">${legend}</div>`;
}
function sxRender(runs) {
  $('#sx-chart').innerHTML = sxChart(runs);
  $('#sx-runs').innerHTML = runs.length ? '<table class="tbl"><tr><th>Prova</th><th>Durata</th><th>Riposo</th><th>Max</th><th>Fine</th><th>MHz inizio→fine</th><th></th></tr>' + runs.slice().reverse().map(r => `<tr><td><b>${esc(r.label)}</b><div class="muted" style="font-size:11px">${new Date(r.at).toLocaleString()}</div>${(r.notes || []).map(n => `<div style="font-size:12px">${esc(n)}</div>`).join('')}</td><td>${r.minutes} min</td><td>${r.idle_temp}°</td><td><b>${r.max_temp}°</b>${r.t_to_max != null ? `<div class="muted" style="font-size:11px">in ${r.t_to_max}s</div>` : ''}</td><td>${r.end_temp}°</td><td>${r.mhz_start}→${r.mhz_end}</td><td><button class="btn sm" onclick="sxDel('${r.id}')">🗑</button></td></tr>`).join('') + '</table>' : '';
}
async function sxDel(id) { try { const r = await api('stress', {delete: id}); sxRender(r.runs || []); } catch (e) { toast(e.message, 'err'); } }
async function sxRun() {
  $('#sx-job').hidden = false; $('#sx-run').disabled = true; $('#sx-stop').hidden = false; $('#sx-log').textContent = '';
  try {
    const r = await api('stress', {minutes: +$('#sx-min').value, label: $('#sx-label').value});
    sxJob = r.job;
    const res = await pollJob(r.job, jj => { $('#sx-step').textContent = jj.step || ''; $('#sx-pct').textContent = jj.progress >= 0 ? Math.round(jj.progress * 100) + '%' : ''; $('#sx-bar').style.width = (jj.progress >= 0 ? jj.progress * 100 : 0) + '%'; $('#sx-log').textContent = (jj.log || []).join('\n'); });
    sxRender(res.result.runs || []); toast('Stress test completato: max ' + res.result.run.max_temp + ' °C', 'ok');
  } catch (e) { toast(e.message, 'err'); }
  $('#sx-job').hidden = true; $('#sx-run').disabled = false; $('#sx-stop').hidden = true; sxJob = null;
}
async function sxStop() { if (sxJob) try { await api('job/cancel', {id: sxJob}); } catch (e) {} }
async function pfLoad() {
  try {
    const r = await api('perf/games'); const sel = $('#pf-sys'); const cur = sel.value;
    sel.innerHTML = '<option value="">Sistema…</option>' + (r.systems || []).filter(s => s.count > 0).map(s => `<option value="${esc(s.name)}">${esc(s.fullname || s.name)} (${s.count})</option>`).join('');
    if (cur) { sel.value = cur; }
    const p = await api('perf'); pfRender(p.runs || []);
  } catch (e) {}
}
async function pfGames() {
  const sys = $('#pf-sys').value; const gs = $('#pf-game'); gs.innerHTML = '<option value="">Gioco…</option>';
  if (!sys) return;
  try { const r = await api('perf/games?system=' + encodeURIComponent(sys)); gs.innerHTML += (r.games || []).map(g => `<option value="${esc(g.Path)}">${esc(g.Name)}</option>`).join(''); } catch (e) { toast(e.message, 'err'); }
}
function pfRender(runs) {
  const bar = (v, max, cls) => `<div class="bar" style="height:8px;margin-top:4px"><i style="width:${Math.min(100, v / max * 100)}%;background:${cls}"></i></div>`;
  const col = v => v >= 90 ? 'var(--err)' : v >= 75 ? 'var(--warn)' : 'var(--ok)';
  $('#pf-out').innerHTML = runs.slice().reverse().map(r => `<div class="finding ${r.verdict.startsWith('❌') ? 'error' : r.verdict.startsWith('⚠') ? 'warn' : 'ok'}"><i></i><div class="body">
      <span class="t">${esc(r.game)}</span> <span class="muted">${esc(r.system)} · ${r.seconds}s · ${new Date(r.at).toLocaleString()}</span>
      <div class="d">${esc(r.verdict)}</div>
      <div style="display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:10px;margin-top:8px">${(r.results || []).map(x => x.bench ? `<div style="background:var(--bg);border:1px solid var(--line);border-radius:8px;padding:8px 10px;font-size:13px"><b>${esc(x.label)}</b>
        <div style="margin-top:6px;font-size:26px;font-weight:700;color:${x.speed >= 1.8 ? 'var(--ok)' : x.speed >= 1.15 ? 'var(--warn)' : 'var(--err)'}">${Number(x.speed).toFixed(2)}×</div>
        <div class="muted">velocità rispetto al tempo reale${x.fps ? ' · ' + x.fps + ' fps' : ''} · ${x.mode === 'mame' ? 'MAME -bench' : 'RetroArch'} ${x.secs ? x.secs + ' s' : ''} · ${x.temp_max} °C</div>
        <div class="bar" style="height:8px;margin-top:4px"><i style="width:${Math.min(100, x.speed / 4 * 100)}%;background:${x.speed >= 1.8 ? 'var(--ok)' : x.speed >= 1.15 ? 'var(--warn)' : 'var(--err)'}"></i></div>
        ${x.conf !== undefined ? `<details style="margin-top:4px"><summary class="muted" style="cursor:pointer">impostazioni</summary><pre class="log" style="max-height:90px">${esc(x.conf) || '(nessuna: valori globali)'}</pre></details>` : ''}</div>` : `<div style="background:var(--bg);border:1px solid var(--line);border-radius:8px;padding:8px 10px;font-size:13px"><b>${esc(x.label)}</b>
        <div style="margin-top:6px">Core più carico <b style="color:${col(x.core_avg)}">${x.core_avg}%</b> <span class="muted">(picco ${x.core_max}%)</span>${bar(x.core_avg, 100, col(x.core_avg))}</div>
        <div style="margin-top:6px">Emulatore ${x.emu_avg}% di ${x.ncpu} core · CPU totale ${x.cpu_avg}% · ${x.mhz_avg} MHz</div>
        <div>GPU ${x.gpu_avg}${x.gpu_max ? '/' + x.gpu_max : ''} MHz · max ${x.temp_max} °C</div>
        ${x.conf ? `<details style="margin-top:4px"><summary class="muted" style="cursor:pointer">impostazioni</summary><pre class="log" style="max-height:90px">${esc(x.conf) || '(nessuna: valori globali)'}</pre></details>` : ''}</div>`).join('')}</div>
      ${r.compare_note ? `<div class="d" style="margin-top:8px"><b>${esc(r.compare_note)}</b></div>` : ''}
      ${(r.tips || []).length ? `<div class="d" style="margin-top:6px">${r.tips.map(t => '💡 ' + esc(t)).join('\n')}</div>` : ''}
    </div><div class="act" style="padding:10px 10px 0 0"><button class="btn sm" onclick="pfDel('${r.id}')">🗑</button></div></div>`).join('') || '<div class="muted">Nessuna prova ancora.</div>';
}
async function pfDel(id) { try { const r = await api('perf', {delete: id}); pfRender(r.runs || []); } catch (e) { toast(e.message, 'err'); } }
async function pfRun() {
  const sys = $('#pf-sys').value, path = $('#pf-game').value, name = $('#pf-game').selectedOptions[0]?.textContent || '';
  if (!sys || !path) return toast('Scegli sistema e gioco', 'err');
  $('#pf-job').hidden = false; $('#pf-run').disabled = true; $('#pf-stop').hidden = false;
  try {
    const r = await api('perf', {system: sys, path, name, seconds: +$('#pf-sec').value, compare: $('#pf-cmp').checked, live: $('#pf-live').checked});
    pfJob = r.job;
    const res = await pollJob(r.job, jj => { $('#pf-step').textContent = jj.step || ''; $('#pf-pct').textContent = jj.progress >= 0 ? Math.round(jj.progress * 100) + '%' : ''; $('#pf-bar').style.width = (jj.progress >= 0 ? jj.progress * 100 : 0) + '%'; });
    pfRender(res.result.runs || []); toast('Test completato', 'ok');
  } catch (e) { toast(e.message, 'err'); }
  $('#pf-job').hidden = true; $('#pf-run').disabled = false; $('#pf-stop').hidden = true; pfJob = null;
}
async function pfStop() { if (pfJob) try { await api('job/cancel', {id: pfJob}); } catch (e) {} }
