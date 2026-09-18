#!/usr/bin/env python3
"""Build a patched boot.img: replace the FIT 'fdt' payload with the patched DTB
and update the FIT sha256 hash value."""
import hashlib

BASE = "/var/folders/9g/k0tt7z_d47v7m7nslwjj36180000gn/T/opencode/"
DTB_OFF = 0x800
DTB_SIZE = 0x2C850
OLD_HASH = bytes.fromhex("3cdf3f4d4b5d3e5d3c61d09bf7cd283d6e6ed6850f6c90ad7266b6eecb161cb4")

boot = bytearray(open(BASE + "imgcmp/Debian13/boot.img", "rb").read())
dtb = open(BASE + "patched_board.dtb", "rb").read()
assert len(dtb) <= DTB_SIZE, "dtb too large"
dtb_padded = dtb + b"\x00" * (DTB_SIZE - len(dtb))

# sanity: the old hash must match the old payload
old_payload = bytes(boot[DTB_OFF:DTB_OFF + DTB_SIZE])
assert hashlib.sha256(old_payload).digest() == OLD_HASH, "old hash mismatch"

# patch payload
boot[DTB_OFF:DTB_OFF + DTB_SIZE] = dtb_padded

# patch FIT hash value (find the old hash bytes in the FIT header area)
idx = boot.find(OLD_HASH, 0, DTB_OFF)
assert idx >= 0, "hash property not found in FIT header"
new_hash = hashlib.sha256(dtb_padded).digest()
boot[idx:idx + 32] = new_hash
print(f"hash patched at offset {hex(idx)}")
print("new sha256:", new_hash.hex())

open(BASE + "boot-patched.img", "wb").write(boot)
print("boot-patched.img written:", len(boot), "bytes")
