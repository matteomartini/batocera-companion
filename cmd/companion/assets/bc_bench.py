#!/usr/bin/python3
# Benchmark emulatore (Batocera Companion): rilegge dal log di Batocera il comando
# dell'ultimo gioco avviato e lo rilancia in modalità benchmark.
#   RetroArch: --max-frames con vsync/audio sync spenti → velocità rispetto al tempo reale
#   MAME standalone: -bench N → "Average speed: X%"
import ast, json, os, re, subprocess, sys, time
frames = int(sys.argv[1]); secs = int(sys.argv[2])
try:
    txt = open('/userdata/system/logs/es_launch_stdout.log', errors='ignore').read()
except Exception as e:
    print(json.dumps({"error": "log di Batocera non leggibile: %s" % e})); sys.exit(0)
argv = None; env = {}
for line in txt.splitlines():
    m = re.search(r'command: (.*)$', line)
    if not m: continue
    v = m.group(1).strip()
    if v.startswith('['):
        try: argv = ast.literal_eval(v); env_for = env
        except Exception: pass
    elif v.startswith('{'):
        try: env = {str(k): str(x) for k, x in ast.literal_eval(v).items()}
        except Exception: pass
    elif re.match(r'^[A-Za-z_][A-Za-z0-9_]*=', v):
        env = {}
        for tok in v.split(' '):
            if '=' in tok:
                k, _, val = tok.partition('='); env[k] = val
if not argv:
    print(json.dumps({"error": "comando dell'emulatore non trovato nel log di Batocera"})); sys.exit(0)
e = dict(os.environ); e.update(env); e.setdefault('DISPLAY', ':0'); e.setdefault('HOME', '/userdata/system')
exe = os.path.basename(str(argv[0]))

def run(a, tmo):
    t = time.time()
    try:
        p = subprocess.run([str(x) for x in a], env=e, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=tmo)
        return time.time() - t, p.stdout.decode(errors='ignore'), p.returncode
    except subprocess.TimeoutExpired:
        return time.time() - t, 'timeout', -1

if 'retroarch' in exe:
    open('/tmp/bc_bench.cfg', 'w').write(
        'video_vsync = "false"\naudio_sync = "false"\naudio_enable = "false"\nfastforward_ratio = "0.000000"\n'
        'video_frame_delay = "0"\nvideo_frame_delay_auto = "false"\nvideo_black_frame_insertion = "0"\n'
        'menu_enable_widgets = "false"\nvideo_font_enable = "false"\nnotification_show_autoconfig = "false"\n')
    base = [str(x) for x in argv]
    for i, a in enumerate(base):
        if a == '--appendconfig' and i + 1 < len(base):
            base[i + 1] = base[i + 1] + '|/tmp/bc_bench.cfg'; break
        if a.startswith('--appendconfig='):
            base[i] = a + '|/tmp/bc_bench.cfg'; break
    else:
        base += ['--appendconfig', '/tmp/bc_bench.cfg']
    warm = 600
    t1, o1, rc1 = run(base + ['--max-frames=%d' % warm], secs * 6 + 90)
    if rc1 not in (0, 1) and 'timeout' in o1:
        print(json.dumps({"error": "RetroArch non è uscito (timeout)", "out": o1[-600:]})); sys.exit(0)
    t2, o2, rc2 = run(base + ['--max-frames=%d' % (warm + frames)], secs * 6 + 90)
    dt = t2 - t1
    if dt <= 0.05:
        print(json.dumps({"error": "tempi non misurabili (RetroArch è uscito subito?)", "out": o2[-600:]})); sys.exit(0)
    speed = (frames / 60.0) / dt
    print(json.dumps({"mode": "retroarch", "speed": round(speed, 2), "fps": round(frames / dt, 1), "secs": round(dt, 1),
                      "load_secs": round(max(0.0, t1 - (warm / 60.0) / max(speed, 0.01)), 1), "rc": rc2, "cmd": " ".join(base)}))
elif 'mame' in exe:
    a = [str(x) for x in argv] + ['-bench', str(secs)]
    t, o, rc = run(a, secs * 3 + 90)
    m = re.search(r'Average speed:\s*([\d.]+)%', o)
    if not m:
        print(json.dumps({"error": "MAME non ha stampato la velocità media", "out": o[-600:]})); sys.exit(0)
    print(json.dumps({"mode": "mame", "speed": round(float(m.group(1)) / 100, 2), "secs": secs, "rc": rc, "cmd": " ".join(a)}))
else:
    print(json.dumps({"mode": "none", "exe": exe}))
