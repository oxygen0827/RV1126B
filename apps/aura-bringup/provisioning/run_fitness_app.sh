#!/bin/sh
# Aura 健身应用启动：独立 ISP 3A + yolosrv(摄像头推理/录制/上传) + LVGL UI
# 用法: sh run_fitness_app.sh
APP_DIR=/root/yolov8s-pose
UI_DIR=/root/lvgl-app/build-native

# rkipc 等 /oem 工具需要 /oem 库优先；systemd/非登录 shell 默认没有该变量，
# 否则会加载系统里 2021 年的旧 MPP（缺 mpp_enc_cfg_init_k）导致 rkipc 崩溃。
export LD_LIBRARY_PATH=/oem/usr/lib:/oem/lib${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}

pkill -f luckfox_lvgl_demo 2>/dev/null
pkill -x yolosrv-new 2>/dev/null
pkill -x rkipc 2>/dev/null
# Clean up only the legacy DHCP client for this Wi-Fi interface.
pkill -f '^udhcpc -i wlan0( |$)' 2>/dev/null
sleep 2

# 1) Only the vendor 3A engine owns ISP statistics/parameters. rkipc also
# manages networking and clears wlan0 addresses; do not run it alongside NM.
systemctl restart chiform-isp || exit 1

# 2) yolosrv：摄像头推理 + 20s 录制/保存/上传后端（判定窗由 UI 的触发文件控制）
cd "$APP_DIR"
# TTS 密钥（可选）：/root/yolov8s-pose/tts.env 里 ZHIPU_API_KEY=...
TTS_ARGS=""
if [ -f "$APP_DIR/tts.env" ]; then
  . "$APP_DIR/tts.env"
  [ -n "${ZHIPU_API_KEY:-}" ] && TTS_ARGS="-tts-api-key $ZHIPU_API_KEY -tts-cache-dir /userdata/fitness/tts"
fi
mkdir -p /userdata/fitness/tts
# Per-frame JSONL is a diagnostic option, not a production log. The session
# recorder already stores bounded pose data for each requested recording.
# Remove the obsolete stream after stopping its writer above.
rm -f /tmp/fitness_live.jsonl
setsid ./yolosrv-new -model yolov8s_pose_416_w8a8.rknn -v4l2 /dev/video13 \
  -vw 1280 -vh 720 -frames 2147483647 -conf 0.4 -smooth 0.5 -rotate90 \
  -demo -session-file /tmp/fitness_session.trigger -session-seconds 20 \
  -movement air_squat -correction-fps 25 $TTS_ARGS \
  > /tmp/fitness_app.log 2>&1 < /dev/null &
sleep 5

# 2.5) Wi-Fi 状态文件：立即写一次 + 周期刷新（UI 右上角图标颜色）
pkill -f wifi_status_loop.sh 2>/dev/null
setsid sh "$APP_DIR/wifi_status_loop.sh" >/dev/null 2>&1 < /dev/null &

# 3) LVGL UI（fbdev，独占 DSI 屏幕）
cd "$UI_DIR"
setsid ./luckfox_lvgl_demo > /tmp/lvgl-ui.log 2>&1 < /dev/null &
sleep 2

echo "--- processes ---"
ps aux | grep -E '[r]kaiq_3A_server|[y]olosrv-new|[l]uckfox_lvgl_demo' | awk '{print $11, $12}'
echo "--- wifi status ---"
cat /tmp/fitness_wifi_status.txt 2>/dev/null
