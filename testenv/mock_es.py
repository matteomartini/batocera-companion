#!/usr/bin/env python3
# Finto server HTTP di EmulationStation (porta 1234) che legge i gamelist.xml in /userdata/roms.
import json, os, re, hashlib, xml.etree.ElementTree as ET
from http.server import BaseHTTPRequestHandler, HTTPServer
from urllib.parse import unquote

ROMS = '/userdata/roms'
STATE = {'running': None, 'reloads': 0, 'log': []}
NAMES = {'fbneo': 'FinalBurn Neo', 'mame': 'MAME', 'neogeo': 'Neo-Geo', 'nes': 'NES', 'snes': 'Super Nintendo', 'megadrive': 'Mega Drive', 'psx': 'PlayStation', 'ports': 'Ports', 'steam': 'Steam', 'prboom': 'Doom'}

def gid(sys_, path): return hashlib.md5((sys_ + path).encode()).hexdigest()[:12]

def games(sys_):
    d = os.path.join(ROMS, sys_); gl = os.path.join(d, 'gamelist.xml')
    out = []
    if not os.path.exists(gl): return out
    root = ET.parse(gl).getroot()
    for g in root.findall('game'):
        p = (g.findtext('path') or '').strip()
        full = os.path.normpath(os.path.join(d, p))
        item = {'id': gid(sys_, full), 'path': full, 'name': g.findtext('name') or os.path.basename(p), 'system': sys_, 'systemName': sys_}
        for c in g:
            if c.tag not in ('path', 'name'): item[c.tag] = c.text or ''
        out.append(item)
    return out

class H(BaseHTTPRequestHandler):
    def log_message(self, *a): pass
    def send(self, code, body, ct='application/json'):
        b = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.send_response(code); self.send_header('Content-Type', ct); self.send_header('Content-Length', str(len(b))); self.end_headers(); self.wfile.write(b)
    def do_GET(self):
        p = unquote(self.path.split('?')[0])
        if p == '/caps': return self.send(200, {'caps': ['systems']})
        if p == '/systems':
            out = []
            for s in sorted(os.listdir(ROMS)):
                if os.path.isdir(os.path.join(ROMS, s)):
                    out.append({'name': s, 'fullname': NAMES.get(s, s), 'totalGames': len(games(s)), 'visible': True})
            return self.send(200, out)
        m = re.match(r'^/systems/([^/]+)/games$', p)
        if m: return self.send(200, games(m.group(1)))
        m = re.match(r'^/systems/([^/]+)/games/([^/]+)$', p)
        if m:
            for g in games(m.group(1)):
                if g['id'] == m.group(2): return self.send(200, g)
            return self.send(404, {'error': 'not found'})
        m = re.match(r'^/systems/([^/]+)/games/([^/]+)/media/(\w+)$', p)
        if m:
            for g in games(m.group(1)):
                if g['id'] == m.group(2) and g.get(m.group(3)):
                    f = os.path.normpath(os.path.join(ROMS, m.group(1), g[m.group(3)]))
                    if os.path.exists(f): return self.send(200, open(f, 'rb').read(), 'image/png')
            return self.send(404, {'error': 'no media'})
        if p == '/runningGame':
            if STATE['running']: return self.send(200, STATE['running'])
            return self.send(404, {})
        if p == '/reloadgames': STATE['reloads'] += 1; STATE['log'].append('reloadgames'); return self.send(200, {'ok': True})
        if p == '/emukill': STATE['running'] = None; return self.send(200, {'ok': True})
        if p == '/mock/state': return self.send(200, STATE)
        return self.send(404, {'error': 'unknown ' + p})
    def do_POST(self):
        p = unquote(self.path.split('?')[0]); n = int(self.headers.get('Content-Length') or 0); body = self.rfile.read(n)
        if p == '/launch':
            path = body.decode()
            for s in os.listdir(ROMS):
                for g in games(s):
                    if g['path'] == path: STATE['running'] = g; return self.send(200, {'ok': True})
            return self.send(404, {'error': 'no game'})
        if p in ('/messagebox', '/notify'): STATE['log'].append('msg:' + body.decode()); return self.send(200, {'ok': True})
        m = re.match(r'^/addgames/([^/]+)$', p)
        if m:
            s = m.group(1); gl = os.path.join(ROMS, s, 'gamelist.xml')
            root = ET.parse(gl).getroot() if os.path.exists(gl) else ET.Element('gameList')
            for it in json.loads(body):
                g = ET.SubElement(root, 'game')
                for k, v in it.items(): ET.SubElement(g, k).text = str(v)
            ET.ElementTree(root).write(gl, encoding='utf-8', xml_declaration=True)
            return self.send(200, {'ok': True})
        m = re.match(r'^/removegames/([^/]+)$', p)
        if m:
            s = m.group(1); gl = os.path.join(ROMS, s, 'gamelist.xml'); root = ET.parse(gl).getroot()
            paths = set(os.path.normpath(os.path.join(ROMS, s, x['path'])) for x in json.loads(body))
            for g in list(root.findall('game')):
                if os.path.normpath(os.path.join(ROMS, s, g.findtext('path'))) in paths: root.remove(g)
            ET.ElementTree(root).write(gl, encoding='utf-8', xml_declaration=True)
            return self.send(200, {'ok': True})
        m = re.match(r'^/systems/([^/]+)/games/([^/]+)/media/(\w+)$', p)
        if m:
            s, gid_, kind = m.groups()
            for g in games(s):
                if g['id'] == gid_:
                    d = os.path.join(ROMS, s, 'images'); os.makedirs(d, exist_ok=True)
                    stem = os.path.splitext(os.path.basename(g['path']))[0]
                    rel = './images/%s-%s.png' % (stem, kind)
                    open(os.path.join(ROMS, s, rel), 'wb').write(body)
                    gl = os.path.join(ROMS, s, 'gamelist.xml'); root = ET.parse(gl).getroot()
                    for x in root.findall('game'):
                        if os.path.normpath(os.path.join(ROMS, s, x.findtext('path'))) == g['path']:
                            el = x.find(kind)
                            if el is None: el = ET.SubElement(x, kind)
                            el.text = rel
                    ET.ElementTree(root).write(gl, encoding='utf-8', xml_declaration=True)
                    return self.send(200, {'ok': True})
            return self.send(404, {'error': 'no game'})
        return self.send(404, {'error': 'unknown ' + p})

HTTPServer(('127.0.0.1', 1234), H).serve_forever()
