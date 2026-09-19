#!/bin/sh
pkill -f luckfox_lvgl_demo 2>/dev/null
sleep 1
cd /root/lvgl-app/build-native || exit 1
setsid ./luckfox_lvgl_demo > /tmp/lvgl-test.log 2>&1 < /dev/null &
sleep 6
echo "running: $(ps aux | grep -c '[l]uckfox_lvgl_demo')"
dd if=/dev/fb0 of=/tmp/fb4.raw bs=1228800 count=1 2>/dev/null
ls -la /tmp/fb4.raw
head -3 /tmp/lvgl-test.log
