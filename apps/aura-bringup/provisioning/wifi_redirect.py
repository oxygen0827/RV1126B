#!/usr/bin/env python3
"""Hotspot-only DNS/HTTP DNAT using the board's libnftables (no nft CLI needed).

Do not intercept TLS. Only this application's table is changed, atomically.
"""
import ctypes
import ipaddress
import json
import re
import sys

TABLE = 'chiform_portal'


class Nft:
    def __init__(self):
        self.lib = lib = ctypes.CDLL('libnftables.so.1')
        lib.nft_ctx_new.argtypes = [ctypes.c_uint]
        lib.nft_ctx_new.restype = ctypes.c_void_p
        for name in ('nft_ctx_buffer_output', 'nft_ctx_buffer_error', 'nft_ctx_free'):
            getattr(lib, name).argtypes = [ctypes.c_void_p]
        for name in ('nft_ctx_get_output_buffer', 'nft_ctx_get_error_buffer'):
            getattr(lib, name).argtypes = [ctypes.c_void_p]
            getattr(lib, name).restype = ctypes.c_char_p
        lib.nft_run_cmd_from_buffer.argtypes = [ctypes.c_void_p, ctypes.c_char_p]
        lib.nft_run_cmd_from_buffer.restype = ctypes.c_int

    def run(self, command, check=True):
        lib = self.lib
        ctx = lib.nft_ctx_new(0)
        if not ctx:
            raise RuntimeError('cannot allocate nft context')
        try:
            lib.nft_ctx_buffer_output(ctx)
            lib.nft_ctx_buffer_error(ctx)
            code = lib.nft_run_cmd_from_buffer(ctx, command.encode())
            output = (lib.nft_ctx_get_output_buffer(ctx) or b'').decode()
            error = (lib.nft_ctx_get_error_buffer(ctx) or b'').decode()
            if code and check:
                raise RuntimeError(error.strip())
            return code, output
        finally:
            lib.nft_ctx_free(ctx)


def configure(action, iface='wlan0', address='192.168.4.1', port=80):
    if not re.fullmatch(r'[A-Za-z0-9_.-]{1,15}', iface):
        raise ValueError('invalid interface')
    address = str(ipaddress.IPv4Address(address))
    port = int(port)
    if not 1 <= port <= 65535:
        raise ValueError('invalid port')
    nft = Nft()
    exists = nft.run('list table ip ' + TABLE, check=False)[0] == 0
    if action == 'stop':
        if exists:
            nft.run('delete table ip ' + TABLE)
        return
    if action != 'start':
        raise ValueError('expected start or stop')
    quoted_iface = json.dumps(iface)
    rules = ('delete table ip ' + TABLE + '\n') if exists else ''
    rules += f'''table ip {TABLE} {{
 chain prerouting {{
  type nat hook prerouting priority dstnat; policy accept;
  iifname {quoted_iface} udp dport 53 counter dnat to {address}:53
  iifname {quoted_iface} tcp dport 53 counter dnat to {address}:53
  iifname {quoted_iface} tcp dport 80 counter dnat to {address}:{port}
 }}
}}
'''
    nft.run(rules)


if __name__ == '__main__':
    try:
        configure(*sys.argv[1:])
    except (OSError, ValueError, RuntimeError) as exc:
        print('hotspot redirect:', exc, file=sys.stderr)
        sys.exit(1)
