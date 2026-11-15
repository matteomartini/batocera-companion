set -e
export TERM=dumb
arch=$(uname -m)
case "$arch" in x86_64) asset=chiaki-ng.AppImage_x86_64;; aarch64) asset=chiaki-ng.AppImage_arm64;; *) echo "architettura non supportata: $arch"; exit 1;; esac
D=/userdata/system/add-ons/chiaki
mkdir -p "$D/extra" /userdata/system/logs /userdata/roms/ports/images /userdata/system/configs/chiaki
URL="https://github.com/streetpea/chiaki-ng/releases/latest/download/$asset"
echo "Scarico $URL"
rm -f "$D/Chiaki.AppImage.part"
if ! curl -fL --retry 3 --connect-timeout 20 -o "$D/Chiaki.AppImage.part" "$URL"; then
  echo "download diretto fallito, provo tramite API GitHub..."
  TAG=$(curl -fsL https://api.github.com/repos/streetpea/chiaki-ng/releases/latest | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)
  [ -n "$TAG" ] || { echo "ERRORE: GitHub non risponde (rate limit o rete). Riprova tra qualche minuto."; exit 1; }
  curl -fL --retry 3 -o "$D/Chiaki.AppImage.part" "https://github.com/streetpea/chiaki-ng/releases/download/$TAG/$asset"
fi
SZ=$(stat -c %s "$D/Chiaki.AppImage.part")
[ "$SZ" -gt 10000000 ] || { echo "ERRORE: file scaricato troppo piccolo ($SZ byte)"; exit 1; }
mv -f "$D/Chiaki.AppImage.part" "$D/Chiaki.AppImage"; chmod a+x "$D/Chiaki.AppImage"
echo "AppImage ok ($((SZ/1048576)) MB)"
curl -fsL -o "$D/extra/icon.png" https://github.com/batocera-unofficial-addons/batocera-unofficial-addons/raw/main/chiaki/extra/icon.png || true
curl -fsL -o /userdata/roms/ports/images/chiakilogo.png https://github.com/batocera-unofficial-addons/batocera-unofficial-addons/raw/main/chiaki/extra/chiakilogo.png || true
cat > /userdata/roms/ports/Chiaki.sh <<'LAUNCH'
#!/bin/bash
export $(cat /proc/1/environ | tr '\0' '\n')
export DISPLAY=:0.0
export QTWEBENGINE_DISABLE_SANDBOX=1
export QT_QPA_PLATFORM=xcb
app_dir="/userdata/system/add-ons/chiaki"
log="/userdata/system/logs/chiaki.log"
mkdir -p /userdata/system/logs
echo "$(date): avvio Chiaki" >> "$log"
cd "$app_dir" && ./Chiaki.AppImage >> "$log" 2>&1
LAUNCH
chmod +x /userdata/roms/ports/Chiaki.sh
cat > /userdata/system/configs/chiaki/chiaki.desktop <<'DESK'
[Desktop Entry]
Version=1.0
Type=Application
Name=Chiaki-NG
Exec=/userdata/roms/ports/Chiaki.sh
Icon=/userdata/system/add-ons/chiaki/extra/icon.png
Terminal=false
Categories=Game;batocera.linux;
DESK
cp -f /userdata/system/configs/chiaki/chiaki.desktop /usr/share/applications/chiaki.desktop 2>/dev/null || true
cat > /userdata/system/configs/chiaki/restore_desktop_entry.sh <<'RST'
#!/bin/bash
[ -f /usr/share/applications/chiaki.desktop ] || cp /userdata/system/configs/chiaki/chiaki.desktop /usr/share/applications/chiaki.desktop
RST
chmod +x /userdata/system/configs/chiaki/restore_desktop_entry.sh
touch /userdata/system/custom.sh
grep -q configs/chiaki/restore_desktop_entry.sh /userdata/system/custom.sh || echo 'bash /userdata/system/configs/chiaki/restore_desktop_entry.sh &' >> /userdata/system/custom.sh
chmod +x /userdata/system/custom.sh
python3 - <<'PY'
import os, xml.etree.ElementTree as ET
p='/userdata/roms/ports/gamelist.xml'
try: tree=ET.parse(p); root=tree.getroot()
except Exception: root=ET.Element('gameList'); tree=ET.ElementTree(root)
for g in root.findall('game'):
    if (g.findtext('path') or '').strip() in ('./Chiaki.sh','Chiaki.sh'):
        root.remove(g)
g=ET.SubElement(root,'game')
for k,v in (('path','./Chiaki.sh'),('name','Chiaki (PS4/PS5 Remote Play)'),('image','./images/chiakilogo.png'),('desc','Remote Play PlayStation tramite Chiaki-ng. Prima volta: associa la console dal menu di Chiaki.')):
    ET.SubElement(g,k).text=v
tree.write(p, encoding='utf-8', xml_declaration=True)
print('gamelist aggiornata')
PY
echo "Chiaki installato. Lo trovi in Ports."
