#!/bin/bash
# Batocera Companion - registrazione schermo (kmsgrab+VAAPI, ripiego x11grab)
export XDG_RUNTIME_DIR=/var/run
PID_FILE=/tmp/companion-rec.pid
OUT_DIR=/userdata/recordings
LOG=/userdata/system/logs/companion-recording.log
mkdir -p "$OUT_DIR" "$(dirname "$LOG")"
export DISPLAY=$(getLocalXDisplay 2>/dev/null || echo :0)
case "$1" in
  is_running)
    if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then echo true; else echo false; fi ;;
  stop)
    if [ -f "$PID_FILE" ]; then
      P=$(cat "$PID_FILE"); kill -2 "$P" 2>/dev/null
      for i in $(seq 1 60); do kill -0 "$P" 2>/dev/null || break; sleep 0.1; done
      kill -0 "$P" 2>/dev/null && kill -15 "$P"
      rm -f "$PID_FILE"
    fi ;;
  start)
    Q="${2:-mid}"
    case "$Q" in
      ultra) CRF=18; QP=17; PRESET=medium; FPS=60 ;;
      high)  CRF=23; QP=20; PRESET=fast; FPS=60 ;;
      low)   CRF=35; QP=28; PRESET=ultrafast; FPS=30 ;;
      *)     CRF=28; QP=24; PRESET=veryfast; FPS=60 ;;
    esac
    GEOM=$(batocera-resolution currentResolution 2>/dev/null)
    [ -z "$GEOM" ] && GEOM=$(xdpyinfo 2>/dev/null | awk '/dimensions/{print $2}')
    [ -z "$GEOM" ] && GEOM=1280x1024
    SRC=$(LC_ALL=C pactl info 2>/dev/null | grep -E '^Default Sink: ' | sed -e 's/^Default Sink: //' | awk '{print $1".monitor"}')
    AIN=(); AOUT=()
    if [ -n "$SRC" ]; then AIN=(-thread_queue_size 1024 -use_wallclock_as_timestamps 1 -f pulse -ac 2 -i "$SRC"); AOUT=(-c:a aac -b:a 192k); fi
    OUT="$OUT_DIR/rec-$(date +%Y.%m.%d-%Hh%M.%S).mkv"
    CARD=$(ls /dev/dri/card* 2>/dev/null | head -1)
    # 1) kmsgrab + VAAPI: legge l'immagine già pronta per il monitor e la codifica il chip Intel.
    #    Risoluzione nativa, CPU quasi a zero e soprattutto non disturba il gioco (niente scatti/tearing).
    echo "--- start $(date) q=$Q geom=$GEOM audio=$SRC metodo=kmsgrab" > "$LOG"
    ffmpeg -y -loglevel warning -device "$CARD" -thread_queue_size 1024 -use_wallclock_as_timestamps 1 -f kmsgrab -framerate "$FPS" -i - "${AIN[@]}" \
      -vf 'hwmap=derive_device=vaapi,scale_vaapi=format=nv12' -c:v h264_vaapi -qp "$QP" "${AOUT[@]}" "$OUT" >> "$LOG" 2>&1 &
    echo $! > "$PID_FILE"
    sleep 1.5
    if ! kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
      # 2) ripiego: x11grab (più pesante, può far scattare i giochi)
      echo "--- kmsgrab non disponibile, uso x11grab" >> "$LOG"
      ENC=(-c:v libx264 -crf "$CRF" -preset "$PRESET" -pix_fmt yuv420p)
      if command -v vainfo >/dev/null 2>&1 && vainfo --display drm --device /dev/dri/renderD128 2>/dev/null | grep -q "VAProfileH264Main.*VAEntrypointEncSlice"; then
        ENC=(-vaapi_device /dev/dri/renderD128 -vf "format=nv12,hwupload" -c:v h264_vaapi -qp "$QP")
      fi
      ffmpeg -y -probesize 20M -video_size "$GEOM" -framerate "$FPS" -thread_queue_size 1024 -f x11grab -i "$DISPLAY+0,0" "${AIN[@]}" "${ENC[@]}" "${AOUT[@]}" "$OUT" >> "$LOG" 2>&1 &
      echo $! > "$PID_FILE"
      sleep 0.7
    fi
    echo $! > "$PID_FILE"
    sleep 0.7
    kill -0 "$(cat "$PID_FILE")" 2>/dev/null || { rm -f "$PID_FILE"; tail -n 5 "$LOG"; exit 1; }
    echo "$OUT" ;;
esac
