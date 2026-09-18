#!/usr/bin/env python3
"""Patch the Aura DTB: switch the DSI panel from the default 10.1" (800x1280)
to the Waveshare 3.5inch DSI LCD (E): 640x480 @ 24MHz, 1 lane, no init sequence."""
import re
import sys

src = "/var/folders/9g/k0tt7z_d47v7m7nslwjj36180000gn/T/opencode/current_board.dts"
dst = "/var/folders/9g/k0tt7z_d47v7m7nslwjj36180000gn/T/opencode/patched_board.dts"

dts = open(src, encoding="utf-8").read()

# 1. DSI lanes: 2 -> 1 (Waveshare 3.5E is a 1-lane panel per its RPi overlay)
old = "dsi,lanes = <0x02>;"
assert dts.count(old) == 1, f"lanes occurrences: {dts.count(old)}"
dts = dts.replace(old, "dsi,lanes = <0x01>;")

# 2. physical size 216x135mm -> 70x50mm
assert dts.count("width-mm = <0xd8>;") == 1
dts = dts.replace("width-mm = <0xd8>;", "width-mm = <0x46>;")
assert dts.count("height-mm = <0x87>;") == 1
dts = dts.replace("height-mm = <0x87>;", "height-mm = <0x32>;")

# 3. drop the 10.1" panel init sequence (3.5E needs none, like on the RPi)
dts, n = re.subn(r"\n\t\t\tpanel-init-sequence = <[^;]*>;", "", dts, count=1)
assert n == 1, "init sequence not found"

# 4. replace the timing with the Waveshare 3.5E one (from Waveshare_35DSI.dtbo, 35E)
old_timing = """\t\t\t\tdsi_timing0: timing0 {
\t\t\t\t\tclock-frequency = <0x42c1d80>;
\t\t\t\t\thactive = <0x320>;
\t\t\t\t\tvactive = <0x500>;
\t\t\t\t\tvsync-len = <0x04>;
\t\t\t\t\tvback-porch = <0x0a>;
\t\t\t\t\tvfront-porch = <0x1e>;
\t\t\t\t\thsync-len = <0x14>;
\t\t\t\t\thback-porch = <0x14>;
\t\t\t\t\thfront-porch = <0x28>;"""
new_timing = """\t\t\t\tdsi_timing0: timing0 {
\t\t\t\t\tclock-frequency = <0x16e3600>;
\t\t\t\t\thactive = <0x280>;
\t\t\t\t\tvactive = <0x1e0>;
\t\t\t\t\tvsync-len = <0x04>;
\t\t\t\t\tvback-porch = <0x0d>;
\t\t\t\t\tvfront-porch = <0x03>;
\t\t\t\t\thsync-len = <0x20>;
\t\t\t\t\thback-porch = <0x50>;
\t\t\t\t\thfront-porch = <0x30>;"""
assert old_timing in dts, "timing block not found"
dts = dts.replace(old_timing, new_timing)

# 5. this panel has no I2C backlight chip -> disable the ws-bl node
old = """\t\tws_bl: ws-bl@45 {
\t\t\tstatus = "okay";"""
new = """\t\tws_bl: ws-bl@45 {
\t\t\tstatus = "disabled";"""
assert old in dts, "ws-bl node not found"
dts = dts.replace(old, new)

open(dst, "w", encoding="utf-8").write(dts)
print("patched DTS written:", dst)
