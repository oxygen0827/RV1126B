#!/bin/sh
# Host-side application deployment only; no partitions, DTB, or IQ tuning changes.
set -eu
BASE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
ROOT=$(CDPATH= cd -- "$BASE/.." && pwd)
BUILD=${AURA_BUILD_DIR:-/tmp/chiform-aura-build}
mkdir -p "$BUILD"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go -C "$ROOT/app/src" build -o "$BUILD/yolosrv-new" .
adb get-state
adb shell 'mkdir -p /root/chiform-backup-20260919; cp -an /root/yolov8s-pose/yolosrv-new /root/yolov8s-pose/run_fitness_app.sh /root/yolov8s-pose/stop_fitness_app.sh /root/yolov8s-pose/wifi_provision.sh /root/yolov8s-pose/wifi_portal.py /root/yolov8s-pose/wifi_status_loop.sh /root/chiform-backup-20260919/; systemctl stop chiform-fitness'
adb push "$BUILD/yolosrv-new" /root/yolov8s-pose/yolosrv-new
adb push "$BASE/run_fitness_app.sh" "$BASE/stop_fitness_app.sh" "$BASE/wifi_provision.sh" "$BASE/wifi_portal.py" "$BASE/prepare_wifi_portal.sh" "$BASE/wifi_check.py" "$BASE/wifi_redirect.py" "$BASE/wifi_status_loop.sh" /root/yolov8s-pose/
adb push "$BASE/chiform-isp.service" "$BASE/chiform-fitness.service" "$BASE/chiform-wifi-portal.service" /etc/systemd/system/
adb shell 'chmod 644 /etc/systemd/system/chiform-isp.service /etc/systemd/system/chiform-fitness.service /etc/systemd/system/chiform-wifi-portal.service; chmod 755 /root/yolov8s-pose/yolosrv-new /root/yolov8s-pose/wifi_provision.sh /root/yolov8s-pose/run_fitness_app.sh /root/yolov8s-pose/stop_fitness_app.sh; systemctl disable chiform-wifi-portal; systemctl daemon-reload; if nmcli con show CHIFORM-SETUP >/dev/null 2>&1; then nmcli con modify CHIFORM-SETUP connection.autoconnect no; fi; systemctl enable chiform-fitness; systemctl restart chiform-fitness; if systemctl is-active --quiet chiform-wifi-portal; then systemctl restart chiform-wifi-portal; fi'
