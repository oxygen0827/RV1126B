#!/usr/bin/env python3
"""CHIFORM Wi-Fi 配网网页（Debian/NetworkManager 版）。

由 wifi_provision.sh portal 启动：python3 wifi_portal.py [AP_ADDR] [PORT]
- GET  /           配网页面（下拉扫描结果 + SSID/密码）
- GET  /api/scan   扫描结果 JSON（nmcli）
- GET  /api/status 连接状态
- POST /           保存并连接（异步调用 wifi_provision.sh sta）
"""
import html
import json
import os
import subprocess
import sys
import threading
import urllib.parse
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

AP_ADDR = sys.argv[1] if len(sys.argv) > 1 else "192.168.4.1"
PORT = int(sys.argv[2]) if len(sys.argv) > 2 else 80
IFACE = os.environ.get("WIFI_IFACE", "wlan0")
SCRIPT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "wifi_provision.sh")
CRED = "/userdata/fitness/wifi_last.conf"
SWITCH_LOCK = threading.Lock()


def sh(*args, timeout=20):
    try:
        return subprocess.run(args, capture_output=True, text=True, timeout=timeout).stdout
    except Exception:
        return ""


def scan():
    """nmcli 扫描；AP 模式下可能失败，失败返回空表（页面仍可手输）。"""
    out = sh("nmcli", "-t", "-f", "SSID,SIGNAL,SECURITY", "dev", "wifi", "list", "--rescan", "yes", timeout=25)
    if not out.strip():
        out = sh("nmcli", "-t", "-f", "SSID,SIGNAL,SECURITY", "dev", "wifi", "list", timeout=10)
    seen, nets = set(), []
    for line in out.splitlines():
        parts = line.split(":")
        if len(parts) < 3 or not parts[0]:
            continue
        ssid = parts[0].replace("\\:", ":")
        if ssid in seen:
            continue
        seen.add(ssid)
        try:
            signal = int(parts[1] or 0)
        except ValueError:
            signal = 0
        security = parts[2] or "open"
        nets.append({"ssid": ssid, "signal": signal, "security": security})
    nets.sort(key=lambda n: -n["signal"])
    return nets


def _sq(s):
    return "'" + s.replace("'", "'\\''") + "'"


def save_credentials(ssid, password):
    if not ssid or len(ssid) > 32 or "\n" in ssid or "\x00" in ssid:
        raise ValueError("SSID 无效")
    if len(password) > 64 or "\n" in password or "\x00" in password:
        raise ValueError("密码无效")
    os.makedirs(os.path.dirname(CRED), exist_ok=True)
    tmp = CRED + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        f.write("WIFI_SSID=%s\nWIFI_PASSWORD=%s\n" % (_sq(ssid), _sq(password)))
    os.chmod(tmp, 0o600)
    os.replace(tmp, CRED)


def switch_to_sta():
    """在独立 systemd 单元里执行切换：do_sta 会 stop 本 portal 服务，
    普通子进程会随服务 cgroup 一起被杀，必须隔离。"""
    log = open("/tmp/fitness_wifi_switch.log", "a")
    if os.path.exists("/usr/bin/systemd-run"):
        try:
            subprocess.Popen(
                ["systemd-run", "--unit=chiform-wifi-switch", "--collect", "--quiet",
                 "/bin/sh", SCRIPT, "sta"],
                stdout=log, stderr=subprocess.STDOUT,
            )
            return
        except Exception:
            pass
    subprocess.Popen(["/bin/sh", SCRIPT, "sta"], stdout=log, stderr=subprocess.STDOUT)


def status_line():
    sh(SCRIPT, "status", timeout=15)
    try:
        with open("/tmp/fitness_wifi_status.txt", encoding="utf-8") as f:
            return f.readline().strip()
    except OSError:
        return "IDLE"


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def reply(self, body, code=200, content_type="text/html; charset=utf-8"):
        data = body.encode("utf-8") if isinstance(body, str) else body
        self.send_response(code)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    # 手机/系统的强制门户探测路径：统一 302 到配网页
    CAPTIVE_PATHS = (
        "/generate_204", "/gen_204", "/hotspot-detect.html",
        "/connecttest.txt", "/ncsi.txt", "/redirect", "/canonical.html",
    )

    def do_GET(self):
        path = urllib.parse.urlsplit(self.path).path
        if path in self.CAPTIVE_PATHS:
            self.send_response(302)
            self.send_header("Location", "http://%s/" % AP_ADDR)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        if path == "/api/scan":
            self.reply(json.dumps(scan(), ensure_ascii=False), content_type="application/json; charset=utf-8")
            return
        if path == "/api/status":
            self.reply(json.dumps({"status": status_line()}, ensure_ascii=False),
                       content_type="application/json; charset=utf-8")
            return
        # 页面立即返回（探测请求/首访都要快）；扫描结果由前端异步拉取
        self.reply(
            """<!doctype html><meta name=viewport content='width=device-width,initial-scale=1'>
<title>CHIFORM Wi-Fi</title>
<style>body{font:18px sans-serif;max-width:480px;margin:40px auto;padding:0 18px;color:#19324d}
input,button{box-sizing:border-box;width:100%;padding:14px;margin:9px 0;font-size:18px}
button{background:#1677ff;color:white;border:0;border-radius:8px}
a{color:#1677ff}.muted{color:#8a99a8;font-size:14px}</style>
<h1>CHIFORM Wi-Fi</h1><p>选择扫描到的 2.4GHz Wi-Fi，也可以手动输入 SSID。</p>
<form method=post><input name=ssid list=wifi-list required maxlength=32 placeholder='Wi-Fi 名称'>
<datalist id=wifi-list></datalist>
<input name=password type=password maxlength=64
placeholder='密码（开放网络可留空）'><button type=submit>保存并连接</button></form>
<p class=muted id=scan-state>正在扫描…</p>
<p><a href='/'>重新扫描</a></p>
<script>
fetch('/api/scan').then(function(r){return r.json()}).then(function(list){
  var dl=document.getElementById('wifi-list');
  list.forEach(function(n){var o=document.createElement('option');o.value=n.ssid;dl.appendChild(o);});
  document.getElementById('scan-state').textContent = list.length? ('扫描到 '+list.length+' 个网络') : '未扫描到网络，可手动输入 SSID';
}).catch(function(){document.getElementById('scan-state').textContent='扫描失败，可手动输入 SSID';});
</script>"""
        )

    def do_POST(self):
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            length = 0
        if length <= 0 or length > 4096:
            self.reply("<h1>请求无效</h1>", 400)
            return
        raw = self.rfile.read(length).decode("utf-8", errors="replace")
        form = urllib.parse.parse_qs(raw, keep_blank_values=True)
        ssid = form.get("ssid", [""])[0]
        password = form.get("password", [""])[0]
        if not SWITCH_LOCK.acquire(blocking=False):
            self.reply("<h1>正在连接，请稍后</h1>", 409)
            return
        try:
            save_credentials(ssid, password)
        except (ValueError, OSError) as exc:
            SWITCH_LOCK.release()
            self.reply("<h1>配置无效</h1><p>%s</p>" % html.escape(str(exc)), 400)
            return
        try:
            self.reply("<h1>已保存</h1><p>正在连接 %s，请等待约 15 秒。</p>" % html.escape(ssid))
        finally:
            threading.Thread(target=switch_to_sta, daemon=True).start()
            SWITCH_LOCK.release()


if __name__ == "__main__":
    print("wifi portal listening on %s:%d" % (AP_ADDR, PORT))
    ThreadingHTTPServer(("0.0.0.0", PORT), Handler).serve_forever()
