// Mappa event.code del browser -> codici tasto Linux (input-event-codes.h)
const LINUX_KEYS = {
  Escape:1, Digit1:2, Digit2:3, Digit3:4, Digit4:5, Digit5:6, Digit6:7, Digit7:8, Digit8:9, Digit9:10, Digit0:11, Minus:12, Equal:13, Backspace:14, Tab:15,
  KeyQ:16, KeyW:17, KeyE:18, KeyR:19, KeyT:20, KeyY:21, KeyU:22, KeyI:23, KeyO:24, KeyP:25, BracketLeft:26, BracketRight:27, Enter:28, ControlLeft:29,
  KeyA:30, KeyS:31, KeyD:32, KeyF:33, KeyG:34, KeyH:35, KeyJ:36, KeyK:37, KeyL:38, Semicolon:39, Quote:40, Backquote:41, ShiftLeft:42, Backslash:43,
  KeyZ:44, KeyX:45, KeyC:46, KeyV:47, KeyB:48, KeyN:49, KeyM:50, Comma:51, Period:52, Slash:53, ShiftRight:54, NumpadMultiply:55, AltLeft:56, Space:57, CapsLock:58,
  F1:59, F2:60, F3:61, F4:62, F5:63, F6:64, F7:65, F8:66, F9:67, F10:68, F11:87, F12:88,
  Numpad7:71, Numpad8:72, Numpad9:73, NumpadSubtract:74, Numpad4:75, Numpad5:76, Numpad6:77, NumpadAdd:78, Numpad1:79, Numpad2:80, Numpad3:81, Numpad0:82, NumpadDecimal:83,
  NumpadEnter:96, ControlRight:97, NumpadDivide:98, AltRight:100, Home:102, ArrowUp:103, PageUp:104, ArrowLeft:105, ArrowRight:106, End:107, ArrowDown:108, PageDown:109, Insert:110, Delete:111,
};
function sendKey(code, value) {
  return fetch('/api/input' + (window.BC_KEY ? '?key=' + encodeURIComponent(window.BC_KEY) : ''), {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({code, value})})
    .then(r => r.json()).then(j => { if (j.error) throw new Error(j.error); }).catch(e => { if (typeof toast === 'function') toast(e.message, 'err'); else console.warn(e.message); });
}
// Cattura tastiera su un elemento focalizzabile e la inoltra al cabinato.
function bindKeyboard(el, enabled) {
  const pressed = new Set();
  el.addEventListener('keydown', e => {
    if (!enabled()) return;
    // Ctrl+V: prima porto gli appunti del PC sul cabinato, poi premo V là (Ctrl è già premuto)
    if ((e.ctrlKey || e.metaKey) && e.code === 'KeyV') {
      e.preventDefault(); e.stopPropagation();
      if (e.repeat) return;
      clipToCab().finally(() => sendKey(47, 1).then(() => sendKey(47, 0)));
      return;
    }
    // Ctrl+C / Ctrl+X: li premo sul cabinato e poi riporto i suoi appunti sul PC
    if ((e.ctrlKey || e.metaKey) && (e.code === 'KeyC' || e.code === 'KeyX') && !e.repeat) {
      setTimeout(clipFromCab, 400);
    }
    const code = LINUX_KEYS[e.code]; if (!code) return;
    e.preventDefault(); e.stopPropagation();
    if (pressed.has(code)) return; // autorepeat del browser
    pressed.add(code); sendKey(code, 1);
  });
  el.addEventListener('keyup', e => {
    if (!enabled()) return;
    if (e.code === 'KeyV' && !pressed.has(47)) { e.preventDefault(); return; }
    const code = LINUX_KEYS[e.code]; if (!code) return;
    e.preventDefault(); e.stopPropagation();
    pressed.delete(code); sendKey(code, 0);
  });
  el.addEventListener('blur', () => { for (const c of pressed) sendKey(c, 0); pressed.clear(); });
}

// Gamepad touch (telefono): riproduce la plancia del cabinato (joystick + 6 pulsanti con le scritte
// JAB/STRONG/… + Start/Select) e manda i tasti della tastiera virtuale. La mappa cambia da sola:
// nel menu di EmulationStation usa i tasti del menu (Invio=A, Backspace=B…), in gioco quelli di RetroArch.
const PAD_ES = {up: 103, down: 108, left: 105, right: 106, a: 28, b: 14, x: 45, y: 21, start: 57, select: 31, pageup: 16, pagedown: 17, hotkey: 31};
const PAD_GAME = {up: 103, down: 108, left: 105, right: 106, a: 45, b: 44, x: 31, y: 30, start: 28, select: 54, pageup: 16, pagedown: 17, hotkey: 54};
const PAD_DEF = {0: 'a', 1: 'b', 2: 'x', 3: 'y', 4: 'pageup', 5: 'pagedown', 8: 'select', 9: 'start'};
let padInGame = false;
function buildPad(el) {
  if (!el) return;
  const q = window.BC_KEY ? '?key=' + encodeURIComponent(window.BC_KEY) : '';
  const render = (labels, esmap) => {
    const nameOf = k => { const m = esmap['button:' + k] || PAD_DEF[k] || ''; return m.split('+')[0]; };
    const K = (label, name, cls, sub) => `<div class="k ${cls || ''}" data-name="${name}">${label}${sub ? '<small>' + sub + '</small>' : ''}</div>`;
    const B = k => K(labels[k] || PAD_DEF[k] || k, nameOf(k), 'pb' + (String(labels[k] || '').length > 7 ? ' long' : ''), (nameOf(k) || '').toUpperCase().replace('PAGEUP', 'L').replace('PAGEDOWN', 'R'));
    el.innerHTML = `<div class="dpad"><div></div>${K('▲', 'up')}<div></div>${K('◀', 'left')}<div class="mode" id="pad-mode">menu</div>${K('▶', 'right')}<div></div>${K('▼', 'down')}<div></div></div>
      <div class="mid">${K(labels[8] || 'SELECT', nameOf(8))}${K(labels[9] || 'START', nameOf(9))}${K('ESC', 'esc')}${K('F1', 'f1')}</div>
      <div class="plbtns">${B(0)}${B(1)}${B(2)}${B(3)}${B(4)}${B(5)}</div>`;
    const codeOf = name => name === 'esc' ? 1 : name === 'f1' ? 59 : (padInGame ? PAD_GAME : PAD_ES)[name];
    el.querySelectorAll('.k[data-name]').forEach(k => {
      let code = 0;
      const down = e => { e.preventDefault(); code = codeOf(k.dataset.name); if (!code) return; k.classList.add('on'); try { navigator.vibrate && navigator.vibrate(8); } catch (x) {} sendKey(code, 1); };
      const up = e => { e.preventDefault(); k.classList.remove('on'); if (code) sendKey(code, 0); code = 0; };
      k.addEventListener('touchstart', down, {passive: false}); k.addEventListener('touchend', up); k.addEventListener('touchcancel', up);
      k.addEventListener('mousedown', down); k.addEventListener('mouseup', up); k.addEventListener('mouseleave', e => { if (k.classList.contains('on')) up(e); });
    });
  };
  render({}, {});
  fetch('/api/plancia' + q).then(r => r.json()).then(d => {
    if (d.error) return;
    const first = d.devices && d.devices[0]; const esmap = first ? (d.es[first.name] || Object.values(d.es)[0] || {}) : (Object.values(d.es || {})[0] || {});
    render(d.labels || {}, esmap);
  }).catch(() => {});
  const poll = () => fetch('/api/status' + q).then(r => r.json()).then(s => { padInGame = !!s.running; const m = document.getElementById('pad-mode'); if (m) m.textContent = padInGame ? 'gioco' : 'menu'; }).catch(() => {});
  poll(); setInterval(poll, 3000);
}

// Sveglia: esce dal salvaschermo di EmulationStation con un tasto innocuo (Shift sinistro, codice 42)
// Digita una stringa sul cabinato (per URL/codici in app tipo Chiaki).
function typeText(text, enter) {
  return fetch('/api/input/type' + (window.BC_KEY ? '?key=' + encodeURIComponent(window.BC_KEY) : ''), {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify({text, enter: !!enter})})
    .then(r => r.json()).then(j => { if (j.error) throw new Error(j.error); return j; });
}
async function typePrompt() {
  const t = prompt('Testo da digitare sul cabinato (clicca prima nel campo giusto sul cabinato con le frecce/Tab):');
  if (!t) return;
  try { const j = await typeText(t, false); const m = 'Digitati ' + j.typed + ' caratteri' + (j.skipped ? ' (' + j.skipped + ' non digitabili saltati)' : ''); if (typeof toast === 'function') toast(m, 'ok'); else alert(m); }
  catch (e) { if (typeof toast === 'function') toast(e.message, 'err'); else alert(e.message); }
}
function wakeScreen() { return sendKey(42, 1).then(() => sendKey(42, 0)); }

// ---------- appunti condivisi PC <-> cabinato ----------
function bcToast(m, k) { if (typeof toast === 'function') toast(m, k); else console.log(m); }
function clipApi(body) {
  return fetch('/api/clip' + (window.BC_KEY ? '?key=' + encodeURIComponent(window.BC_KEY) : ''), body ? {method: 'POST', headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body)} : {})
    .then(r => r.json()).then(j => { if (j.error) throw new Error(j.error); return j; });
}
async function clipToCab() {
  let t = null;
  try { t = await navigator.clipboard.readText(); } catch (e) { /* browser senza permesso (es. telefono in http) */ }
  if (t == null) t = prompt('Incolla qui il testo da mandare agli appunti del cabinato:');
  if (!t) return;
  try { await clipApi({text: t}); bcToast('Appunti inviati al cabinato (' + t.length + ' caratteri)', 'ok'); }
  catch (e) { bcToast('Appunti: ' + e.message, 'err'); }
}
async function clipFromCab() {
  try {
    const j = await clipApi(null);
    if (!j.text) return;
    try { await navigator.clipboard.writeText(j.text); bcToast('Copiato dal cabinato: ' + (j.text.length > 40 ? j.text.slice(0, 40) + '…' : j.text), 'ok'); }
    catch (e) { prompt('Testo copiato sul cabinato (selezionalo e copialo):', j.text); }
  } catch (e) { bcToast('Appunti: ' + e.message, 'err'); }
}

// ---------- puntatore del mouse sopra la vista live ----------
// (kmsgrab non cattura il cursore: lo ridisegno con le coordinate lette dal cabinato)
function liveCursor(img) {
  if (img._cur) return img._cur;
  const host = img.parentElement; if (getComputedStyle(host).position === 'static') host.style.position = 'relative';
  const el = document.createElement('div');
  el.style.cssText = 'position:absolute;width:0;height:0;pointer-events:none;z-index:5;display:none;border-left:7px solid #fff;border-right:7px solid transparent;border-bottom:12px solid transparent;filter:drop-shadow(0 0 1px #000) drop-shadow(0 0 1px #000);transform:rotate(-20deg);transform-origin:0 0';
  host.appendChild(el);
  let es = null, sw = 0, sh = 0, mx = -1, my = -1, lastMove = 0;
  const place = () => {
    if (!sw || mx < 0 || !img.naturalWidth) { el.style.display = 'none'; return; }
    const r = img.getBoundingClientRect(), hr = host.getBoundingClientRect();
    el.style.left = (r.left - hr.left + mx * r.width / sw) + 'px';
    el.style.top = (r.top - hr.top + my * r.height / sh) + 'px';
    // nascosto se fermo da 8 secondi (Batocera nasconde il cursore quando non serve)
    el.style.display = Date.now() - lastMove < 8000 ? 'block' : 'none';
  };
  const api = {
    start() {
      api.stop();
      es = new EventSource('/live/cursor' + (window.BC_KEY ? '?key=' + encodeURIComponent(window.BC_KEY) : ''));
      es.onmessage = e => {
        const p = e.data.trim().split(/\s+/);
        if (p.length === 2 && !p[0].includes(':')) { sw = +p[0]; sh = +p[1]; return; }
        const x = +(p[0] || '').split(':')[1], y = +(p[1] || '').split(':')[1];
        if (!isNaN(x) && !isNaN(y)) { if (x !== mx || y !== my) lastMove = Date.now(); mx = x; my = y; place(); }
      };
      api._t = setInterval(place, 1000);
    },
    stop() { if (es) { es.close(); es = null; } clearInterval(api._t); el.style.display = 'none'; }
  };
  window.addEventListener('resize', place);
  img._cur = api; return api;
}
