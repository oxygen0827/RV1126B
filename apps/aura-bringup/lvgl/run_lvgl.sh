#!/bin/sh
# 快速测试 LVGL 应用显示（关闭 rkipc VO，跑 10 秒）
pkill rkipc 2>/dev/null
sleep 2
cd /root/lvgl-app/build-native
timeout 10 ./luckfox_lvgl_demo > /tmp/lvgl-run.log 2>&1
echo "exit: $?"
tail -15 /tmp/lvgl-run.log
