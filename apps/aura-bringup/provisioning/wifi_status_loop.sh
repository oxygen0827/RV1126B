#!/bin/sh
# One status implementation; preserve in-flight STARTING / CONNECTING states.
APP_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
while :; do
 sh "$APP_DIR/wifi_provision.sh" status
 sleep 3
done
