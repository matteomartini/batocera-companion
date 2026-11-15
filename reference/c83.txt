#!/usr/bin/env python3
# Batocera Companion - inventario della cartella roms: file, dimensioni, CRC32 (con header
# saltato dove serve), contenuto degli zip, override per gioco in batocera.conf, core installati.
import os, sys, json, zlib, zipfile, re, glob

ROOT = sys.argv[1] if len(sys.argv) > 1 else "/userdata/roms"
MAX_CRC = 256 * 1024 * 1024   # oltre questa dimensione (immagini CD/DVD) niente CRC: si va per nome
SKIP_DIRS = {"images", "videos", "manuals", "media", "music", "downloaded_images", "downloaded_videos", "downloaded_media", "downloaded_manuals", "downloaded_marquees", "downloaded_wheels", "magazines", "mixrbv2", "snap", "titles", "wheel", "marquee", "fanart", "boxart", "box3d", "bezels", "screenshots", "map", "backcover", "manual", "thumbnails"}
SKIP_FILES = {"gamelist.xml", "_info.txt", "gamelist.xml.old", "gamelist.xml.bak", "readme.txt", "systeminfo.txt"}

def log(*a):
    print(*a, file=sys.stderr, flush=True)

def header_skip(sys_name, size, head):
    if sys_name == "nes" and head[:4] == b"NES\x1a": return 16
    if sys_name == "fds" and head[:4] == b"FDS\x1a": return 16
    if sys_name == "snes" and size % 1024 == 512: return 512
    if sys_name == "lynx" and head[:4] == b"LYNX": return 64
    if sys_name == "atari7800" and b"ATARI7800" in head[:10]: return 128
    return 0

def crc_file(path, sys_name, size):
    if size > MAX_CRC or size == 0: return None
    c = 0
    with open(path, "rb") as f:
        head = f.read(16)
        skip = header_skip(sys_name, size, head)
        f.seek(skip)
        while True:
            b = f.read(1 << 20)
            if not b: break
            c = zlib.crc32(b, c)
    return c & 0xffffffff

out = {"systems": {}, "overrides": [], "cores": [], "m3u": {}}
try:
    out["cores"] = sorted(os.path.basename(p)[:-len("_libretro.so")] for p in glob.glob("/usr/lib/libretro/*_libretro.so"))
except Exception: pass

# override per gioco in batocera.conf:  psx["Gioco.chd"].core=xyz
try:
    rx = re.compile(r'^([a-z0-9_]+)\["([^"]+)"\]\.(core|emulator)=(.*)$')
    for l in open("/userdata/system/batocera.conf", encoding="utf-8", errors="ignore"):
        m = rx.match(l.strip())
        if m: out["overrides"].append({"system": m.group(1), "file": m.group(2), "key": m.group(3), "value": m.group(4).strip()})
except Exception: pass

nsys = 0
for sysdir in sorted(os.listdir(ROOT)):
    sp = os.path.join(ROOT, sysdir)
    if not os.path.isdir(sp) or sysdir.startswith("."): continue
    files = []
    for dp, dn, fn in os.walk(sp):
        rel = os.path.relpath(dp, sp)
        depth = 0 if rel == "." else rel.count(os.sep) + 1
        dn[:] = [d for d in dn if d.lower() not in SKIP_DIRS and not d.startswith(".") and depth < 3]
        for f in fn:
            if f.startswith(".") or f.lower() in SKIP_FILES: continue
            p = os.path.join(dp, f)
            try: size = os.path.getsize(p)
            except OSError: continue
            ext = f.rsplit(".", 1)[-1].lower() if "." in f else ""
            item = {"path": os.path.relpath(p, sp), "size": size, "ext": ext}
            if ext == "m3u":
                try: out["m3u"][sysdir + "/" + item["path"]] = [l.strip() for l in open(p, encoding="utf-8", errors="ignore") if l.strip()]
                except Exception: pass
            elif ext == "zip" and size < 2 * 1024 * 1024 * 1024:
                try:
                    with zipfile.ZipFile(p) as z:
                        item["entries"] = [[i.filename, i.CRC & 0xffffffff, i.file_size] for i in z.infolist() if not i.is_dir()]
                except Exception as e:
                    item["bad"] = "zip non leggibile"
            elif ext in ("7z", "rar", "chd", "pbp", "cso", "rvz", "wbfs", "gcz", "wua", "wud", "nsp", "xci", "cue", "ccd", "sub", "gdi", "cdi", "m3u"):
                pass  # dischi/archivi: si riconoscono per nome
            elif size <= 64 * 1024 * 1024 or (ext in ("nds", "3ds", "gba", "n64", "z64", "v64", "sfc", "smc") and size <= MAX_CRC):
                try: item["crc"] = crc_file(p, sysdir, size)
                except Exception: item["crc"] = None
            files.append(item)
    out["systems"][sysdir] = files
    nsys += 1
    log("%s: %d file" % (sysdir, len(files)))

print(json.dumps(out))
