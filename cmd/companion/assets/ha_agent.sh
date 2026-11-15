#!/bin/bash

###############################################################################

# HA-Batocera Agent (versione ridotta Batocera Companion)
# Solo: acceso/spento, gioco in corso, temperatura CPU, RAM, disco + pulsanti Riavvia/Spegni.
# Basato su HA-Batocera Agent 1.0.0 (JBSLabs)

###############################################################################

###############################################################################
# State Variables
###############################################################################

BASE_DIR="/userdata/system/homeassistant"
CONFIG_FILE="$BASE_DIR/config.conf"
LOG_FILE="$BASE_DIR/logs/homeassistant.log"

STATE_DIR="/tmp/playsession"

SERVICE_FLAG="$STATE_DIR/running"
GAME_FILE="$STATE_DIR/current_game"
LAST_GAME_FILE="$STATE_DIR/last_played_game"
EMULATOR_FILE="$STATE_DIR/emulator"
CURRENT_SESSION_START_FILE="$STATE_DIR/current_session_start"
LAST_SESSION_DURATION_FILE="$STATE_DIR/last_session_duration"

mkdir -p "$BASE_DIR/logs" "$STATE_DIR"

source "$CONFIG_FILE"

###############################################################################
# Logging + MQTT helpers
###############################################################################

log() {
  echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >> "$LOG_FILE"
}

mqtt_pub() {
  local topic="$1"
  local message="$2"
  local retain="${3:-true}"

  if [ "$retain" = "true" ]; then
    mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -u "$MQTT_USER" -P "$MQTT_PASSWORD" -t "$topic" -m "$message" -r
  else
    mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -u "$MQTT_USER" -P "$MQTT_PASSWORD" -t "$topic" -m "$message"
  fi
}

device_json() {
  # modello e versione reali [patch Companion]
  local model ver
  model=$(dmidecode -s system-product-name 2>/dev/null | head -1); [ -n "$model" ] || model="PC"
  ver=$(awk '{print $1}' /usr/share/batocera/batocera.version 2>/dev/null); [ -n "$ver" ] || ver="?"
  echo "\"device\":{\"identifiers\":[\"$DEVICE_ID\"],\"name\":\"$DEVICE_NAME\",\"manufacturer\":\"Batocera\",\"model\":\"$model\",\"sw_version\":\"Batocera $ver\"}"
}

availability_json() {
  echo "\"availability_topic\":\"$BASE_TOPIC/status\",\"payload_available\":\"online\",\"payload_not_available\":\"offline\""
}

###############################################################################
# MQTT Discovery
###############################################################################

# entità delle versioni precedenti, da togliere da Home Assistant (config vuota = entità rimossa)
OLD_ENTITIES="button/wake binary_sensor/update sensor/controller_connected sensor/controller_battery sensor/controller_count sensor/emulator sensor/last_played_game sensor/current_session sensor/last_session sensor/cpu_usage sensor/ip_address sensor/uptime sensor/storage_used sensor/storage_available sensor/storage_total sensor/rom_count sensor/software_version sensor/cpu_freq sensor/load1 sensor/ram_used_mb sensor/ram_total_mb sensor/swap_used_mb sensor/net_rx sensor/net_tx sensor/volume sensor/processes sensor/kernel sensor/cpu_model sensor/gpu_model sensor/resolution"

remove_old_entities() {
  local e h f base label chip
  for e in $OLD_ENTITIES; do
    mqtt_pub "$DISCOVERY_PREFIX/${e%%/*}/$DEVICE_ID/${e#*/}/config" ""
  done
  # sensori hwmon (hw_*) della versione con "tutti i sensori"
  for h in /sys/class/hwmon/hwmon*; do
    [ -r "$h/name" ] || continue
    chip=$(cat "$h/name" 2>/dev/null)
    for f in "$h"/temp*_input "$h"/fan*_input "$h"/in*_input "$h"/power*_input; do
      [ -r "$f" ] || continue
      base=$(basename "$f" _input)
      label=$(cat "$h/${base}_label" 2>/dev/null); [ -z "$label" ] && label="$base"
      mqtt_pub "$DISCOVERY_PREFIX/sensor/$DEVICE_ID/hw_$(slug "$chip")_$(slug "$label")/config" ""
    done
  done
}

slug() { echo "$1" | tr 'A-Z' 'a-z' | sed 's/[^a-z0-9]\+/_/g; s/^_//; s/_$//'; }

publish_discovery() {
  local dev avail
  dev=$(device_json)
  avail=$(availability_json)
  remove_old_entities

  mqtt_pub "$DISCOVERY_PREFIX/binary_sensor/$DEVICE_ID/online/config" "{\"name\":\"Acceso\",\"unique_id\":\"${DEVICE_ID}_online\",\"state_topic\":\"$BASE_TOPIC/status\",\"payload_on\":\"online\",\"payload_off\":\"offline\",\"device_class\":\"power\",$dev}"
  mqtt_pub "$DISCOVERY_PREFIX/sensor/$DEVICE_ID/current_game/config" "{\"name\":\"Gioco in corso\",\"unique_id\":\"${DEVICE_ID}_current_game\",\"state_topic\":\"$BASE_TOPIC/current_game\",\"icon\":\"mdi:gamepad-variant\",$dev,$avail}"
  mqtt_pub "$DISCOVERY_PREFIX/sensor/$DEVICE_ID/cpu_temperature/config" "{\"name\":\"Temperatura CPU\",\"unique_id\":\"${DEVICE_ID}_cpu_temperature\",\"state_topic\":\"$BASE_TOPIC/cpu_temperature\",\"unit_of_measurement\":\"°C\",\"device_class\":\"temperature\",\"state_class\":\"measurement\",$dev,$avail}"
  mqtt_pub "$DISCOVERY_PREFIX/sensor/$DEVICE_ID/ram_usage/config" "{\"name\":\"RAM usata\",\"unique_id\":\"${DEVICE_ID}_ram_usage\",\"state_topic\":\"$BASE_TOPIC/ram_usage\",\"icon\":\"mdi:memory\",\"unit_of_measurement\":\"%\",\"state_class\":\"measurement\",$dev,$avail}"
  mqtt_pub "$DISCOVERY_PREFIX/sensor/$DEVICE_ID/storage_percent/config" "{\"name\":\"Disco usato\",\"unique_id\":\"${DEVICE_ID}_storage_percent\",\"state_topic\":\"$BASE_TOPIC/storage/percent\",\"icon\":\"mdi:harddisk\",\"unit_of_measurement\":\"%\",\"state_class\":\"measurement\",$dev,$avail}"
  mqtt_pub "$DISCOVERY_PREFIX/button/$DEVICE_ID/reboot/config" "{\"name\":\"Riavvia\",\"unique_id\":\"${DEVICE_ID}_reboot\",\"command_topic\":\"$BASE_TOPIC/command/reboot\",\"icon\":\"mdi:restart\",$dev,$avail}"
  mqtt_pub "$DISCOVERY_PREFIX/button/$DEVICE_ID/shutdown/config" "{\"name\":\"Spegni\",\"unique_id\":\"${DEVICE_ID}_shutdown\",\"command_topic\":\"$BASE_TOPIC/command/shutdown\",\"icon\":\"mdi:power-off\",$dev,$avail}"
  log "MQTT discovery published (versione ridotta)"
}

###############################################################################
# Sensori (solo letture da file: nessun programma esterno pesante)
###############################################################################

get_current_game() {
  [ -f "$GAME_FILE" ] && cat "$GAME_FILE" || echo "Nessuno"
}

get_cpu_temp() {
  # temperatura del package CPU (x86_pkg_temp); thermal_zone0 su questi PC è il chipset
  local z
  for z in /sys/class/thermal/thermal_zone*; do
    if [ "$(cat "$z/type" 2>/dev/null)" = "x86_pkg_temp" ]; then
      awk '{printf "%.1f", $1/1000}' "$z/temp"; return
    fi
  done
  awk '{printf "%.1f", $1/1000}' /sys/class/thermal/thermal_zone0/temp 2>/dev/null
}

get_ram_usage() {
  awk '/MemTotal/ {t=$2} /MemAvailable/ {a=$2} END {if (t > 0) printf "%.1f", (t - a) / t * 100}' /proc/meminfo
}

get_storage_percent() {
  df /userdata | awk 'NR==2 {gsub("%","",$5); print $5}'
}

publish_sensors() {
  mqtt_pub "$BASE_TOPIC/status" "online"
  mqtt_pub "$BASE_TOPIC/current_game" "$(get_current_game)"
  mqtt_pub "$BASE_TOPIC/cpu_temperature" "$(get_cpu_temp)"
  mqtt_pub "$BASE_TOPIC/ram_usage" "$(get_ram_usage)"
  mqtt_pub "$BASE_TOPIC/storage/percent" "$(get_storage_percent)"
}

format_duration() { :; }

###############################################################################
# Batocera event scripts
###############################################################################

install_event_scripts() {
  local event

  for event in game-start game-end system-selected controls-changed; do
    mkdir -p "/userdata/system/configs/emulationstation/scripts/$event"

    cat > "/userdata/system/configs/emulationstation/scripts/$event/homeassistant.sh" <<EOF
#!/bin/bash
/userdata/system/homeassistant/homeassistant.sh event "$event" "\$@"
EOF

    chmod +x "/userdata/system/configs/emulationstation/scripts/$event/homeassistant.sh"
  done

  log "Event scripts installed"
}

handle_event() {
  local event="$1"
  local rom
  local system
  local game
  local current_game
  local start
  local now
  local elapsed

  shift

  case "$event" in
    game-start)
      rom="$1"
      system=$(echo "$rom" | awk -F'/roms/' '{print $2}' | cut -d'/' -f1)

      game=$(basename "$rom")
      game="${game%.*}"
      game=$(echo "$game" | sed 's/\\ / /g; s/\\//g')
      [ -n "$3" ] && game="$3"   # nome del gioco come lo mostra EmulationStation

      echo "$game" > "$GAME_FILE"
      echo "$system" > "$EMULATOR_FILE"
      date +%s > "$CURRENT_SESSION_START_FILE"
    
      log "Game started: $game / $system"
      mqtt_pub "$BASE_TOPIC/current_game" "$game"
      ;;
    
    game-end)
      current_game=$(cat "$GAME_FILE" 2>/dev/null)
      if [ -n "$current_game" ] && [ "$current_game" != "Nessuno" ] && [ "$current_game" != "unknown" ]; then
        echo "$current_game" > "$LAST_GAME_FILE"
      fi

      # Save completed session duration
      if [ -f "$CURRENT_SESSION_START_FILE" ]; then
        start=$(cat "$CURRENT_SESSION_START_FILE" 2>/dev/null)
        now=$(date +%s)
        if echo "$start" | grep -Eq '^[0-9]+$'; then
          elapsed=$((now - start))
          if [ "$elapsed" -ge 0 ]; then
            format_duration "$elapsed" > "$LAST_SESSION_DURATION_FILE"
          fi
        fi
      fi
      echo "Nessuno" > "$GAME_FILE"
      rm -f "$CURRENT_SESSION_START_FILE"
      log "Game ended"
      mqtt_pub "$BASE_TOPIC/current_game" "Nessuno"
      ;;

    system-selected)
      if [ -n "$1" ] && [ "$1" != "unknown" ] && [ "$1" != "Unknown" ]; then
        echo "$1" > "$EMULATOR_FILE"
        log "System selected: $1"
      fi
      ;;

    controls-changed)
      log "Controls changed"
      ;;
  esac
}

###############################################################################
# Commands + service lifecycle
###############################################################################

listen_commands() {
  mosquitto_sub \
    -h "$MQTT_HOST" \
    -p "$MQTT_PORT" \
    -u "$MQTT_USER" \
    -P "$MQTT_PASSWORD" \
    -v \
    -t "$BASE_TOPIC/command/#" \
    --will-topic "$BASE_TOPIC/status" \
    --will-payload "offline" \
    --will-retain | while read -r topic payload
  do
    log "Command received: $topic $payload"

    case "$topic" in
      "$BASE_TOPIC/command/reboot")
        mqtt_pub "$BASE_TOPIC/status" "offline"
        reboot
        ;;

      "$BASE_TOPIC/command/shutdown")
        mqtt_pub "$BASE_TOPIC/status" "offline"
        poweroff
        ;;

    esac
  done
}

start_service() {
  echo "running" > "$SERVICE_FLAG"
  pgrep -f emulatorlauncher >/dev/null 2>&1 || echo "Nessuno" > "$GAME_FILE"
  install_event_scripts
  publish_discovery
  publish_sensors
  log "Batocera Home Assistant service started"
  listen_commands &
  COMMAND_PID=$!
  while [ -f "$SERVICE_FLAG" ]; do
    sleep 30
    publish_sensors
  done
  kill "$COMMAND_PID" 2>/dev/null
}

stop_service() {
  rm -f "$SERVICE_FLAG"
  mqtt_pub "$BASE_TOPIC/status" "offline"
  log "Batocera Home Assistant service stopped"
}

case "$1" in
  start) start_service ;;
  stop) stop_service ;;
  restart) stop_service; sleep 2; start_service ;;
  install) install_event_scripts; publish_discovery; publish_sensors ;;
  event) shift; handle_event "$@" ;;
  *) echo "Usage: $0 start|stop|restart|install|event" ;;
esac
