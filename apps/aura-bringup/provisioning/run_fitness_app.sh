#!/bin/sh
# Aura 健身应用启动：rkipc(3A,VO off) + yolosrv(摄像头推理/录制/上传) + LVGL UI
# 用法: sh run_fitness_app.sh
APP_DIR=/root/yolov8s-pose
UI_DIR=/root/lvgl-app/build-native

pkill -f luckfox_lvgl_demo 2>/dev/null
pkill -f 'yolosrv-new' 2>/dev/null
pkill rkipc 2>/dev/null
sleep 2

# 1) rkipc：只提供 ISP 3A（关闭自己的显示层，把屏幕让给 LVGL）
sed -i 's/^enable_vo                      = .*/enable_vo                      = 0/' /userdata/rkipc.ini
setsid rkipc -a /oem/usr/share/iqfiles > /tmp/rkipc-app.log 2>&1 < /dev/null &
sleep 6

# 2) yolosrv：摄像头推理 + 20s 录制/保存/上传后端（判定窗由 UI 的触发文件控制）
cd "$APP_DIR"
# TTS 密钥（可选）：/root/yolov8s-pose/tts.env 里 ZHIPU_API_KEY=...
TTS_ARGS=""
if [ -f "$APP_DIR/tts.env" ]; then
  . "$APP_DIR/tts.env"
  [ -n "${ZHIPU_API_KEY:-}" ] && TTS_ARGS="-tts-api-key $ZHIPU_API_KEY -tts-cache-dir /userdata/fitness/tts"
fi
mkdir -p /userdata/fitness/tts
setsid ./yolosrv-new -model yolov8s_pose_416_w8a8.rknn -v4l2 /dev/video13 \
  -vw 1280 -vh 720 -frames 100000 -conf 0.4 -smooth 0.5 -rotate180 \
  -demo -session-file /tmp/fitness_session.trigger -session-seconds 20 \
  -movement air_squat -correction-fps 25 -jsonl /tmp/fitness_live.jsonl $TTS_ARGS 8080 \
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
ps aux | grep -E '[r]kipc -a|[y]olosrv-new|[l]uckfox_lvgl_demo' | awk '{print $11, $12}'
echo "--- wifi status ---"
cat /tmp/fitness_wifi_status.txt 2>/dev/null
