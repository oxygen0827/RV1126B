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
STA_CON=${WIFI_STA_CON:-chiform-sta}
STATUS=${WIFI_STATUS_FILE:-/tmp/fitness_wifi_status.txt}
PORTAL_PORT=${WIFI_PORTAL_PORT:-80}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
PORTAL_PY=$SCRIPT_DIR/wifi_portal.py
PORTAL_LOG=/tmp/fitness_wifi_portal.log
PORTAL_PID=/tmp/fitness_wifi_portal.pid
SITE=${WIFI_SITE:-/root/yolov8s-pose}
LAST_CRED=/userdata/fitness/wifi_last.conf

log_status() { printf '%s\n' "$*" >"$STATUS"; echo "$*"; }

ensure_nm() {
	command -v nmcli >/dev/null 2>&1 || { log_status "FAILED: nmcli missing"; exit 1; }
}

ap_down() {
	nmcli con down "$AP_CON" >/dev/null 2>&1 || true
}

ap_up() {
	# 无密码开放热点；NM shared 模式自带 DHCP/DNS（依赖 dnsmasq）
	# 重建为开放热点（nmcli 的 key-mgmt none 会被当成 WEP，必须移除安全段）
	nmcli con delete "$AP_CON" >/dev/null 2>&1 || true
	nmcli con add type wifi ifname "$IFACE" con-name "$AP_CON" autoconnect no ssid "$AP_SSID" >/dev/null
	nmcli con modify "$AP_CON" \
		802-11-wireless.mode ap 802-11-wireless.band bg \
		ipv4.method shared ipv4.addresses "$AP_ADDR/24" ipv6.method disabled >/dev/null
	nmcli con modify "$AP_CON" remove 802-11-wireless-security >/dev/null 2>&1 || true
	# 强制门户：所有域名解析到配网页，手机连上后自动弹"登录网络"
	DNSMASQ_SHARED=/etc/NetworkManager/dnsmasq-shared.d
	mkdir -p "$DNSMASQ_SHARED" 2>/dev/null || true
	printf 'address=/#/%s\n' "$AP_ADDR" > "$DNSMASQ_SHARED/chiform-captive.conf" 2>/dev/null || true
	nmcli con up "$AP_CON" >/dev/null
	# 部分内核/驱动下 NM shared 模式不下发地址（或下发后被后续阶段冲掉）：
	# 等 NM 激活流程完全结束，再补地址并复查稳定性。
	i=0
	while [ "$i" -lt 25 ]; do
		st=$(nmcli -t -f GENERAL.STATE con show "$AP_CON" 2>/dev/null | cut -d: -f2)
		case "$st" in activated*) break ;; esac
		sleep 1
		i=$((i + 1))
	done
	sleep 8
	i=0
	while [ "$i" -lt 8 ]; do
		if ip -4 addr show "$IFACE" 2>/dev/null | grep -q "inet $AP_ADDR/"; then
			sleep 2
			if ip -4 addr show "$IFACE" 2>/dev/null | grep -q "inet $AP_ADDR/"; then
				break
			fi
		fi
		ip addr add "$AP_ADDR/24" dev "$IFACE" 2>/dev/null || true
		sleep 2
		i=$((i + 1))
	done
}

portal_up() {
	# 优先 systemd 托管（restart 自带旧实例清理与端口释放）
	if command -v systemctl >/dev/null 2>&1; then
		systemctl restart chiform-wifi-portal >/dev/null 2>&1 || true
		sleep 2
		systemctl is-active --quiet chiform-wifi-portal && return 0
		pkill -f wifi_portal.py 2>/dev/null || true
		sleep 1
		systemctl start chiform-wifi-portal >/dev/null 2>&1 || true
		sleep 1
	fi
	setsid python3 "$PORTAL_PY" "$AP_ADDR" "$PORTAL_PORT" >"$PORTAL_LOG" 2>&1 < /dev/null &
	echo $! >"$PORTAL_PID"
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
	log_status "STARTING: hotspot $AP_SSID"
	# 断开当前 STA，避免 AP+STA 冲突
	for c in $(nmcli -t -f NAME,DEVICE con show --active 2>/dev/null | awk -F: -v d="$IFACE" '$2==d{print $1}'); do
		nmcli con down "$c" >/dev/null 2>&1 || true
	done
	ap_down
	if ! ap_up; then
		log_status "FAILED: cannot start hotspot"
		exit 1
	fi
	sleep 2
	portal_up
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
	while [ "$i" -lt 4 ]; do
		if [ -n "${WIFI_PASSWORD:-}" ]; then
			out=$(nmcli dev wifi connect "$WIFI_SSID" password "$WIFI_PASSWORD" ifname "$IFACE" 2>&1)
		else
			out=$(nmcli dev wifi connect "$WIFI_SSID" ifname "$IFACE" 2>&1)
		fi
		rc=$?
		echo "sta try $i rc=$rc: $out"
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
	line=$(nmcli -t -f DEVICE,STATE,CONNECTION dev 2>/dev/null | awk -F: -v d="$IFACE" '$1==d{print $2" "$3}')
	IP=$(ip -4 addr show "$IFACE" 2>/dev/null | awk '/inet /{print $2; exit}')
	if nmcli -t -f NAME con show --active 2>/dev/null | grep -qx "$AP_CON"; then
		log_status "HOTSPOT READY: join $AP_SSID -> http://$AP_ADDR"
	elif [ -n "$IP" ]; then
		log_status "CONNECTED: ${line:-$IFACE} $IP"
	else
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
