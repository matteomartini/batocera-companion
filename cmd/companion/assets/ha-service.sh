#!/bin/bash
# Servizio Batocera: agente Home Assistant (HA-Batocera) — installato da Batocera Companion
case "$1" in
  start)
    [ -x /userdata/system/homeassistant/homeassistant.sh ] || exit 0
    # aspetta la rete (max 60 s) prima di pubblicare
    for i in $(seq 1 30); do { ! command -v ip >/dev/null || ip route 2>/dev/null | grep -q default; } && break; sleep 2; done
    setsid nohup /userdata/system/homeassistant/homeassistant.sh start >/dev/null 2>&1 </dev/null &
    ;;
  stop)
    /userdata/system/homeassistant/homeassistant.sh stop >/dev/null 2>&1
    pkill -f "[h]omeassistant.sh start" 2>/dev/null
    pkill -f "mosquitto_sub.*command/#" 2>/dev/null
    ;;
  restart)
    "$0" stop; sleep 2; "$0" start
    ;;
  status)
    pgrep -f "[h]omeassistant.sh start" >/dev/null && echo running || echo stopped
    ;;
esac
exit 0
