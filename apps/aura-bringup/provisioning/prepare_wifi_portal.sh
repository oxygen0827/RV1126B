#!/bin/sh
# Aura's legacy init glob also executes S50nginx.disabled. Move the factory
# web-server launcher outside that glob; preserve it for explicit rollback.
set -eu
INIT_DIR=/oem/usr/etc/init.d
for name in S50nginx S50nginx.disabled; do
 file="$INIT_DIR/$name"
 [ -f "$file" ] || continue
 destination="$INIT_DIR/chiform-disabled/$name"
 if [ -e "$destination" ]; then
  echo "portal: backup already exists: $destination" >&2
  exit 1
 fi
 mkdir -p "$INIT_DIR/chiform-disabled"
 mv "$file" "$destination"
done
# Only stop the vendor binary, not unrelated HTTP services.
if [ -x /oem/usr/sbin/nginx ]; then
 start-stop-daemon --stop --exec /oem/usr/sbin/nginx --retry TERM/3/KILL/1 --oknodo
fi
