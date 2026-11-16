#!/usr/bin/env python3
# Pulsanti volume - Batocera Companion
import os, struct, select, fcntl, json, time, subprocess, glob, urllib.request
EVIOCGNAME = 0x81004506
EVIOCGPHYS = 0x81004507
CONF = '/userdata/system/companion/volume-buttons.json'
fmt = 'llHHi'; sz = struct.calcsize(fmt)
cfg = json.load(open(CONF))
UP, DOWN, STEP, NAME = cfg['up'], cfg['down'], int(cfg.get('step', 5)), cfg.get('name', '')
PHYS = cfg.get('phys', '')   # porta USB del controller: se impostata, gli altri controller con lo stesso nome vengono ignorati
MAX = max(100, min(200, int(cfg.get('max', 100))))   # volume massimo (PipeWire permette oltre il 100%)
BOOT = int(cfg.get('boot', MAX))                     # volume impostato all'avvio
NOTIFY = bool(cfg.get('notify', False))              # popup di testo di ES (di default no: ES mostra già la sua barra)
GAP = max(0.1, float(cfg.get('interval', 1.0)))        # tempo minimo fra due variazioni (anche tenendo premuto)
last_change = 0.0
def notify(txt):
    try: urllib.request.urlopen(urllib.request.Request('http://127.0.0.1:1234/notify', data=txt.encode()), timeout=1).read()
    except Exception: pass
def pw_env():
    env = dict(os.environ)
    if 'PIPEWIRE_RUNTIME_DIR' in env: return env
    for root in ('/run', '/var/run', '/tmp'):
        for dp, dn, fn in os.walk(root):
            if 'pipewire-0' in fn:
                env['PIPEWIRE_RUNTIME_DIR'] = dp; env['XDG_RUNTIME_DIR'] = dp; return env
            if dp.count('/') > 4: dn[:] = []
    return None
def getvol():
    env = pw_env()
    if env:
        try:
            out = subprocess.run(['wpctl','get-volume','@DEFAULT_AUDIO_SINK@'],capture_output=True,text=True,timeout=3,env=env).stdout
            return int(round(float(out.split()[1]) * 100))
        except Exception: pass
    try: return int(subprocess.run(['batocera-audio','getSystemVolume'],capture_output=True,text=True,timeout=3).stdout.strip() or 50)
    except Exception: return 50
def setvol(v, quiet=False):
    v = max(0, min(MAX, v)); env = pw_env(); ok = False
    if env:
        try: ok = subprocess.run(['wpctl','set-volume','-l','2.0','@DEFAULT_AUDIO_SINK@','%.2f' % (v/100.0)],timeout=3,env=env).returncode == 0
        except Exception: ok = False
    if not ok:
        try: subprocess.run(['batocera-audio','setSystemVolume',str(min(100, v))],timeout=3)
        except Exception: pass
    if not quiet and NOTIFY: notify('Volume %d%%' % v)
    return v
def opendevs():
    fds = {}
    for p in glob.glob('/dev/input/event*'):
        try:
            fd = os.open(p, os.O_RDONLY | os.O_NONBLOCK)
        except Exception: continue
        n = ''
        try:
            name = bytearray(256); fcntl.ioctl(fd, EVIOCGNAME, name); n = name.split(b'\0',1)[0].decode(errors='ignore')
        except Exception: pass
        if NAME and n and NAME not in n: os.close(fd); continue
        if PHYS:
            ph = ''
            try:
                phys = bytearray(256); fcntl.ioctl(fd, EVIOCGPHYS, phys); ph = phys.split(b'\0',1)[0].decode(errors='ignore')
            except Exception: pass
            if ph != PHYS: os.close(fd); continue
        fds[fd] = p
    return fds
# all'avvio: aspetta PipeWire ed EmulationStation, poi imposta il volume di boot (ES lo rimette a 100 quando parte)
for _ in range(60):
    if pw_env(): break
    time.sleep(1)
time.sleep(8); setvol(BOOT, quiet=True); boot_again = time.time() + 20
fds = opendevs(); last_scan = time.time(); held = {}
while True:
    now = time.time()
    if boot_again and now >= boot_again: setvol(BOOT, quiet=True); boot_again = 0
    if now - last_scan > 5:   # nuovi dispositivi (riconnessione USB)
        for fd in fds: os.close(fd)
        fds = opendevs(); last_scan = now
    r, _, _ = select.select(list(fds), [], [], 0.1)
    now = time.time()
    for fd in r:
        try: data = os.read(fd, sz * 64)
        except OSError:
            continue
        for i in range(0, len(data) - sz + 1, sz):
            _, _, t, code, val = struct.unpack(fmt, data[i:i+sz])
            if t != 1 or code not in (UP, DOWN): continue
            if val == 1:
                if now - last_change >= GAP:
                    setvol(getvol() + (STEP if code == UP else -STEP)); last_change = now
                held[code] = True
            elif val == 0:
                held.pop(code, None)
    if held and now - last_change >= GAP:   # tenuto premuto: ripete, al massimo una volta ogni GAP secondi
        code = next(iter(held))
        setvol(getvol() + (STEP if code == UP else -STEP)); last_change = now
