#!/usr/bin/env python3
"""Host-safe regression tests; all nmcli/systemd mutations use temporary fakes."""
import http.client
import os
import shutil
import socket
import time

from wifi_check import dns_query
from pathlib import Path
import subprocess
import tempfile
import threading
import unittest
from unittest.mock import patch

import wifi_portal as portal


class PortalTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.server = portal.ThreadingHTTPServer(('127.0.0.1', 0), portal.Handler)
        cls.worker = threading.Thread(target=cls.server.serve_forever, daemon=True)
        cls.worker.start()

    @classmethod
    def tearDownClass(cls):
        cls.server.shutdown()
        cls.server.server_close()
        cls.worker.join()

    def request(self, path, host, method='GET'):
        conn = http.client.HTTPConnection('127.0.0.1', self.server.server_port, timeout=2)
        conn.request(method, path, headers={'Host': host})
        response = conn.getresponse()
        result = response.status, dict(response.getheaders()), response.read()
        conn.close()
        return result

    def test_os_probes_and_unknown_hosts_redirect_without_scan(self):
        with patch.object(portal, 'scan', side_effect=AssertionError('probe must not scan')):
            for host, path in [('connectivitycheck.gstatic.com', '/generate_204'),
                               ('www.msftconnecttest.com', '/connecttest.txt'),
                               ('connect.rom.miui.com', '/unknown-probe')]:
                for method in ('GET', 'HEAD'):
                    code, headers, body = self.request(path, host, method)
                    self.assertEqual(code, 302)
                    self.assertEqual(headers['Location'], 'http://192.168.4.1/')
                    self.assertEqual(body, b'')
            code, _, body = self.request('/', '192.168.4.1')
            self.assertEqual(code, 200)
            self.assertIn(b'<form method=post ', body)
            self.assertEqual(self.request('/healthz', '127.0.0.1')[2], b'CHIFORM portal ready')

    def test_apple_probe_returns_setup_html_like_rv1106(self):
        with patch.object(portal, 'scan', side_effect=AssertionError('probe must not scan')):
            for host, path in [('captive.apple.com', '/hotspot-detect.html'),
                               ('www.apple.com', '/library/test/success.html'),
                               ('192.168.4.1', '/hotspot-detect.html')]:
                code, headers, body = self.request(path, host)
                self.assertEqual(code, 200)
                self.assertIn(b'<title>CHIFORM Wi-Fi</title>', body)
                self.assertIn(b'action="http://192.168.4.1/"', body)
                self.assertNotIn(b'<BODY>Success</BODY>', body)
                self.assertEqual(headers['Cache-Control'], 'no-store')
                head_code, head_headers, head_body = self.request(path, host, 'HEAD')
                self.assertEqual(head_code, 200)
                self.assertEqual(head_headers['Content-Length'], str(len(body)))
                self.assertEqual(head_body, b'')

    def test_scan_preserves_escaped_ssid_without_disrupting_ap(self):
        with patch.object(portal, 'sh', return_value='Office\\:2:89:WPA2\nOffice\\:2:70:WPA2\n') as command:
            self.assertEqual(portal.scan(), [{'ssid': 'Office:2', 'signal': 89, 'security': 'WPA2'}])
            self.assertIn('no', command.call_args.args)
            self.assertNotIn('yes', command.call_args.args)

    def test_disconnected_poll_preserves_setup_failure(self):
        with tempfile.TemporaryDirectory() as root:
            base = Path(root)
            status = base / 'status'
            failure = 'FAILED: cannot start setup page\n'
            status.write_text(failure)
            for name, body in {'nmcli': 'exit 0', 'ip': 'exit 0'}.items():
                command = base / name
                command.write_text('#!/bin/sh\n' + body + '\n')
                command.chmod(0o755)
            env = dict(os.environ, PATH=str(base)+':'+os.environ['PATH'],
                       WIFI_STATUS_FILE=str(status), WIFI_LOCK_FILE=str(base/'lock'))
            result = subprocess.run(['sh', str(Path(__file__).with_name('wifi_provision.sh')), 'status'],
                                    env=env, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(status.read_text(), failure)

    def test_failed_sta_restores_hotspot_under_set_e(self):
        with tempfile.TemporaryDirectory() as root:
            base = Path(root)
            log = base / 'commands'
            cred = base / 'wifi.conf'
            cred.write_text("WIFI_SSID='missing-network'\nWIFI_PASSWORD='test-only'\n")
            scripts = {
                'nmcli': 'echo "$*" >> "$TEST_LOG"\ncase "$*" in\n *"dev wifi connect"*) exit 10;;\n *"--active"*) exit 0;;\n esac\n',
                'systemctl': 'exit 0\n', 'pkill': 'exit 0\n',
                'sleep': 'exit 0\n', 'python3': 'exit 0\n',
                'flock': 'exit 0\n',
            }
            for name, body in scripts.items():
                f = base / name
                f.write_text('#!/bin/sh\n' + body)
                f.chmod(0o755)
            env = dict(os.environ, PATH=str(base)+':'+os.environ['PATH'], TEST_LOG=str(log),
                       WIFI_CREDENTIAL_FILE=str(cred), WIFI_STATUS_FILE=str(base/'status'),
                       WIFI_LOCK_FILE=str(base/'lock'), WIFI_DNSMASQ_DIR=str(base/'dns'),
                       WIFI_PORTAL_PID=str(base/'pid'))
            result = subprocess.run(['sh', str(Path(__file__).with_name('wifi_provision.sh')), 'sta'],
                                    env=env, capture_output=True, text=True, timeout=5)
            self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
            self.assertEqual(log.read_text().count('dev wifi connect'), 2)
            self.assertIn('con up CHIFORM-SETUP', log.read_text())
            self.assertIn('HOTSPOT READY', (base/'status').read_text())
            self.assertIn('no-resolv', (base/'dns/chiform-captive.conf').read_text())
            # Exercise dnsmasq against the generated production config with a
            # populated resolver file (the cold-boot condition that broke AAAA).
            if shutil.which('dnsmasq') and os.geteuid() == 0:
                resolver = base / 'resolv.conf'
                resolver.write_text('nameserver 8.8.8.8\n')
                with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
                    sock.bind(('127.0.0.1', 0))
                    port = sock.getsockname()[1]
                proc = subprocess.Popen([
                    'dnsmasq', '--no-daemon', '--no-hosts', '--bind-interfaces',
                    '--listen-address=127.0.0.1', '--port='+str(port),
                    '--conf-file='+str(base/'dns/chiform-captive.conf'),
                    '--resolv-file='+str(resolver)],
                    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                try:
                    time.sleep(0.2)
                    self.assertIsNone(proc.poll())
                    dns_query('127.0.0.1', 28, port)
                    dns_query('127.0.0.1', 65, port)
                finally:
                    proc.terminate()
                    proc.wait(timeout=2)


if __name__ == '__main__':
    unittest.main()
