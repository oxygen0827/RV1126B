#!/bin/sh
# 全链路回归：录制(5s) -> 本地保存 -> 云上传
pkill -f 'yolosrv-new' 2>/dev/null
sleep 1
rm -f /root/yolov8s-pose/upload/* /tmp/fitness_*.trigger /tmp/fitness_local_*.txt 2>/dev/null
rm -rf /userdata/fitness/sessions/* 2>/dev/null
cd /root/yolov8s-pose
setsid ./yolosrv-new -model yolov8s_pose_416_w8a8.rknn -v4l2 /dev/video13 \
  -vw 1280 -vh 720 -frames 100000 -conf 0.4 -smooth 0.5 -rotate180 \
  -demo -session-file /tmp/fitness_session.trigger -session-seconds 5 \
  -movement air_squat -correction-fps 25 -jsonl /tmp/fitness_live.jsonl 8080 \
  > /tmp/fitness_e2e.log 2>&1 < /dev/null &
sleep 8
echo "=== 1) record 5s ==="
date +%s > /tmp/fitness_session.trigger
sleep 7
ls -la /root/yolov8s-pose/upload/ | grep -E "h264|pose|meta"

echo "=== 2) local save ==="
touch /tmp/fitness_save.trigger
sleep 12
cat /tmp/fitness_local_ok.txt 2>/dev/null || cat /tmp/fitness_local_err.txt 2>/dev/null
find /userdata/fitness/sessions -type f 2>/dev/null | head -4

echo "=== 3) cloud upload ==="
touch /tmp/fitness_upload.trigger
sleep 25
cat /root/yolov8s-pose/upload/status_ok.txt 2>/dev/null || cat /root/yolov8s-pose/upload/status_err.txt 2>/dev/null
echo
echo "=== log tail ==="
tail -4 /root/yolov8s-pose/upload/upload.log
