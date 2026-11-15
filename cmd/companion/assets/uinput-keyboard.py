import os, sys, struct, fcntl, time
UI_SET_EVBIT=0x40045564; UI_SET_KEYBIT=0x40045565; UI_DEV_SETUP=0x405c5503; UI_DEV_CREATE=0x5501; UI_DEV_DESTROY=0x5502
EV_SYN=0; EV_KEY=1
fd=os.open('/dev/uinput', os.O_WRONLY|os.O_NONBLOCK)
fcntl.ioctl(fd, UI_SET_EVBIT, EV_KEY); fcntl.ioctl(fd, UI_SET_EVBIT, EV_SYN)
for k in range(1,256): fcntl.ioctl(fd, UI_SET_KEYBIT, k)
setup=struct.pack('<HHHH80sI', 0x03, 0x1d6b, 0x0104, 1, b'Batocera Companion Keyboard', 0)
fcntl.ioctl(fd, UI_DEV_SETUP, setup); fcntl.ioctl(fd, UI_DEV_CREATE)
def emit(t,c,v): os.write(fd, struct.pack('<qqHHi', 0, 0, t, c, v))
print('READY', flush=True)
try:
    for line in sys.stdin:
        p=line.split()
        if len(p)!=2: continue
        try: code=int(p[0]); val=int(p[1])
        except ValueError: continue
        emit(EV_KEY, code, val); emit(EV_SYN, 0, 0)
finally:
    fcntl.ioctl(fd, UI_DEV_DESTROY); os.close(fd)
