#!/bin/sh
# Start the live squat-analysis pipeline: yolosrv (camera->pose) + bridge -> edge-stream
set -e
pkill -f 'yolosrv -model' 2>/dev/null || true
pkill -f bridge.py 2>/dev/null || true
pkill -f edge-stream 2>/dev/null || true
sleep 1

cd /root/yolov8s-pose
rm -f live2.jsonl
setsid ./yolosrv -model yolov8s_pose_416_w8a8.rknn -v4l2 /dev/video13 \
  -vw 1280 -vh 720 -frames 100000 -conf 0.4 -smooth 0.5 -rotate180 \
  -jsonl live2.jsonl 8080 > live2.log 2>&1 < /dev/null &
sleep 5

cd /root/chiform-trajectory
MODEL_HASH=828aca281b74914d14885123729614f87f665004a85eb34809898b05a96aac3c \
  setsid python3 bridge.py /root/yolov8s-pose/live2.jsonl 1280 720 \
  > /tmp/bridge.log 2>&1 < /dev/null &
sleep 3

echo "--- processes ---"
ps aux | grep -E 'yolosrv|bridge.py|edge-stream' | grep -v grep | awk '{print $11, $12, $13}'
echo "--- yolosrv tail ---"
tail -2 live2.log
echo "--- bridge log ---"
head -5 /tmp/bridge.log
