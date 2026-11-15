#!/usr/bin/env python3
# Batocera Companion - plancia -> tastiera per emulatori Wine (Model 2/3)
import os, struct, select, fcntl, glob, time, json, urllib.request, xml.etree.ElementTree as ET
EVIOCGNAME = 0x81004506
UI_SET_EVBIT=0x40045564; UI_SET_KEYBIT=0x40045565; UI_DEV_SETUP=0x405c5503; UI_DEV_CREATE=0x5501; UI_DEV_DESTROY=0x5502
fmt='llHHi'; sz=struct.calcsize(fmt)
SYSTEMS = {'model2', 'model3', 'supermodel'}
K = {'1':2,'2':3,'3':4,'4':5,'5':6,'6':7,'q':16,'w':17,'e':18,'a':30,'s':31,'d':32,'z':44,'x':45,'c':46,'up':103,'left':105,'right':106,'down':108,'lctrl':29,'lalt':56,'space':57,'enter':28}
# tasti di default del Model 2 Emulator (tastiera): P1 1/5, frecce, P2 2/6
MAP = {
  0: {'start':'1','select':'5','hotkey':'5','up':'up','down':'down','left':'left','right':'right',
      'joystick1up':'up','joystick1down':'down','joystick1left':'left','joystick1right':'right',
      'a':'up','b':'down','x':'z','y':'x','pageup':'c','pagedown':'a'},
  1: {'start':'2','select':'6','hotkey':'6'},
}
# ---- tastiera virtuale
ufd=os.open('/dev/uinput', os.O_WRONLY|os.O_NONBLOCK)
fcntl.ioctl(ufd, UI_SET_EVBIT, 1); fcntl.ioctl(ufd, UI_SET_EVBIT, 0)
for k in range(1,256): fcntl.ioctl(ufd, UI_SET_KEYBIT, k)
fcntl.ioctl(ufd, UI_DEV_SETUP, struct.pack('<HHHH80sI', 0x03, 0x1d6b, 0x0105, 1, b'Batocera Companion Pad Keys', 0)); fcntl.ioctl(ufd, UI_DEV_CREATE)
def emit(code, val):
    os.write(ufd, struct.pack('<qqHHi', 0, 0, 1, code, val)); os.write(ufd, struct.pack('<qqHHi', 0, 0, 0, 0, 0))
# ---- configurazione controller di EmulationStation: nome dispositivo -> {input: (tipo,id,valore)}
def es_config():
    cfg = {}
    try:
        root = ET.parse('/userdata/system/configs/emulationstation/es_input.cfg').getroot()
        for ic in root.findall('inputConfig'):
            name = (ic.get('deviceName') or '').strip()
            m = {}
            for i in ic.findall('input'):
                m[i.get('name')] = (i.get('type'), int(i.get('id')), int(i.get('value')))
            cfg[name] = m
    except Exception: pass
    return cfg
def running_system():
    try:
        b = urllib.request.urlopen('http://127.0.0.1:1234/runningGame', timeout=1).read()
        if not b.strip(): return ''
        return (json.loads(b).get('systemName') or '').lower()
    except Exception: return ''
def player_paths():
    # ordine giocatori deciso da EmulationStation (InputP1Path, InputP2Path... in es_settings.cfg)
    out = {}
    try:
        root = ET.parse('/userdata/system/configs/emulationstation/es_settings.cfg').getroot()
        for e in root.findall('string'):
            n = e.get('name') or ''
            if n.startswith('InputP') and n.endswith('Path') and e.get('value'):
                try: out[e.get('value')] = int(n[6:-4]) - 1
                except ValueError: pass
    except Exception: pass
    return out
EVIOCGPHYS = 0x81004507
def opendevs():
    cfg = es_config(); pp = player_paths(); devs = []
    conf = {}
    try: conf = json.load(open('/userdata/system/companion/wine-keys.json'))
    except Exception: pass
    p1 = conf.get('p1_phys') or ''
    for p in sorted(glob.glob('/dev/input/event*'), key=lambda s: int(s[16:])):
        try:
            fd = os.open(p, os.O_RDONLY|os.O_NONBLOCK)
            n = bytearray(256); fcntl.ioctl(fd, EVIOCGNAME, n); n = n.split(b'\0',1)[0].decode(errors='ignore').strip()
            ph = bytearray(256)
            try: fcntl.ioctl(fd, EVIOCGPHYS, ph); ph = ph.split(b'\0',1)[0].decode(errors='ignore')
            except Exception: ph = ''
        except Exception: continue
        if n not in cfg or not cfg[n]:
            os.close(fd); continue
        # ordine: il dispositivo rilevato come P1 (porta USB), poi quelli assegnati da ES, poi per numero
        rank = 0 if (p1 and ph == p1) else 1 + pp.get(p, 99)
        devs.append([fd, n, cfg[n], rank])
    devs.sort(key=lambda d: d[3])
    if conf.get('swap'): devs.reverse()
    return [(d[0], d[1], d[2]) for d in devs]
active = False; devs = []; pressed = {}; last_scan = 0; last_check = 0
def release_all():
    for k in list(pressed):
        emit(pressed.pop(k), 0)
while True:
    now = time.time()
    if now - last_check > 1.5:
        last_check = now
        on = running_system() in SYSTEMS
        if on != active:
            active = on
            if active:
                for fd,_,_ in devs: os.close(fd)
                devs = opendevs(); last_scan = now
            else:
                release_all()
                for fd,_,_ in devs: os.close(fd)
                devs = []
    if not active:
        time.sleep(0.3); continue
    r,_,_ = select.select([d[0] for d in devs], [], [], 0.1)
    for fd in r:
        try: data = os.read(fd, sz*64)
        except OSError: continue
        pi = [d[0] for d in devs].index(fd); name, inputs = devs[pi][1], devs[pi][2]
        amap = MAP.get(pi, {})
        for i in range(0, len(data)-sz+1, sz):
            _,_,t,code,val = struct.unpack(fmt, data[i:i+sz])
            if t == 1:   # pulsante
                for iname,(ity,iid,ival) in inputs.items():
                    if ity == 'button' and 288+iid == code:
                        key = amap.get(iname)
                        if key:
                            kc = K[key]; tag = (pi, iname)
                            if val == 1: emit(kc, 1); pressed[tag] = kc
                            elif val == 0 and tag in pressed: emit(pressed.pop(tag), 0)
            elif t == 3: # asse (stick digitale dello zero delay: 0 / 127 / 255)
                for iname,(ity,iid,ival) in inputs.items():
                    if ity == 'axis' and iid == code:
                        key = amap.get(iname)
                        if not key: continue
                        kc = K[key]; tag = (pi, iname)
                        on = (val < 100) if ival < 0 else (val > 155)
                        if on and tag not in pressed: emit(kc, 1); pressed[tag] = kc
                        elif not on and tag in pressed: emit(pressed.pop(tag), 0)
        # hotkey + start = esci dal gioco
        if (0,'hotkey') in pressed and (0,'start') in pressed:
            release_all()
            try: urllib.request.urlopen('http://127.0.0.1:1234/emukill', timeout=2).read()
            except Exception: pass
