#!/usr/bin/env python3
"""Board-only integration: a real isolated Wi-Fi-like client, DNS and HTTP.

AURA_NET_TEST=1 python3 -m unittest -v test_captive_network
Uses only temporary network namespaces; never changes wlan0 or its rules.
"""
import os
from pathlib import Path
import subprocess
import tempfile
import time
import unittest


@unittest.skipUnless(os.environ.get('AURA_NET_TEST') == '1', 'requires Linux root/netns')
class CaptiveNetworkTest(unittest.TestCase):
    def test_cached_http_and_external_dns_reach_portal(self):
        tag = str(os.getpid())
        router, client = 'cf-router-' + tag, 'cf-client-' + tag
        ap, sta = 'cfap' + tag, 'cfsta' + tag
        processes = []
        namespaces = []
        base = Path(__file__).resolve().parent
        def run(*args):
            return subprocess.run(args, capture_output=True, text=True, check=True, timeout=5)
        try:
            for name in (router, client):
                run('ip', 'netns', 'add', name)
                namespaces.append(name)
            run('ip', 'link', 'add', ap, 'type', 'veth', 'peer', 'name', sta)
            run('ip', 'link', 'set', ap, 'netns', router)
            run('ip', 'link', 'set', sta, 'netns', client)
            for name, iface, addr in ((router, ap, '192.168.4.1/24'), (client, sta, '192.168.4.2/24')):
                run('ip', '-n', name, 'addr', 'add', addr, 'dev', iface)
                run('ip', '-n', name, 'link', 'set', iface, 'up')
                run('ip', '-n', name, 'link', 'set', 'lo', 'up')
            run('ip', '-n', client, 'route', 'add', 'default', 'via', '192.168.4.1')
            with tempfile.TemporaryDirectory() as tmp:
                config = Path(tmp) / 'dns.conf'
                # Use the configuration actually installed by wifi_provision.
                config.write_bytes(Path('/etc/NetworkManager/dnsmasq-shared.d/chiform-captive.conf').read_bytes())
                for argv in ([ 'python3', str(base/'wifi_portal.py'), '192.168.4.1', '80'],
                             ['dnsmasq', '--no-daemon', '--no-hosts', '--bind-interfaces',
                              '--listen-address=192.168.4.1', '--conf-file='+str(config)]):
                    processes.append(subprocess.Popen(['ip','netns','exec',router]+argv,
                                     stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL))
                time.sleep(0.3)
                for process in processes:
                    self.assertIsNone(process.poll(), 'isolated portal/DNS did not start')
                probe = '''import http.client,sys
c=http.client.HTTPConnection(sys.argv[1],80,timeout=1)
c.request('GET','/generate_204',headers={'Host':'connectivitycheck.gstatic.com'})
r=c.getresponse();assert r.status==302,(r.status,r.read());assert r.getheader('Location')=='http://192.168.4.1/'
'''
                old = subprocess.run(['ip','netns','exec',client,'python3','-c',probe,'93.184.216.34'],
                                     capture_output=True, timeout=3)
                self.assertNotEqual(old.returncode, 0, 'baseline unexpectedly reaches external IP')
                run('ip','netns','exec',router,'python3',str(base/'wifi_redirect.py'),'start',ap,'192.168.4.1','80')
                for address in ('93.184.216.34','198.18.0.1','192.168.4.1'):
                    run('ip','netns','exec',client,'python3','-c',probe,address)
                dns = '''import socket,struct
q=struct.pack('!6H',123,256,1,0,0,0)+b'\\x07captive\\x05apple\\x03com\\0'+struct.pack('!2H',1,1)
s=socket.socket(socket.AF_INET,socket.SOCK_DGRAM);s.settimeout(1);s.sendto(q,('8.8.8.8',53));r=s.recv(1024)
assert r[3]&15==0;assert socket.inet_aton('198.18.0.1') in r
'''
                run('ip','netns','exec',client,'python3','-c',dns)
                page = '''import http.client
c=http.client.HTTPConnection('192.168.4.1',80,timeout=1);c.request('GET','/');r=c.getresponse();assert r.status==200;assert b'<form method=post ' in r.read()
'''
                run('ip','netns','exec',client,'python3','-c',page)
                apple = '''import http.client
c=http.client.HTTPConnection('198.18.0.1',80,timeout=1)
c.request('GET','/hotspot-detect.html',headers={'Host':'captive.apple.com','User-Agent':'CaptiveNetworkSupport/1.0 wispr'})
r=c.getresponse();assert r.status==200;body=r.read();assert b'<title>CHIFORM Wi-Fi</title>' in body;assert b'action="http://192.168.4.1/"' in body
'''
                run('ip','netns','exec',client,'python3','-c',apple)
                # Removal affects only our table and restores the original path.
                run('ip','netns','exec',router,'python3',str(base/'wifi_redirect.py'),'stop')
        finally:
            for process in processes:
                process.terminate()
            for process in processes:
                process.wait(timeout=3)
            for name in reversed(namespaces):
                subprocess.run(['ip','netns','del',name],capture_output=True)


if __name__ == '__main__':
    unittest.main()
