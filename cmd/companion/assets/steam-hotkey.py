#!/usr/bin/env python3
# Mentre Steam è aperto: hotkey + start sulla plancia = chiudi Steam.
# Legge i dati grezzi USB (hidraw): restano leggibili anche quando Steam si prende la plancia.
# Encoder DragonRise: 7° byte, 0x10 = select/hotkey, 0x20 = start.
import os, glob, select, subprocess, time
fds = []
for h in glob.glob('/sys/class/hidraw/hidraw*'):
    try:
        if 'DragonRise' in open(h + '/device/uevent', errors='ignore').read():
            fds.append(os.open('/dev/' + os.path.basename(h), os.O_RDONLY | os.O_NONBLOCK))
    except OSError: pass
while fds:
    for fd in select.select(fds, [], [], 1)[0]:
        try: r = os.read(fd, 64)
        except OSError: continue
        if len(r) >= 7 and (r[6] & 0x30) == 0x30:
            env = dict(os.environ, DISPLAY=':0')
            subprocess.run(['flatpak', 'run', 'com.valvesoftware.Steam', '-shutdown'], env=env, timeout=20)
            time.sleep(10)
            subprocess.run(['pkill', '-f', 'com.valvesoftware.Steam'])
            raise SystemExit
