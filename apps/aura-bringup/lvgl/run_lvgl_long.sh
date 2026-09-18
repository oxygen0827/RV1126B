#!/bin/sh
pkill luckfox_lvgl_demo 2>/dev/null
pkill rkipc 2>/dev/null
sleep 2
cd /root/lvgl-app/build-native
setsid timeout 180 ./luckfox_lvgl_demo > /tmp/lvgl-run.log 2>&1 < /dev/null &
sleep 3
echo "running: $(ps aux | grep -c '[l]uckfox_lvgl_demo')"
head -5 /tmp/lvgl-run.log
