#!/bin/bash
# Avvio di Steam dalla sezione Steam (Batocera Companion)
# - output su file: la pipe di EmulationStation si chiude dopo 300 righe e uccide Steam
# - sveglia il servizio flatpak-portal (senza, lo Steam flatpak si rifiuta di partire)
# - hotkey+start sulla plancia chiude Steam (Batocera non lo gestisce con questo avvio)
exec > /userdata/system/logs/steam-run.log 2>&1 < /dev/null
export DISPLAY=:0
timeout 8 dbus-send --session --print-reply --dest=org.freedesktop.portal.Flatpak /org/freedesktop/portal/Flatpak org.freedesktop.DBus.Peer.Ping >/dev/null 2>&1
python3 /userdata/system/steam-hotkey.py & HK=$!
ROM="$1"
if [ "$(basename "$ROM")" = "Steam.steam" ] || [ ! -s "$ROM" ]; then batocera-steam; else batocera-steam "$(tr -d '\r\n' < "$ROM")"; fi
kill $HK 2>/dev/null
