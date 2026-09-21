#!/usr/bin/env python3
"""Bounded readiness check: HTTP and both DNS query families on the actual AP."""
import http.client
import os
import socket
import struct
import sys
import time


def dns_query(addr, qtype, port=53):
    # A synthetic hostname prevents a cached upstream answer masking captive DNS.
    name = b"\x07chiform\x05probe\x04test\0"
    packet = struct.pack("!6H", 0xCAFE, 0x100, 1, 0, 0, 0) + name + struct.pack("!2H", qtype, 1)
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.settimeout(0.5)
        sock.sendto(packet, (addr, port))
        reply, _ = sock.recvfrom(1024)
    if len(reply) < 12:
        raise ValueError("short DNS reply")
    ident, flags, _, answers, _, _ = struct.unpack("!6H", reply[:12])
    if ident != 0xCAFE or flags & 15:
        raise ValueError("DNS failed")
    if qtype == 1 and (answers < 1 or socket.inet_aton(os.environ.get("WIFI_PROBE_ADDR", "198.18.0.1")) not in reply[12:]):
        raise ValueError("DNS does not point at portal")
    if qtype != 1 and answers != 0:
        raise ValueError("unexpected non-A answer")


def check(addr, port, http_only=False):
    conn = http.client.HTTPConnection(addr, port, timeout=0.5)
    try:
        conn.request("GET", "/healthz")
        response = conn.getresponse()
        if response.status != 200 or response.read() != b"CHIFORM portal ready":
            raise ValueError("wrong HTTP service")
    finally:
        conn.close()
    if not http_only:
        dns_query(addr, 1)
        dns_query(addr, 28)
        dns_query(addr, 65)  # Modern clients may ask for HTTPS service records too.


if __name__ == "__main__":
    deadline = time.monotonic() + (0 if "--once" in sys.argv else 4)
    while True:
        try:
            check(sys.argv[1], int(sys.argv[2]), "--http-only" in sys.argv)
            break
        except (OSError, ValueError, http.client.HTTPException) as exc:
            if time.monotonic() >= deadline:
                print("portal not ready:", exc, file=sys.stderr)
                sys.exit(1)
            time.sleep(0.1)
