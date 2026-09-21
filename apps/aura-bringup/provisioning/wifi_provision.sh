#!/bin/sh
# CHIFORM 免烧录配网（Debian 13 / NetworkManager 版）
# 用法: wifi_provision.sh {portal|sta|status|sync-time|stop}
#   portal    停 STA，起 CHIFORM-SETUP 开放热点(192.168.4.1) + 网页配网
#   sta       连接已保存的 Wi-Fi（失败自动回退热点）
#   status    输出状态行（写 /tmp/fitness_wifi_status.txt，LVGL 前端读取）
#   sync-time 无 RTC 时校时（UDP NTP / HTTP Date 兜底）
set -eu

IFACE=${WIFI_IFACE:-wlan0}
AP_SSID=${WIFI_AP_SSID:-CHIFORM-SETUP}
AP_ADDR=${WIFI_AP_ADDR:-192.168.4.1}
AP_CON=${AP_SSID}
# Reserved benchmarking address, used only behind the hotspot DNAT. Avoid
# RFC1918 answers which some Android versions treat as no-internet DNS.
export WIFI_PROBE_ADDR=${WIFI_PROBE_ADDR:-198.18.0.1}
STATUS=${WIFI_STATUS_FILE:-/tmp/fitness_wifi_status.txt}
PORTAL_PORT=${WIFI_PORTAL_PORT:-80}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PORTAL_PID=${WIFI_PORTAL_PID:-/tmp/fitness_wifi_portal.pid}
LAST_CRED=${WIFI_CREDENTIAL_FILE:-/userdata/fitness/wifi_last.conf}
LOCK=${WIFI_LOCK_FILE:-/run/chiform-wifi.lock}
export LC_ALL=C

# Serialize button presses / POST switches; status must not overwrite transitions.
case "${1:-status}" in
 portal|sta|stop)
  exec 9>"$LOCK"
  flock -w 3 9 || { cat "$STATUS" 2>/dev/null || true; exit 0; }
  ;;
esac

log_status() { printf '%s\n' "$*" >"$STATUS"; echo "$*"; }

ensure_nm() {
	command -v nmcli >/dev/null 2>&1 || { log_status "FAILED: nmcli missing"; exit 1; }
}

ap_down() {
	python3 "$SCRIPT_DIR/wifi_redirect.py" stop || return 1
	nmcli con down "$AP_CON" >/dev/null 2>&1 || true
}

ap_up() {
 # Reuse the profile: repeated presses must not drop an already healthy AP.
 DNSMASQ_SHARED=${WIFI_DNSMASQ_DIR:-/etc/NetworkManager/dnsmasq-shared.d}
 mkdir -p "$DNSMASQ_SHARED"
 printf 'address=/#/%s\nlocal=/#/\nno-resolv\ndhcp-option=3,%s\ndhcp-option=6,%s\n' "$WIFI_PROBE_ADDR" "$AP_ADDR" "$AP_ADDR" > "$DNSMASQ_SHARED/chiform-captive.conf"
 if ! nmcli con show "$AP_CON" >/dev/null 2>&1; then
  nmcli con add type wifi ifname "$IFACE" con-name "$AP_CON" autoconnect no ssid "$AP_SSID" >/dev/null || return 1
 fi
 nmcli con modify "$AP_CON" \
  connection.autoconnect no \
  802-11-wireless.mode ap 802-11-wireless.band bg 802-11-wireless.channel 6 \
  ipv4.method shared ipv4.addresses "$AP_ADDR/24" ipv6.method disabled >/dev/null || return 1
 nmcli con modify "$AP_CON" remove 802-11-wireless-security >/dev/null 2>&1 || true
 nmcli --wait 12 con up "$AP_CON" ifname "$IFACE" >/dev/null || return 1
 python3 "$SCRIPT_DIR/wifi_redirect.py" start "$IFACE" "$AP_ADDR" "$PORTAL_PORT" || return 1
 # No fixed sleeps or IP repairs: NM is the sole owner of this interface.
 python3 "$SCRIPT_DIR/wifi_check.py" "$AP_ADDR" "$PORTAL_PORT" || return 1
}

portal_up() {
 # Start HTTP before Wi-Fi association/DHCP so the very first OS probe succeeds.
 systemctl start chiform-wifi-portal || return 1
 python3 "$SCRIPT_DIR/wifi_check.py" 127.0.0.1 "$PORTAL_PORT" --http-only
}

portal_down() {
	if command -v systemctl >/dev/null 2>&1; then
		systemctl stop chiform-wifi-portal >/dev/null 2>&1 || true
	fi
	pkill -f wifi_portal.py 2>/dev/null || true
	rm -f "$PORTAL_PID"
}

do_portal() {
 ensure_nm
 # Apply to reused/legacy profiles as well as newly created ones.
 if nmcli con show "$AP_CON" >/dev/null 2>&1; then
  nmcli con modify "$AP_CON" connection.autoconnect no || return 1
 fi
 log_status "STARTING: hotspot $AP_SSID"
 if ! portal_up; then
  systemctl stop chiform-wifi-portal >/dev/null 2>&1 || true
  log_status "FAILED: cannot start setup page"
  return 1
 fi
 # Install before checking an existing AP too (upgrade without disconnect).
 python3 "$SCRIPT_DIR/wifi_redirect.py" start "$IFACE" "$AP_ADDR" "$PORTAL_PORT" || {
  log_status "FAILED: hotspot redirect unavailable"; return 1;
 }
 if nmcli -t -f NAME con show --active | grep -Fxq "$AP_CON" &&
    python3 "$SCRIPT_DIR/wifi_check.py" "$AP_ADDR" "$PORTAL_PORT" --once; then
  log_status "HOTSPOT READY: join $AP_SSID -> http://$AP_ADDR"
  return 0
 fi
 # NM activation handles the STA -> AP transition without a second disconnect.
 if ! ap_up; then
  log_status "FAILED: hotspot DNS/DHCP/page not ready"
  return 1
 fi
 log_status "HOTSPOT READY: join $AP_SSID -> http://$AP_ADDR"
}

do_sta() {
	ensure_nm
	[ -f "$LAST_CRED" ] || { log_status "FAILED: no saved wifi"; exit 1; }
	# shellcheck disable=SC1090
	. "$LAST_CRED"   # WIFI_SSID / WIFI_PASSWORD
	log_status "CONNECTING: $WIFI_SSID"
	portal_down
	ap_down
	# 旧 profile 可能残留（key-mgmt 缺失等）导致连接失败，先清掉重建
	nmcli con delete "$WIFI_SSID" >/dev/null 2>&1 || true
	# 刚退出热点模式时驱动需要时间稳定，重试几次
	sleep 2
	ok=0
	i=0
	while [ "$i" -lt 2 ]; do
		if [ -n "${WIFI_PASSWORD:-}" ]; then
			if out=$(nmcli --wait 15 dev wifi connect "$WIFI_SSID" password "$WIFI_PASSWORD" ifname "$IFACE" 2>&1); then rc=0; else rc=$?; fi
		else
			if out=$(nmcli --wait 15 dev wifi connect "$WIFI_SSID" ifname "$IFACE" 2>&1); then rc=0; else rc=$?; fi
		fi
		echo "sta try $i rc=$rc"
		if [ "$rc" -eq 0 ]; then
			ok=1
			break
		fi
		nmcli con delete "$WIFI_SSID" >/dev/null 2>&1 || true
		sleep 4
		i=$((i + 1))
	done
	if [ "$ok" -eq 1 ]; then
		sleep 2
		if nmcli -t -f DEVICE,STATE dev | grep -q "^$IFACE:connected"; then
			IP=$(ip -4 addr show "$IFACE" 2>/dev/null | awk '/inet /{print $2; exit}')
			log_status "CONNECTED: $WIFI_SSID $IP"
			sh "$0" sync-time >/dev/null 2>&1 || true
			exit 0
		fi
	fi
	log_status "FAILED: $WIFI_SSID; restoring hotspot"
	do_portal
	exit 1
}

do_status() {
	# A transition holds fd 9; keep its STARTING/CONNECTING message visible.
	exec 8>"$LOCK"
	if ! flock -n 8; then cat "$STATUS" 2>/dev/null || true; return; fi
	line=$(nmcli -t -f DEVICE,STATE,CONNECTION dev 2>/dev/null | awk -F: -v d="$IFACE" '$1==d{print $2" "$3}')
	IP=$(ip -4 addr show "$IFACE" 2>/dev/null | awk '/inet /{print $2; exit}')
	if nmcli -t -f NAME con show --active 2>/dev/null | grep -Fxq "$AP_CON"; then
		if ! python3 "$SCRIPT_DIR/wifi_check.py" "$AP_ADDR" "$PORTAL_PORT" --once; then
			log_status "FAILED: hotspot services unavailable"; return
		fi
		log_status "HOTSPOT READY: join $AP_SSID -> http://$AP_ADDR"
	elif [ -n "$IP" ]; then
		log_status "CONNECTED: ${line:-$IFACE} $IP"
	else
		# Keep the setup failure visible until retry/stop or an actual connection.
		if [ -r "$STATUS" ] && grep -q '^FAILED:' "$STATUS"; then
			cat "$STATUS"; return
		fi
		log_status "IDLE: ${line:-$IFACE}"
	fi
}

sync_clock() {
	year=$(date +%Y 2>/dev/null || echo 0)
	case "$year" in ''|*[!0-9]*) year=0 ;; esac
	[ "$year" -ge 2024 ] && { echo "clock ok ($year)"; return 0; }
	python3 - <<'PY' || true
import socket, struct, sys, time, urllib.request

def set_time(ts):
    import subprocess
    subprocess.run(["date", "-s", "@%d" % int(ts)], check=False)

# 1) UDP NTP（不占 123 端口绑定，客户端 socket）
for host in ("ntp.aliyun.com", "ntp1.aliyun.com", "time.cloud.tencent.com"):
    try:
        addr = socket.getaddrinfo(host, 123, socket.AF_INET, socket.SOCK_DGRAM)[0][4]
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.settimeout(2.5)
        pkt = b'\x1b' + 47 * b'\0'
        s.sendto(pkt, addr)
        data, _ = s.recvfrom(1024)
        s.close()
        if len(data) >= 48:
            t = struct.unpack("!12I", data[:48])[10] - 2208988800
            # 热点/内网劫持会给出错误时间，只接受合理值
            if t > 1700000000:
                set_time(t)
                print("ntp ok via", host)
                sys.exit(0)
    except Exception:
        continue

# 2) HTTP Date 头兜底
for url in ("http://www.baidu.com", "https://www.taobao.com", "http://connect.rom.miui.com/generate_204"):
    try:
        req = urllib.request.Request(url, method="HEAD")
        with urllib.request.urlopen(req, timeout=3) as r:
            d = r.headers.get("Date")
        if d:
            ts = time.mktime(time.strptime(d, "%a, %d %b %Y %H:%M:%S %Z"))
            if ts > 1700000000:
                set_time(ts)
                print("http date ok via", url)
                sys.exit(0)
    except Exception:
        continue
print("clock sync failed")
sys.exit(1)
PY
}

case "${1:-status}" in
	portal)    do_portal ;;
	sta)       do_sta ;;
	status)    do_status ;;
	sync-time) sync_clock ;;
	stop)      portal_down; ap_down; log_status "STOPPED" ;;
	*)
		echo "usage: $0 {portal|sta|status|sync-time|stop}" >&2
		exit 2
		;;
esac
