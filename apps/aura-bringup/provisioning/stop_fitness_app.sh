#!/bin/sh
# 停止健身应用栈（供 systemd ExecStop 调用）
pkill -f luckfox_lvgl_demo 2>/dev/null
pkill -x yolosrv-new 2>/dev/null
pkill -f wifi_status_loop.sh 2>/dev/null
pkill rkipc 2>/dev/null
systemctl stop chiform-isp 2>/dev/null
sleep 1
echo "fitness app stopped"
