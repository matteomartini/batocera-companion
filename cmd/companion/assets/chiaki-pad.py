#!/usr/bin/env python3
# Batocera Companion: pad virtuale per Chiaki (PS5 Remote Play).
# Legge gli encoder del cabinato (presi in esclusiva) e li ripresenta come un unico gamepad
# "Batocera Companion Pad" con la mappatura scelta nel Companion: tasti → ✕ ○ □ △ L1 R1 L2 R2 L3 R3
# Create Options PS, joystick → croce / levetta sinistra / levetta destra.
# Config: /userdata/system/companion/chiaki-pad.json  {"map": {"<phys>|b:<code>": "cross", "<phys>|a:<asse x>": "lstick", ...}}
# Scrive la mappatura SDL del pad virtuale in /tmp/bc_chiaki_pad.map (la legge Chiaki.sh).
import os, sys, json, struct, fcntl, select, glob, time, signal

CFG = '/userdata/system/companion/chiaki-pad.json'
MAPFILE = '/tmp/bc_chiaki_pad.map'
EVIOCGNAME = 0x81004506; EVIOCGPHYS = 0x81004507; EVIOCGRAB = 0x40044590
def EVIOCGABS(c): return 0x80184540 + c
UI_SET_EVBIT = 0x40045564; UI_SET_KEYBIT = 0x40045565; UI_SET_ABSBIT = 0x40045567
UI_DEV_SETUP = 0x405c5503; UI_ABS_SETUP = 0x401c5504; UI_DEV_CREATE = 0x5501; UI_DEV_DESTROY = 0x5502
EV_SYN, EV_KEY, EV_ABS = 0, 1, 3
ABS_X, ABS_Y, ABS_Z, ABS_RX, ABS_RY, ABS_RZ, ABS_HAT0X, ABS_HAT0Y = 0, 1, 2, 3, 4, 5, 16, 17
BTN = {'cross': 0x130, 'circle': 0x131, 'triangle': 0x133, 'square': 0x134, 'l1': 0x136, 'r1': 0x137,
       'l2': 0x138, 'r2': 0x139, 'create': 0x13a, 'options': 0x13b, 'ps': 0x13c, 'l3': 0x13d, 'r3': 0x13e}
TRIG = {'l2': ABS_Z, 'r2': ABS_RZ}
STICK = {'dpad': (ABS_HAT0X, ABS_HAT0Y), 'lstick': (ABS_X, ABS_Y), 'rstick': (ABS_RX, ABS_RY)}
VID, PID, VER = 0x1209, 0xbc05, 0x0100

def guid():
    b = struct.pack('<HHHHHHHH', 0x06, 0, VID, 0, PID, 0, VER, 0)
    return b.hex()

# ordine SDL: pulsanti in ordine di codice, assi in ordine di codice, hat0 = h0
SDL_MAP = (guid() + ',Batocera Companion Pad,platform:Linux,'
           'a:b0,b:b1,y:b2,x:b3,leftshoulder:b4,rightshoulder:b5,back:b8,start:b9,guide:b10,leftstick:b11,rightstick:b12,'
           'leftx:a0,lefty:a1,lefttrigger:a2,rightx:a3,righty:a4,righttrigger:a5,'
           'dpup:h0.1,dpright:h0.2,dpdown:h0.4,dpleft:h0.8,')

def log(*a):
    try:
        with open('/userdata/system/logs/chiaki-pad.log', 'a') as f: f.write(time.strftime('%H:%M:%S ') + ' '.join(str(x) for x in a) + '\n')
    except Exception: pass

def main():
    try: cfg = json.load(open(CFG))
    except Exception as e:
        log('config non leggibile:', e); return 1
    m = cfg.get('map') or {}
    phys_needed = set(k.split('|')[0] for k in m)
    # dispositivi reali
    devs = {}
    for p in glob.glob('/dev/input/event*'):
        try: fd = os.open(p, os.O_RDONLY | os.O_NONBLOCK)
        except Exception: continue
        ph = ''
        try:
            buf = bytearray(256); fcntl.ioctl(fd, EVIOCGPHYS, buf); ph = buf.split(b'\0', 1)[0].decode(errors='ignore')
        except Exception: pass
        if ph in phys_needed:
            devs[fd] = ph
        else:
            os.close(fd)
    if not devs:
        log('nessun encoder trovato per', phys_needed); return 1
    # pad virtuale
    ui = os.open('/dev/uinput', os.O_WRONLY | os.O_NONBLOCK)
    for ev in (EV_KEY, EV_ABS, EV_SYN): fcntl.ioctl(ui, UI_SET_EVBIT, ev)
    for code in BTN.values(): fcntl.ioctl(ui, UI_SET_KEYBIT, code)
    def absset(code, mn, mx, flat=0):
        fcntl.ioctl(ui, UI_SET_ABSBIT, code)
        fcntl.ioctl(ui, UI_ABS_SETUP, struct.pack('<H2xiiiiii', code, 0, mn, mx, 0, flat, 0))
    for c in (ABS_X, ABS_Y, ABS_RX, ABS_RY): absset(c, -32767, 32767, 128)
    for c in (ABS_Z, ABS_RZ): absset(c, 0, 255)
    for c in (ABS_HAT0X, ABS_HAT0Y): absset(c, -1, 1)
    fcntl.ioctl(ui, UI_DEV_SETUP, struct.pack('<HHHH80sI', 0x06, VID, PID, VER, b'Batocera Companion Pad', 0))
    fcntl.ioctl(ui, UI_DEV_CREATE)
    def emit(t, c, v): os.write(ui, struct.pack('<qqHHi', 0, 0, t, c, v))
    # prendo gli encoder in esclusiva, cosi' Chiaki vede solo il pad virtuale
    absinfo = {}
    for fd, ph in devs.items():
        try: fcntl.ioctl(fd, EVIOCGRAB, 1)
        except Exception as e: log('grab fallito', ph, e)
        for code in range(0, 8):
            try:
                buf = bytearray(24); fcntl.ioctl(fd, EVIOCGABS(code), buf)
                v, mn, mx = struct.unpack('<iii', bytes(buf[:12]))
                absinfo[(ph, code)] = (mn, mx)
            except Exception: pass
    with open(MAPFILE, 'w') as f: f.write(SDL_MAP + '\n')
    log('pad virtuale attivo; encoder:', list(devs.values()), 'mappe:', len(m))
    # tabella: (phys, 'b', code) -> target ; (phys, 'a', code) -> (stick, axis index)
    bmap = {}; amap = {}
    for k, target in m.items():
        ph, src = k.split('|', 1)
        kind, code = src.split(':'); code = int(code)
        if kind == 'b': bmap[(ph, code)] = target
        elif kind == 'a' and target in STICK:
            x = code & ~1
            amap[(ph, x)] = (target, 0); amap[(ph, x + 1)] = (target, 1)
    def stop(*_):
        for fd in devs:
            try: fcntl.ioctl(fd, EVIOCGRAB, 0)
            except Exception: pass
        try: fcntl.ioctl(ui, UI_DEV_DESTROY)
        except Exception: pass
        try: os.remove(MAPFILE)
        except Exception: pass
        sys.exit(0)
    signal.signal(signal.SIGTERM, stop); signal.signal(signal.SIGINT, stop)
    fmt = 'llHHi'; sz = struct.calcsize(fmt)
    # sentinella: se Chiaki non c'e' piu' (chiuso dall'hotkey o crashato) rilascio subito gli encoder,
    # altrimenti EmulationStation resterebbe senza tasti
    started = time.time(); lastchk = 0
    def chiaki_alive():
        for pid in os.listdir('/proc'):
            if not pid.isdigit(): continue
            try:
                with open('/proc/%s/cmdline' % pid, 'rb') as f:
                    if b'Chiaki' in f.read(): return True
            except Exception: pass
        return False
    while True:
        now = time.time()
        if now - lastchk > 2:
            lastchk = now
            if now - started > 20 and not chiaki_alive():
                log('Chiaki chiuso: rilascio gli encoder'); stop()
        r, _, _ = select.select(list(devs), [], [], 1.0)
        for fd in r:
            try: data = os.read(fd, sz * 64)
            except BlockingIOError: continue
            except Exception: continue
            ph = devs[fd]; dirty = False
            for i in range(0, len(data) - sz + 1, sz):
                _, _, t, code, val = struct.unpack(fmt, data[i:i + sz])
                if t == EV_KEY:
                    tg = bmap.get((ph, code))
                    if not tg: continue
                    if tg in BTN: emit(EV_KEY, BTN[tg], 1 if val else 0); dirty = True
                    if tg in TRIG: emit(EV_ABS, TRIG[tg], 255 if val else 0); dirty = True
                elif t == EV_ABS:
                    a = amap.get((ph, code))
                    if not a: continue
                    target, idx = a
                    mn, mx = absinfo.get((ph, code), (0, 255))
                    mid = (mn + mx) / 2.0; span = (mx - mn) / 2.0 or 1
                    n = (val - mid) / span          # -1 .. 1
                    if abs(n) < 0.3: n = 0
                    out = STICK[target][idx]
                    if target == 'dpad': emit(EV_ABS, out, -1 if n < 0 else (1 if n > 0 else 0))
                    else: emit(EV_ABS, out, int(max(-1, min(1, n)) * 32767))
                    dirty = True
            if dirty: emit(EV_SYN, 0, 0)

if __name__ == '__main__':
    sys.exit(main() or 0)
