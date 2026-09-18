#!/usr/bin/env python3
"""Bridge: yolosrv JSONL pose stream -> chiform edge-stream (contract 0.2).

Usage: bridge.py <yolosrv.jsonl> [src_w] [src_h]

- Tails the yolosrv JSONL (written live by yolosrv -jsonl).
- Emits header + frame lines (COCO-17 normalized, [x,y,null,score]) to edge-stream.
- Prints compact feedback lines and logs all events to /tmp/edge-events.jsonl.
"""
import json
import os
import subprocess
import sys
import threading
import time

POSE_JSONL = sys.argv[1] if len(sys.argv) > 1 else "/root/yolov8s-pose/live.jsonl"
SRC_W = int(sys.argv[2]) if len(sys.argv) > 2 else 1280
SRC_H = int(sys.argv[3]) if len(sys.argv) > 3 else 720

ROOT = "/root/chiform-trajectory"
MODEL_HASH = os.environ.get("MODEL_HASH", "0" * 64)
BASE_HASH = "0bfc63696ffff95a4d5530aab4a333d96b19781df2018e5708dbb2516e8f8507"
FPS = float(os.environ.get("SRC_FPS", "30"))
REPLAY = os.environ.get("REPLAY") == "1"

edge = subprocess.Popen(
    [f"{ROOT}/edge-stream", "-config", f"{ROOT}/config/trajectory.yaml",
     "-action_config", f"{ROOT}/config/actions/squat.yaml"],
    stdin=subprocess.PIPE, stdout=subprocess.PIPE,
    stderr=open("/tmp/edge-stderr.log", "w"), text=True, bufsize=1)

evf = open("/tmp/edge-events.jsonl", "w")


def reader():
    for line in edge.stdout:
        evf.write(line)
        evf.flush()
        try:
            e = json.loads(line)
        except Exception:
            continue
        t = e.get("type")
        if t == "ready":
            print("[ready] config=%s precision=%s" % (
                e.get("config_version"), e.get("model_context", {}).get("model_precision")), flush=True)
        elif t == "trajectory":
            u = e.get("update", {})
            for r in u.get("rep_updates") or []:
                print("[rep %s] %s..%s status=%s codes=%s" % (
                    r.get("rep_id"), r.get("start_ms"), r.get("end_ms"),
                    r.get("assessment_status"), r.get("static_codes")), flush=True)
            for r in u.get("new_reps") or []:
                print("[new_rep] %s" % json.dumps(r, ensure_ascii=False)[:300], flush=True)
        elif t == "trajectory_final":
            res = e.get("result", {})
            print("[final] reason=%s live_reps=%d" % (e.get("reason"), len(res.get("live_reps", []))), flush=True)
        elif t == "error":
            print("[error] %s" % json.dumps(e, ensure_ascii=False)[:300], flush=True)


threading.Thread(target=reader, daemon=True).start()

header = {
    "kind": "header", "contract_version": "0.2", "action": "squat", "view": 0,
    "model_version": "yolov8s-pose-416-int8.rknn@sha256:" + MODEL_HASH,
    "model_family": "yolov8s",
    "base_model_version": "yolov8s-pose.onnx@sha256:" + BASE_HASH,
    "model_precision": "int8",
    "src_width": SRC_W, "src_height": SRC_H, "fps": FPS,
    "coord": {"xy": "normalized[0,1]", "z": None},
}
edge.stdin.write(json.dumps(header) + "\n")
edge.stdin.flush()
print("[header] sent", flush=True)

start = time.time()
pos = 0
last_evt = -1
while True:
    try:
        size = os.path.getsize(POSE_JSONL)
    except OSError:
        time.sleep(0.1)
        continue
    if size < pos:
        pos = 0
    if size <= pos:
        if REPLAY:
            break
        time.sleep(0.03)
        continue
    with open(POSE_JSONL, "r") as f:
        f.seek(pos)
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                d = json.loads(line)
            except Exception:
                continue
            if REPLAY:
                now_ms = int(d.get("frame", 0) * 1000 / FPS)
            else:
                now_ms = int((time.time() - start) * 1000)
            if now_ms <= last_evt:
                now_ms = last_evt + 1
            last_evt = now_ms
            persons = d.get("persons") or []
            targets = []
            if persons:
                p = max(persons, key=lambda x: x.get("score", 0))
                kps = []
                for kp in p["kp"]:
                    x, y, s = kp[0] / SRC_W, kp[1] / SRC_H, kp[2]
                    if not (0.0 <= x <= 1.0 and 0.0 <= y <= 1.0 and 0.0 <= s <= 1.0):
                        kps.append([None, None, None, None])
                    else:
                        kps.append([x, y, None, s])
                targets.append({"id": 1, "kps": kps})
            frame = {"kind": "frame", "frame": d.get("frame", 0), "t_ms": now_ms, "targets": targets}
            try:
                edge.stdin.write(json.dumps(frame) + "\n")
                edge.stdin.flush()
            except BrokenPipeError:
                print("[bridge] edge-stream stdin closed", flush=True)
                sys.exit(1)
        pos = f.tell()
