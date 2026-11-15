#!/bin/bash
# Finto cabinato Batocera in questo container (per i test del Companion).
set -e
mkdir -p /userdata/roms/{fbneo,mame,neogeo,nes,snes,megadrive,psx,ports,steam} /userdata/system/configs/emulationstation /userdata/bios /userdata/saves /userdata/splash /userdata/themes /userdata/recordings /usr/share/batocera /usr/share/emulationstation /usr/lib/libretro /media /userdata/system/services /userdata/system/logs
echo "43.1 2025/09/01 12:00" > /usr/share/batocera/batocera.version
echo "x86_64" > /usr/share/batocera/batocera.arch
for c in fbneo mame mame2003_plus mame2010 mame2015 snes9x genesis_plus_gx pcsx_rearmed nestopia swanstation; do touch /usr/lib/libretro/${c}_libretro.so; done
cat > /userdata/system/batocera.conf <<'C'
# batocera.conf di test
global.videomode=auto
global.ratio=auto
global.smooth=1
global.rewind=1
audio.bgmusic=1
system.timezone=Europe/Rome
mame["oldgame.zip"].core=mame2016
C
cat > /userdata/system/configs/emulationstation/es_settings.cfg <<'C'
<?xml version="1.0"?>
<config>
	<string name="ThemeSet" value="es-theme-carbon" />
	<string name="UIMode" value="Full" />
	<string name="ScreenSaverBehavior" value="dim" />
	<bool name="VolumePopup" value="true" />
</config>
C
cat > /userdata/system/configs/emulationstation/es_input.cfg <<'C'
<?xml version="1.0"?>
<inputList>
	<inputConfig type="keyboard" deviceName="Keyboard" deviceGUID="-1">
		<input name="a" type="key" id="13" value="1" />
		<input name="b" type="key" id="8" value="1" />
		<input name="x" type="key" id="120" value="1" />
		<input name="y" type="key" id="121" value="1" />
		<input name="start" type="key" id="32" value="1" />
		<input name="select" type="key" id="115" value="1" />
		<input name="up" type="key" id="1073741906" value="1" />
		<input name="down" type="key" id="1073741905" value="1" />
		<input name="left" type="key" id="1073741904" value="1" />
		<input name="right" type="key" id="1073741903" value="1" />
	</inputConfig>
	<inputConfig type="joystick" deviceName="DragonRise Inc.   Generic   USB  Joystick  " deviceGUID="03000000790000000600000010010000">
		<input name="a" type="button" id="0" value="1" />
		<input name="b" type="button" id="1" value="1" />
		<input name="x" type="button" id="2" value="1" />
		<input name="y" type="button" id="3" value="1" />
		<input name="pageup" type="button" id="4" value="1" />
		<input name="pagedown" type="button" id="5" value="1" />
		<input name="select" type="button" id="8" value="1" />
		<input name="start" type="button" id="9" value="1" />
		<input name="hotkey" type="button" id="8" value="1" />
		<input name="up" type="hat" id="0" value="1" />
		<input name="down" type="hat" id="0" value="4" />
		<input name="left" type="hat" id="0" value="8" />
		<input name="right" type="hat" id="0" value="2" />
	</inputConfig>
</inputList>
C
cat > /usr/share/emulationstation/es_systems.cfg <<'C'
<?xml version="1.0"?>
<systemList>
  <system><name>fbneo</name><fullname>FinalBurn Neo</fullname><path>/userdata/roms/fbneo</path><extension>.zip .7z</extension><emulators><emulator name="libretro"><cores><core default="true">fbneo</core></cores></emulator></emulators></system>
  <system><name>mame</name><fullname>MAME</fullname><path>/userdata/roms/mame</path><extension>.zip .7z</extension><emulators><emulator name="mame"><cores><core default="true">mame</core></cores></emulator><emulator name="libretro"><cores><core>mame2003_plus</core><core>mame2010</core><core>mame2015</core></cores></emulator></emulators></system>
  <system><name>neogeo</name><fullname>Neo-Geo</fullname><path>/userdata/roms/neogeo</path><extension>.zip .7z</extension><emulators><emulator name="libretro"><cores><core default="true">fbneo</core></cores></emulator></emulators></system>
  <system><name>nes</name><fullname>NES</fullname><path>/userdata/roms/nes</path><extension>.nes .zip .7z .unh .fds</extension><emulators><emulator name="libretro"><cores><core default="true">nestopia</core><core>fceumm</core></cores></emulator></emulators></system>
  <system><name>snes</name><fullname>Super Nintendo</fullname><path>/userdata/roms/snes</path><extension>.sfc .smc .zip .7z</extension><emulators><emulator name="libretro"><cores><core default="true">snes9x</core></cores></emulator></emulators></system>
  <system><name>megadrive</name><fullname>Mega Drive</fullname><path>/userdata/roms/megadrive</path><extension>.md .bin .gen .zip .7z</extension><emulators><emulator name="libretro"><cores><core default="true">genesis_plus_gx</core></cores></emulator></emulators></system>
  <system><name>psx</name><fullname>PlayStation</fullname><path>/userdata/roms/psx</path><extension>.cue .chd .pbp .iso .m3u</extension><emulators><emulator name="libretro"><cores><core default="true">swanstation</core><core>pcsx_rearmed</core></cores></emulator></emulators></system>
  <system><name>ports</name><fullname>Ports</fullname><path>/userdata/roms/ports</path><extension>.sh</extension><emulators><emulator name="sh"><cores><core default="true">sh</core></cores></emulator></emulators></system>
  <system><name>steam</name><fullname>Steam</fullname><path>/userdata/roms/steam</path><extension>.steam</extension><emulators><emulator name="steam"><cores><core default="true">steam</core></cores></emulator></emulators></system>
  <system><name>prboom</name><fullname>Doom</fullname><path>/userdata/roms/prboom</path><extension>.wad</extension><emulators><emulator name="libretro"><cores><core default="true">prboom</core></cores></emulator></emulators></system>
</systemList>
C
# comandi finti
mkdir -p /usr/local/bin
cat > /usr/local/bin/batocera-settings-get <<'C'
#!/bin/bash
grep -m1 "^$1=" /userdata/system/batocera.conf 2>/dev/null | cut -d= -f2-
C
cat > /usr/local/bin/batocera-settings-set <<'C'
#!/bin/bash
k="$1"; v="$2"; f=/userdata/system/batocera.conf
esc=$(printf '%s' "$k" | sed 's/[][\.*^$/]/\\&/g')
if grep -q "^$esc=" "$f"; then sed -i "s|^$esc=.*|$k=$v|" "$f"; else echo "$k=$v" >> "$f"; fi
C
cat > /usr/local/bin/batocera-es-swissknife <<'C'
#!/bin/bash
echo "swissknife $*" >> /tmp/mock_swissknife.log
C
cat > /usr/local/bin/batocera-systems <<'C'
#!/bin/bash
echo "> neogeo"; echo "MISSING bios/neogeo.zip Neo Geo BIOS"; echo "> psx"; echo "INVALID bios/scph5501.bin"; echo "> nes"; echo "OK bios/disksys.rom"
C
cat > /usr/local/bin/batocera-services <<'C'
#!/bin/bash
echo "services $*" >> /tmp/mock_services.log; [ "$1" = list ] && echo "* companion"; exit 0
C
cat > /usr/local/bin/batocera-resolution <<'C'
#!/bin/bash
echo 1280x960
C
cat > /usr/local/bin/batocera-audio <<'C'
#!/bin/bash
echo 50
C
cat > /usr/local/bin/batocera-es-theme <<'C'
#!/bin/bash
[ "$1" = list ] && { echo "es-theme-carbon [installed] https://github.com/batocera-linux/es-theme-carbon"; echo "es-theme-art-book-next https://github.com/anthonycaccese/es-theme-art-book-next"; }
exit 0
C
chmod +x /usr/local/bin/batocera-*
# ROM di prova
python3 - <<'PY'
import zipfile, os, random
random.seed(1)
# fbneo set finto non nel DAT
with zipfile.ZipFile('/userdata/roms/fbneo/notindat.zip','w') as z: z.writestr('a.bin', os.urandom(100))
open('/userdata/roms/nes/Test Game (USA).nes','wb').write(b'NES\x1a'+os.urandom(16396))
open('/userdata/roms/snes/Another (Europe) (Disc 1).sfc','wb').write(os.urandom(2048))
open('/userdata/roms/psx/Game (Disc 1).cue','w').write('FILE "Game (Disc 1).bin" BINARY\n  TRACK 01 MODE2/2352\n    INDEX 01 00:00:00\n')
open('/userdata/roms/psx/Game (Disc 1).bin','wb').write(os.urandom(4096))
open('/userdata/roms/psx/Game (Disc 2).cue','w').write('FILE "Game (Disc 2).bin" BINARY\n')
open('/userdata/roms/psx/Game (Disc 2).bin','wb').write(os.urandom(4096))
PY
for s in fbneo nes snes psx megadrive mame neogeo; do
python3 - "$s" <<'PY'
import sys, os, xml.etree.ElementTree as ET
s=sys.argv[1]; d='/userdata/roms/'+s
root=ET.Element('gameList')
for f in sorted(os.listdir(d)):
    if f.endswith(('.zip','.nes','.sfc','.cue','.md','.bin')) and not f.endswith('.bin'):
        g=ET.SubElement(root,'game'); ET.SubElement(g,'path').text='./'+f; ET.SubElement(g,'name').text=os.path.splitext(f)[0]
ET.ElementTree(root).write(d+'/gamelist.xml', encoding='utf-8', xml_declaration=True)
PY
done
echo mock-ok
