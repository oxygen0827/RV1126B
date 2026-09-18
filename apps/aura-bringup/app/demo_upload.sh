#!/bin/sh
# 板端演示云上传：session_meta.json → 转码 MP4 → A3 → PUT(视频+序列) → A5 → A6 轮询 → 报告
# 用法：sh demo_upload.sh    （屏幕「发送」后由板端后台直接执行）
# 依赖：ffmpeg python3 curl gzip；凭证在 uploader.env（不入库）
# 可在 Mac 上跑：UPLOAD_DIR=<本地目录> sh demo_upload.sh（脚本对 UPLOAD_DIR 内文件操作，
# 完成后如检测到 adb 会把状态回推板端）。
set -eu

# The deployed RV1106 image does not register the vendor MPP directory with
# the dynamic loader for processes started by the UI. Keep mpi_enc_test usable
# from the background uploader as well as from an interactive shell.
if [ -d /oem/usr/lib ]; then
  LD_LIBRARY_PATH="/oem/usr/lib:/oem/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
  export LD_LIBRARY_PATH
fi

UPLOAD=${UPLOAD_DIR:-/root/pose_deploy/upload}
APP=${APP_DIR:-/root/pose_deploy/yolov8n-pose}
META=$UPLOAD/session_meta.json
ENV_FILE=${UPLOADER_ENV:-$APP/uploader.env}
LOG=$UPLOAD/upload.log
OK=$UPLOAD/status_ok.txt
ERR=$UPLOAD/status_err.txt

# Keep the file lock across exec; an orphan uploader must also protect media
# from a newly restarted backend. Python is already a board dependency.
mkdir -p "$UPLOAD"
if [ "${FITNESS_UPLOAD_LOCKED:-0}" != "1" ]; then
  exec python3 - "$0" "$UPLOAD" <<'PY'
import fcntl,os,sys
lock = open(os.path.join(sys.argv[2], ".session.lock"), "a")
try:
    fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
except BlockingIOError:
    sys.exit("session files busy")
os.set_inheritable(lock.fileno(), True)
os.environ["FITNESS_UPLOAD_LOCKED"] = "1"
os.execv("/bin/sh", ["/bin/sh", sys.argv[1]])
PY
fi

BACK_TO_BOARD=0
if command -v adb >/dev/null 2>&1 && [ "$UPLOAD" != "/root/pose_deploy/upload" ]; then
  BACK_TO_BOARD=1
fi

mkdir -p "$UPLOAD"
rm -f "$OK" "$ERR" "$OK.tmp"
log() { echo "[$(date +%H:%M:%S)] $*" >> "$LOG"; echo "$*"; }
fail() { log "FAIL: $*"; printf '%s\n' "$*" > "$ERR"; exit 1; }
on_exit() {
  rc=$?
  if [ "$rc" -ne 0 ] && [ ! -f "$ERR" ]; then
    printf '%s\n' "uploader exited ($rc); see upload.log" > "$ERR"
  fi
}
trap on_exit 0

if [ ! -f "$META" ]; then fail "meta not ready"; fi
# Encode-only mode is also used by the local-save path and deliberately does
# not require cloud credentials or a network connection.
ENCODE_ONLY=${FITNESS_ENCODE_ONLY:-0}
if [ "$ENCODE_ONLY" != "1" ]; then
  if [ ! -f "$ENV_FILE" ]; then fail "AUTH: uploader.env missing"; fi
  . "$ENV_FILE"   # DEMO_BASE DEMO_TOKEN DEMO_PROXY(可选,如 http://127.0.0.1:1443)
  [ -n "${DEMO_BASE:-}" ] || fail "AUTH: DEMO_BASE missing"
  [ -n "${DEMO_TOKEN:-}" ] || fail "AUTH: DEMO_TOKEN missing"
fi

# TLS validation needs a plausible clock. Boards without an RTC boot at 2021,
# so try one sync (the network is required for the upload anyway) and fail with
# a clear reason instead of an opaque curl=60.
if [ "$ENCODE_ONLY" != "1" ]; then
  year=$(date +%Y 2>/dev/null || echo 0)
  case "$year" in ''|*[!0-9]*) year=0 ;; esac
  if [ "$year" -lt 2024 ]; then
    if [ -x /root/pose_deploy/wifi_provision.sh ]; then
      sh /root/pose_deploy/wifi_provision.sh sync-time >/dev/null 2>&1 || true
      year=$(date +%Y 2>/dev/null || echo 0)
      case "$year" in ''|*[!0-9]*) year=0 ;; esac
    fi
  fi
  if [ "$year" -lt 2024 ]; then
    fail "CLOCK: device time is not synced; TLS upload would fail"
  fi
fi

AUTH="Authorization: Bearer ${DEMO_TOKEN:-}"
J='Content-Type: application/json'
PROXY_ARG=""
if [ -n "${DEMO_PROXY:-}" ]; then PROXY_ARG="-x $DEMO_PROXY"; fi
ORIGIN=${DATA_ORIGIN:-real}

# read meta fields (python3 单点解析)
get() { python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))[sys.argv[2]])' "$META" "$1"; }
get_opt() { python3 -c 'import json,sys;m=json.load(open(sys.argv[1]));v=m.get(sys.argv[2]);print("" if v is None else v)' "$META" "$1"; }
CRI=${UPLOAD_CRI:-$(get client_request_id)}; MV=$(get movement)
ST_MS=$(get started_at_ms);    EN_MS=$(get ended_at_ms)
RC=$(get record_count);        MVER=$(get model_version)
W=$(get width);                H=$(get height)
PS=$(get fps);                 VFPS=$(get_opt video_fps)
VCOUNT=$(get_opt video_frame_count); VGOP=$(get_opt video_gop)
VIDEO_RAW=$(get video_file);   POSE_GZ=$(get pose_file)
[ -n "$CRI" ] && [ -n "$MV" ] && [ -n "$ST_MS" ] && [ -n "$EN_MS" ] || fail "required session metadata missing"
[ -n "$RC" ] && [ -n "$MVER" ] && [ -n "$W" ] && [ -n "$H" ] || fail "required media metadata missing"
# 板端 meta 的路径为板端绝对路径；在 Mac（桥接器）侧运行时映射到本地素材目录
if [ ! -f "$VIDEO_RAW" ] && [ -f "$UPLOAD/video_raw.bgr" ]; then VIDEO_RAW="$UPLOAD/video_raw.bgr"; fi
if [ ! -f "$VIDEO_RAW" ] && [ -f "$UPLOAD/video.h264" ]; then VIDEO_RAW="$UPLOAD/video.h264"; fi
if [ ! -f "$VIDEO_RAW" ] && [ -f "$UPLOAD/video.mp4" ]; then VIDEO_RAW="$UPLOAD/video.mp4"; fi
if [ ! -f "$POSE_GZ" ] && [ -f "$UPLOAD/pose.jsonl.gz" ]; then POSE_GZ="$UPLOAD/pose.jsonl.gz"; fi
case "$VIDEO_RAW" in
  *.h264|*.264) VIDEO_KIND=h264 ;;
  *.mp4)        VIDEO_KIND=mp4 ;;
  *)            VIDEO_KIND=bgr ;;
esac
[ -s "$VIDEO_RAW" ] || fail "video source missing or empty"

if [ "$ENCODE_ONLY" != "1" ]; then
  [ -s "$POSE_GZ" ] || fail "pose sequence missing or empty"
fi

# Validate complete source files before creating any cloud session. Do not
# silently upload truncated frames or skip corrupt JSONL records.
if [ "$ENCODE_ONLY" = "1" ]; then
  TR='[0,0]'
else
  TR=$(python3 - "$META" "$VIDEO_RAW" "$POSE_GZ" "$VIDEO_KIND" <<'PY'
import gzip,json,os,sys
meta=json.load(open(sys.argv[1]))
w,h=meta['width'],meta['height']
assert type(w) is int and type(h) is int and 0<w<=4096 and 0<h<=4096, 'invalid dimensions'
if sys.argv[4]=='bgr':
    size=os.path.getsize(sys.argv[2])
    assert size>=w*h*3 and size%(w*h*3)==0, 'truncated raw frame'
assert 0<=meta['started_at_ms']<meta['ended_at_ms'], 'invalid session times'
times=[]
with gzip.open(sys.argv[3], 'rt') as sequence:
    for line in sequence:
        record=json.loads(line)
        if record.get('kind')=='frame':
            t=record['t_ms']
            assert type(t) is int and t>=0 and (not times or t>=times[-1]), 'invalid frame time'
            times.append(t)
assert times and len(times)==meta['record_count'], 'pose record count mismatch'
print(json.dumps([times[0],times[-1]]))
PY
) || fail "invalid recording data"
fi

log "meta: move=$MV cri=$CRI rc=$RC t=${ST_MS}..${EN_MS}"

curl_fail() {
  stage=$1; rc=$2
  case "$rc" in
    6|7|28) fail "NETWORK: cloud endpoint unreachable ($stage)" ;;
    22) fail "CLOUD: server rejected request ($stage)" ;;
    *) fail "CLOUD: request failed ($stage, curl=$rc)" ;;
  esac
}

# Encode raw BGR independently of the cloud flow.  Set FITNESS_ENCODE_ONLY=1
# to stop after writing video.mp4 (or FITNESS_ENCODE_OUTPUT for another path).
# On RV1106 the vendor MPP
# utility accepts BGR888 directly (format 65543) and avoids the unavailable
# h264_v4l2m2m/libx264 encoders.  ffmpeg remains a portable fallback.
run_timeout() {
  if command -v timeout >/dev/null 2>&1; then timeout "${ENCODE_TIMEOUT:-180}" "$@"; else "$@"; fi
}

# Validate the actual container before publishing it. A successful exit code
# from a vendor encoder alone is insufficient (truncated output has occurred
# in the field). When want>0 the frame count must match exactly.
probe_encoded_mp4() {
  file=$1; want=${2:-${VCOUNT:-0}}; gop=${3:-${VGOP:-0}}
  probe=$(mktemp "${TMPDIR:-/tmp}/fitness-probe.XXXXXX") || return 1
  # Reuse the existing decode/count pass after recording, never in the capture
  # loop. Only new GOP-tagged recordings require periodic random-access frames.
  if run_timeout ffprobe -v error -count_frames -select_streams v:0 \
      -show_entries stream=codec_name,width,height,nb_read_frames -show_entries format=duration \
      -show_entries frame=key_frame,pict_type \
      -of json "$file" >"$probe" 2>>"$LOG" \
     && python3 - "$probe" "$want" "$gop" 2>>"$LOG" <<'PY'
import json,sys
d=json.load(open(sys.argv[1])); s=(d.get('streams') or [{}])[0]
w,h=int(s.get('width') or 0),int(s.get('height') or 0)
n=int(s.get('nb_read_frames') or 0); dur=float((d.get('format') or {}).get('duration') or 0)
want=int(sys.argv[2]); gop=int(sys.argv[3])
assert want>=0 and gop>=0, 'invalid video frame count/GOP'
assert w==640 and h==480 and n>0 and dur>0
if want: assert n==want, 'frame count mismatch'
if gop:
    assert s.get('codec_name')=='h264', 'expected H.264'
    frames=d.get('frames') or []
    assert len(frames)==n, 'incomplete frame probe'
    keys=[i for i,f in enumerate(frames) if f.get('key_frame')==1 and f.get('pict_type')=='I']
    assert keys and keys[0]==0, 'video must start with a key I frame'
    assert all(b-a<=gop for a,b in zip(keys, keys[1:]+[n])), 'keyframe gap exceeds GOP'
PY
  then rm -f "$probe"; return 0; fi
  rm -f "$probe"
  return 1
}

# Live MPP recording writes Annex-B H.264; only the container needs to be
# produced on the board (stream copy, no re-encode).
remux_h264_to_mp4() {
  h264=$1; out=$2; fps=${3:-25}
  work=$(mktemp -d "${TMPDIR:-/tmp}/fitness-remux.XXXXXX") || return 1
  mp4=$work/video.mp4
  if run_timeout ffmpeg -y -loglevel error -r "$fps" -i "$h264" -c:v copy -an -movflags +faststart "$mp4" >>"$LOG" 2>&1 \
     && [ -s "$mp4" ] \
     && probe_encoded_mp4 "$mp4"; then
    mv "$mp4" "$out" || { rm -rf "$work"; return 1; }
    rm -rf "$work"
    return 0
  fi
  rm -rf "$work"
  return 1
}

encode_raw_to_mp4() {
  raw=$1; out=$2; width=$3; height=$4; fps=${5:-5}
  frame_bytes=$((width * height * 3))
  raw_size=$(wc -c < "$raw" | tr -d '[:space:]')
  [ "$frame_bytes" -gt 0 ] && [ "$raw_size" -ge "$frame_bytes" ] && [ $((raw_size % frame_bytes)) -eq 0 ] || return 2
  frames=$((raw_size / frame_bytes))
  # MPP uses a ratio for FPS and mode:length:vi_length for GOP; ffmpeg uses -g length.
  timing=$(python3 - "$fps" <<'PY'
import math,sys
fps=float(sys.argv[1])
assert math.isfinite(fps) and 0.001<=fps<=1000, 'invalid recording fps'
num=int(math.floor(fps*1000+0.5)); gop=max(1, int(math.floor(num*2/1000+0.5)))
divisor=math.gcd(num,1000); rate='%d:%d:0' % (num//divisor,1000//divisor)
print('%s/%s %d' % (rate,rate,gop))
PY
  ) || return 2
  mpp_fps=${timing% *}; raw_gop=${timing##* }
  work=$(mktemp -d "${TMPDIR:-/tmp}/fitness-encode.XXXXXX") || return 1
  h264=$work/video.h264; mp4=$work/video.mp4
  success=0
  mpp=${MPP_ENC_BIN:-/oem/usr/bin/mpi_enc_test}
  if [ "$width" -eq 640 ] && [ "$height" -eq 480 ] && [ -x "$mpp" ]; then
    if run_timeout "$mpp" -i "$raw" -o "$h264" -w "$width" -h "$height" -f 65543 -t 7 -n "$frames" -fps "$mpp_fps" -g "0:$raw_gop:0" -bps 1200000 >>"$LOG" 2>&1 \
       && [ -s "$h264" ] \
       && run_timeout ffmpeg -y -loglevel error -r "$fps" -i "$h264" -c:v copy -an -movflags +faststart "$mp4" >>"$LOG" 2>&1 \
       && [ -s "$mp4" ]; then
      success=1
    else
      log "WARN: MPP H.264 encode unavailable; trying ffmpeg fallback"
    fi
  fi
  if [ "$success" -ne 1 ]; then
    rm -f "$mp4"
    # Keep the legacy ffmpeg V4L2-M2M path for boards that expose it.
    if run_timeout ffmpeg -y -loglevel error -framerate "$fps" -f rawvideo -pix_fmt bgr24 -s "${width}x${height}" -i "$raw" \
         -vf scale=640:480,format=nv12 -c:v h264_v4l2m2m -b:v 1200k -g "$raw_gop" -an -movflags +faststart "$mp4" >>"$LOG" 2>&1 \
       && [ -s "$mp4" ]; then success=1; fi
  fi
  if [ "$success" -ne 1 ]; then
    rm -f "$mp4"
    if run_timeout ffmpeg -y -loglevel error -framerate "$fps" -f rawvideo -pix_fmt bgr24 -s "${width}x${height}" -i "$raw" \
         -vf scale=640:480 -c:v libx264 -crf 30 -g "$raw_gop" -pix_fmt yuv420p -an -movflags +faststart "$mp4" >>"$LOG" 2>&1 \
       && [ -s "$mp4" ]; then success=1; fi
  fi
  if [ "$success" -ne 1 ]; then rm -rf "$work"; return 1; fi
  # Validate the actual container before publishing it.  A successful exit
  # code from a vendor encoder alone is insufficient (truncated output has
  # occurred in the field).
  if ! probe_encoded_mp4 "$mp4" "$frames" "$raw_gop"; then rm -rf "$work"; return 1; fi
  mv "$mp4" "$out" || { rm -rf "$work"; return 1; }
  rm -rf "$work"
  return 0
}

MP4=$UPLOAD/video.mp4
if [ "$VIDEO_RAW" != "$MP4" ]; then rm -f "$MP4"; fi
VIDEO_FPS=${VIDEO_FPS:-$VFPS}
VIDEO_FPS=${VIDEO_FPS:-$PS}
VIDEO_FPS=${VIDEO_FPS:-25}
case "$VIDEO_KIND" in
  bgr)
    if ! encode_raw_to_mp4 "$VIDEO_RAW" "$MP4" "$W" "$H" "$VIDEO_FPS"; then
      fail "ENCODE: H.264 video encoding or validation failed"
    fi
    ;;
  h264)
    if ! remux_h264_to_mp4 "$VIDEO_RAW" "$MP4" "$VIDEO_FPS"; then
      fail "ENCODE: H.264 remux or validation failed"
    fi
    ;;
  mp4)
    if [ "$VIDEO_RAW" != "$MP4" ]; then
      cp "$VIDEO_RAW" "$MP4" || fail "ENCODE: MP4 copy failed"
    fi
    probe_encoded_mp4 "$MP4" || fail "ENCODE: invalid MP4 container"
    ;;
esac
V_SIZE=$(wc -c < "$MP4" | tr -d '[:space:]')
V_DUR=$(ffprobe -v error -show_entries format=duration -of csv=p=0 "$MP4" 2>/dev/null) || fail "ENCODE: unable to read MP4 duration"
[ "$V_SIZE" -gt 0 ] || fail "ENCODE: empty MP4 after transcode"
log "video: $V_SIZE bytes, ${V_DUR}s (640x480)"

if [ "$ENCODE_ONLY" = "1" ]; then
  # Caller may provide an alternate destination while preserving the raw file.
  if [ -n "${FITNESS_ENCODE_OUTPUT:-}" ] && [ "$FITNESS_ENCODE_OUTPUT" != "$MP4" ]; then
    cp "$MP4" "$FITNESS_ENCODE_OUTPUT"
  fi
  exit 0
fi

# 2. A3 创建会话（重放幂等：CRI 固定；失败可换新 CRI 重跑一次）
A3_BODY=$(python3 - "$CRI" "$MV" "$ORIGIN" <<'PY'
import json,sys,time
print(json.dumps(dict(client_request_id=sys.argv[1], contract_version="0.2",
    movement_key=sys.argv[2], view=0, data_origin=sys.argv[3], device_time_ms=int(time.time()*1000))))
PY
)
A3=$(curl -sf $PROXY_ARG -m 30 -X POST -H "$AUTH" -H "$J" "$DEMO_BASE/sessions" -d "$A3_BODY" 2>>"$LOG") || { rc=$?; curl_fail "A3 create session (movement=$MV origin=$ORIGIN)" "$rc"; }
SID=$(printf '%s' "$A3" | python3 -c '
import json,sys
sid=json.load(sys.stdin)["data"].get("session_id")
assert isinstance(sid,str) and sid
print(sid)
') || fail "A3 response missing session_id"
REPLAY=0
if ! printf '%s' "$A3" | python3 -c '
import json,sys,urllib.parse
u=json.load(sys.stdin)["data"].get("upload") or {}
assert all(isinstance(v,str) and v for v in (u.get("video_key"),u.get("sequence_key"),u.get("video_put_url"),u.get("sequence_put_url")))
assert all(urllib.parse.urlsplit(u[k]).scheme=="https" and urllib.parse.urlsplit(u[k]).netloc for k in ("video_put_url","sequence_put_url"))
' 2>/dev/null; then
  # The server deduplicates by client_request_id: re-sending an existing
  # session returns it without fresh upload URLs. Only a response with a
  # session id and no upload block is a valid replay; anything else fails.
  printf '%s' "$A3" | python3 -c 'import json,sys; d=json.load(sys.stdin).get("data",{}); assert isinstance(d.get("session_id"),str) and d["session_id"]; assert not d.get("upload")' 2>/dev/null || fail "A3 response contains invalid upload fields"
  REPLAY=1
fi
P_SIZE=$(wc -c < "$POSE_GZ" | tr -d '[:space:]')
if [ "$REPLAY" = "1" ]; then
  log "session: $SID (already exists; skipping uploads)"
else
  VKEY=$(printf '%s' "$A3" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['upload']['video_key'])") || fail "A3 response missing video_key"
  VURL=$(printf '%s' "$A3" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['upload']['video_put_url'])") || fail "A3 response missing video_put_url"
  SURL=$(printf '%s' "$A3" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['upload']['sequence_put_url'])") || fail "A3 response missing sequence_put_url"
  SKEY=$(printf '%s' "$A3" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['upload']['sequence_key'])") || fail "A3 response missing sequence_key"
  [ -n "$VKEY" ] && [ -n "$VURL" ] && [ -n "$SURL" ] && [ -n "$SKEY" ] || fail "A3 response contains empty upload fields"
  log "session: $SID"

  # 3. 直传（预签名 URL：签名绑定 Content-Type，勿增删头）
  curl -sf $PROXY_ARG -m 600 -o /dev/null -X PUT -H 'Content-Type: video/mp4' --data-binary "@$MP4" "$VURL" 2>>"$LOG" || { rc=$?; curl_fail "PUT video" "$rc"; }
  curl -sf $PROXY_ARG -m 120 -o /dev/null -X PUT -H 'Content-Type: application/gzip' --data-binary "@$POSE_GZ" "$SURL" 2>>"$LOG" || { rc=$?; curl_fail "PUT pose" "$rc"; }
  log "uploads done"
fi

# 4. A5 finalize（序列 time_range_ms = 首末行 t_ms）；重放已有会话时跳过
if [ "$REPLAY" != "1" ]; then
A5_BODY=$(python3 - "$META" "$VKEY" "$SKEY" "$V_SIZE" "$V_DUR" "$P_SIZE" "$TR" <<'PY'
import json,math,sys,time
m=json.load(open(sys.argv[1])); duration=float(sys.argv[5])
assert math.isfinite(duration) and duration>0, 'invalid encoded duration'
print(json.dumps(dict(
    video=dict(oss_key=sys.argv[2],file_size=int(sys.argv[4]),duration_ms=round(duration*1000),width=640,height=480,fps=float(m.get('video_fps') or m.get('fps') or 25),codec="h264"),
    sequence=dict(oss_key=sys.argv[3],file_size=int(sys.argv[6]),record_count=m['record_count'],time_range_ms=json.loads(sys.argv[7]),model_version=m['model_version']),
    started_at_ms=m['started_at_ms'],ended_at_ms=m['ended_at_ms'],device_time_ms=int(time.time()*1000),
    firmware_version=m.get('firmware_version','demo-0.1'),model_version=m['model_version'])))
PY
) || fail "invalid finalize metadata"
A5=$(curl -sf $PROXY_ARG -m 60 -X POST -H "$AUTH" -H "$J" "$DEMO_BASE/sessions/$SID/finalize" -d "$A5_BODY" 2>>"$LOG") || { rc=$?; curl_fail "A5 finalize" "$rc"; }
log "finalize: $(echo "$A5" | head -c 120)"
fi

# 5. A6 轮询（200s 上限；单次请求最长 20s；断网也计入超时）
ST_AT=$(date +%s)
while :; do
  if [ $(( $(date +%s) - ST_AT )) -ge "${UPLOAD_POLL_TIMEOUT:-200}" ]; then fail "poll timeout"; fi
  sleep 3
  A6=$(curl -sf $PROXY_ARG -m 20 -H "$AUTH" "$DEMO_BASE/sessions/$SID" 2>>"$LOG") || { log "WARN: A6 poll error, retry"; continue; }
  ST=$(echo "$A6" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['status'])") || { log "WARN: invalid A6 response, retry"; continue; }
  if [ "$ST" = "report_ready" ]; then
    echo "$A6" > "$UPLOAD/report.json"
    VERDICT=$(echo "$A6" | python3 -c "import json,sys;print(json.load(sys.stdin)['data']['report']['summary']['verdict_text'])" 2>/dev/null)
    echo "$VERDICT" > "$UPLOAD/report_verdict.txt"
    log "REPORT READY: $VERDICT"
    python3 - "$SID" "$V_SIZE" "$P_SIZE" <<'EOF' > "$OK.tmp"
import datetime,json,sys
json.dump({
    "session_id": sys.argv[1],
    "video_size": int(sys.argv[2]),
    "pose_size": int(sys.argv[3]),
    "status": "report_ready",
    "completed_at": datetime.datetime.now(datetime.timezone.utc).isoformat(),
}, sys.stdout, separators=(",", ":"))
sys.stdout.write("\n")
EOF
    mv "$OK.tmp" "$OK"
    if [ "$BACK_TO_BOARD" = "1" ]; then
      adb push "$OK" /root/pose_deploy/upload/status_ok.txt >/dev/null 2>&1 || true
      adb push "$UPLOAD/report_verdict.txt" /root/pose_deploy/upload/report_verdict.txt >/dev/null 2>&1 || true
    fi
    exit 0
  fi
  if [ "$ST" = "failed" ] || [ "$ST" = "expired" ]; then
    echo "$A6" > "$UPLOAD/report.json"
    log "FAIL: session $ST $(echo "$A6" | python3 -c "import json,sys;d=json.load(sys.stdin)['data'];print(d.get('fail_reason_code'),d.get('error_msg'))")"
    fail "session $ST"
  fi
done
