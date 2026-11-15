#!/usr/bin/env python3
import os, struct, select, fcntl, glob, sys, json, time
EVIOCGNAME = 0x81004506; EVIOCGPHYS = 0x81004507
fmt='llHHi'; sz=struct.calcsize(fmt)
fds = {}
for p in glob.glob('/dev/input/event*'):
    try:
        fd = os.open(p, os.O_RDONLY|os.O_NONBLOCK)
        n = bytearray(256); fcntl.ioctl(fd, EVIOCGNAME, n); n = n.split(b'\0',1)[0].decode(errors='ignore').strip()
        ph = bytearray(256); fcntl.ioctl(fd, EVIOCGPHYS, ph); ph = ph.split(b'\0',1)[0].decode(errors='ignore')
    except Exception: continue
    if 'keyboard' in n.lower() or 'mouse' in n.lower() or 'evmapy' in n.lower() or 'hotkeys' in n.lower() or 'button' in n.lower() or 'HDA' in n or 'Video' in n:
        os.close(fd); continue
    fds[fd] = (p, n, ph)
end = time.time() + float(sys.argv[1]) if len(sys.argv) > 1 else time.time() + 60
while time.time() < end:
    r,_,_ = select.select(list(fds), [], [], 0.2)
    for fd in r:
        try: data = os.read(fd, sz*64)
        except OSError: continue
        for i in range(0, len(data)-sz+1, sz):
            _,_,t,code,val = struct.unpack(fmt, data[i:i+sz])
            if (t == 1 and val == 1) or (t == 3 and code < 2 and val not in (126, 127, 128)):
                p, n, ph = fds[fd]
                print(json.dumps({'device': os.path.basename(p), 'name': n, 'phys': ph})); sys.exit(0)
print(json.dumps({}))
