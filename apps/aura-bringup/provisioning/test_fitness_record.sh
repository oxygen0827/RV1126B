#!/bin/sh
# 测试健身录制链路：启动 demo 模式 -> 触发会话 -> 检查产物
pkill -f 'yolosrv-new' 2>/dev/null
sleep 1
rm -f /root/yolov8s-pose/upload/* /tmp/fitness_session.trigger 2>/dev/null
cd /root/yolov8s-pose
setsid ./yolosrv-new -model yolov8s_pose_416_w8a8.rknn -v4l2 /dev/video13 \
  -vw 1280 -vh 720 -frames 100000 -conf 0.4 -smooth 0.5 -rotate90 \
  -demo -session-file /tmp/fitness_session.trigger -session-seconds 5 \
  -movement air_squat -correction-fps 25 -jsonl /tmp/fitness_live.jsonl 8080 \
  > /tmp/fitness_test.log 2>&1 < /dev/null &
sleep 8
echo "=== trigger session ==="
date +%s > /tmp/fitness_session.trigger
sleep 8
echo "=== upload dir ==="
ls -la /root/yolov8s-pose/upload/
echo "=== meta ==="
head -c 600 /root/yolov8s-pose/upload/session_meta.json 2>/dev/null; echo
echo "=== log tail ==="
tail -6 /tmp/fitness_test.log
