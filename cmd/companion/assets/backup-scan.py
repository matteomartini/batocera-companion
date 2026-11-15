import os, sys, json
root = '/userdata'
spec = json.loads(sys.argv[1])  # {cat: {"rels": [...], "exclude": [...]}}
out = {}
for cat, sp in spec.items():
    files = []
    for rel in sp['rels']:
        base = os.path.join(root, rel)
        if not os.path.isdir(base): continue
        excl = {os.path.normpath(os.path.join(base, e)) for e in (sp.get('exclude') or [])}
        for d, dirs, fs in os.walk(base):
            dirs[:] = [x for x in dirs if os.path.normpath(os.path.join(d, x)) not in excl and not os.path.islink(os.path.join(d, x))]
            for f in fs:
                p = os.path.join(d, f)
                if os.path.islink(p) or f.endswith('.part'): continue
                try: st = os.stat(p)
                except OSError: continue
                files.append([os.path.relpath(p, root), st.st_size, round(st.st_mtime, 2)])
    out[cat] = files
json.dump(out, sys.stdout, separators=(',', ':'))
