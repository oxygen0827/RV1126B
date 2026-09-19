#!/bin/sh
# Wi-Fi 状态文件周期刷新（LVGL 右上角图标颜色用）
APP_DIR=/root/yolov8s-pose
STATUS=/tmp/fitness_wifi_status.txt

refresh() {
  st=$(nmcli -t -f DEVICE,STATE,CONNECTION dev 2>/dev/null | awk -F: '$1=="wlan0"{print $2" "$3}')
  ip=$(ip -4 addr show wlan0 2>/dev/null | awk '/inet /{print $2; exit}')
  if nmcli -t -f NAME con show --active 2>/dev/null | grep -qx CHIFORM-SETUP; then
    printf 'HOTSPOT READY: join CHIFORM-SETUP -> http://192.168.4.1\n' > "$STATUS"
  elif [ -n "$ip" ]; then
    printf 'CONNECTED: %s %s\n' "${st:-wlan0}" "$ip" > "$STATUS"
  else
    printf 'IDLE: %s\n' "${st:-wlan0}" > "$STATUS"
  fi
}

while :; do
  refresh
  sleep 5
done
