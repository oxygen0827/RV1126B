#!/bin/sh
# DSI preview: yolosrv MJPEG (640x360) -> letterbox to 640x480 -> kmssink
pkill -f 'gst-launch-1.0' 2>/dev/null || true
sleep 1
setsid gst-launch-1.0 -q souphttpsrc location=http://127.0.0.1:8080/stream is-live=true ! \
  multipartdemux ! jpegdec ! videoconvert ! videobox top=-60 bottom=-60 ! \
  videoconvert ! queue ! fbdevsink > /tmp/preview.log 2>&1 < /dev/null &
sleep 4
echo "--- preview log ---"
head -6 /tmp/preview.log
echo "--- gst process ---"
ps aux | grep gst-launch | grep -v grep | wc -l
