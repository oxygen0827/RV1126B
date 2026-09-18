# Luckfox Aura 上板与部署记录（2026-09-18）

本目录是 2026-09-18 在 Luckfox Aura（RV1126B）实板上完成的刷机、屏幕适配、模型部署与
CHIFORM 项目移植的产物归档，用于回退与复现。设备端已生效的改动**同时存在于板子上**，
本目录是可重新生成的源材料。

## 板子当前状态（截至记录时）

- 系统：官方 Debian 13 eMMC 镜像（`Luckfox_Aura_Debian13_eMMC_260606`），kernel 6.1.141
- 网络：Wi-Fi `LDKJ` → `192.168.31.110`；ADB（USB）可用；SSH root/aura 免密已配
- DSI：微雪 3.5inch DSI LCD (E) 已点亮（640x480@60，1 lane）
- 摄像头：IMX415 8MP Camera (A) @ CSI0，v4l2 `/dev/video13`（1280x720 NV12）
- 部署：yolosrv（yolov8s-pose 416 W8A8）+ edge-stream（chiform trajectory）+ bridge

## 1. 刷机背景（Loader bug）

原厂 eMMC 只有半套镜像且**板上旧 Loader 存在 32MB 闪存访问上限**（>0x10000 扇区读写均失败，
读出恒为 0xCC；用 upgrade_tool 与 rkdeveloptool 两个工具交叉验证）。处理后：
`UL download.bin` 更新 Loader → 板子进入 MaskROM → `UF update.img` + `WL` 写入 rootfs.img
（头/中/尾抽样校验通过）。刷机工具：`reference/luckfox-aura/tools/upgrade_tool_v2.44_for_mac.zip`。

## 2. DSI 屏幕适配（微雪 3.5" E）

- 面板参数来自微雪官方 dtbo（`Waveshare_35DSI.dtbo` 的 `35E` 覆写）：
  640x480 @ 24MHz，hfp=48/hsync=32/hbp=80，vfp=3/vsync=4/vbp=13，1 lane，RGB888，无 init sequence
- Aura 官方 DTB 默认是 10.1" 面板（800x1280/2 lane/带 init），因此直接改 DTB：
  1. `dtb/current_board.dtb` 从 boot 分区提取（FIT 的 `fdt` 条目 @0x800，182352 字节）
  2. `dtb/patch_dts.py` 改面板参数（640x480、1 lane、去 init、禁 ws-bl）
  3. `dtb/patch_boot.py` 把新 DTB 写回 **FIT 的 fdt 条目和 resource 里的 DTB 副本**
     （U-Boot 实际使用 resource 里的 DTB），并同步更新两处 FIT SHA-256
  4. `boot-patched.img` 写入板子 `/dev/mmcblk0p4`（备份在板子 `/root/boot-backup.img`）
- 触摸：GT911 在 i2c3 0x5d（`gt9271@5d`），内核自动识别；背光常亮（官方 FAQ 明确该屏不支持背光调节）

## 3. yolov8s-pose 部署（应用侧修复）

见 `../yolov8s-pose-rv1126b/`（含修复补丁、二进制与 README）：
- `boardJPEG` NRGBA/3 字节越界 panic → 修为 RGBA
- v4l2 模式 HTTP 路由缺失（/stream /infer /health 404）→ 统一注册
- MJPEG 标注缓冲 640x480 → 640x360（16:9），并修正标注坐标缩放（原先按源图坐标画在缩略图上）
- 运行依赖：`librknnrt.so`（板端为 librknnrt.so，交付包写的 librknnmrt.so 需软链）

实测：raw 视频 29.8 FPS；摄像头 17–23 FPS（含 MJPEG 叠加）。

## 4. CHIFORM 轨迹包移植

- 部署 `edge-stream` + `config/trajectory.yaml` + `config/actions/squat.yaml` 到板子
  `/root/chiform-trajectory/`；fixtures 回放通过（correct 5 rep 无误报；valgus 出 `KNEE_VALGUS` 等 static_codes）
- `chiform/bridge.py`：把 yolosrv 的逐帧 JSONL（416 空间像素坐标）转成 edge-stream 的
  header/frame 协议（归一化 [0,1]，越界点按协议置 null），支持 REPLAY=1 用视频时间轴回放
- `chiform/start_live.sh`：yolosrv(摄像头) → bridge → edge-stream 全链路启动
- `chiform/start_preview.sh`：DSI 预览（GStreamer + fbdevsink，实验性，未采用）

## 5. 已知问题 / 未完成

- **显示比例**：rkipc 的 VO 把 16:9 画面直接拉满 4:3 面板（官方默认行为，暂回退保留）
- **摄像头 3A**：3A 在 rkipc 进程内；rkipc 与 yolosrv 同时读 ISP 会报 `sof disorder`，
  AE/AWB 表现不稳定。单独跑 yolosrv 时可手动设曝光/增益（如 exposure=500, gain=60）
- **全身入画**：测试机位目前只能拍到上半身，深蹲规则需要髋/膝/脚踝，待重新摆位
- 交付包 Makefile 在包根目录构建会失败（go.mod 在 src/ 下），应 `cd src && go build`

## 6. 从零复现（新板）

```bash
# 1) 刷机（源镜像见 reference/luckfox-aura/firmware/standard/）
#    按住 BOOT 上电进入 MaskROM，用 upgrade_tool：
#    UF update.img -noreset && WL 0x607C40 0x37D800 rootfs.img && RD
# 2) DTB 适配微雪 3.5" E：
python3 dtb/patch_dts.py && dtc -I dts -O dtb -o /tmp/patched.dtb /tmp/patched_board.dts
python3 dtb/patch_boot.py   # 生成 boot-patched.img（需调整脚本内路径）
# 3) 推送运行文件（见各 README）
```
