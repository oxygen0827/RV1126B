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

## 7. CHIFORM 健身应用移植（2026-09-18，RV1106 → RV1126B）

将 RV1106 健身应用（`ai-exercise-tutor` 仓库）的后端功能移植到 RV1126B 的 yolosrv：

- `app/src/`：RV1126B yolosrv + 从 RV1106 移植的模块
  （`demo_rec.go` 录制/保存/上传、`fitness_record.go` 触发、`pose_session.go` 判定窗、
  `pose_writer.go` 协议 7.2 序列写出、`tts*.go`、`fitness_compat.go` 兼容层）
- `app/demo_upload.sh`：云上传脚本（Aura 适配：库路径修正、尺寸校验改按 meta、A380/A5 尺寸）
- `app/yolosrv-rv1126b-fitness`：已构建的 aarch64 二进制

**实板验证（全部通过）**：录制 5s → `video.h264`（640x360，MPP 硬编）→ 本地保存
`/userdata/fitness/sessions/<id>/{video.mp4,pose.jsonl.gz,session_meta.json}` →
云上传 `report_ready`（会话 ds_ae735c3ef982ce42130c6a0b）。

运行方式：

```bash
/root/yolov8s-pose/yolosrv-new -model yolov8s_pose_416_w8a8.rknn -v4l2 /dev/video13 \
  -vw 1280 -vh 720 -frames 100000 -conf 0.4 -smooth 0.5 -rotate180 \
  -demo -session-file /tmp/fitness_session.trigger -session-seconds 20 \
  -movement air_squat -correction-fps 25 -jsonl /tmp/fitness_live.jsonl 8080
# 触发：touch /tmp/fitness_session.trigger（UI 写）
# 保存：touch /tmp/fitness_save.trigger    上传：touch /tmp/fitness_upload.trigger
```

Aura 侧踩坑（已在脚本/代码里修掉）：

1. `/oem/usr/lib` 的旧 freetype(2.6) 抢在系统库前 → ffmpeg 崩
   （`FT_Set_Var_Design_Coordinates`）；demo_upload.sh 里把系统库路径前置。
2. `probe_encoded_mp4` 硬编码 640x480 校验 → 改成按 meta 的宽高。
3. `launchUpload` 未传 `APP_DIR/UPLOADER_ENV` → 上传脚本找不到 uploader.env。
4. Aura 无 ffmpeg → `apt install ffmpeg`。

## 8. 免烧录配网（Debian/NetworkManager 版）

`provisioning/`：

- `wifi_provision.sh`：`portal|sta|status|sync-time|stop`
  - portal：nmcli 起开放热点 `CHIFORM-SETUP`（192.168.4.1，ipv4 shared）+ 网页；
    部分驱动下 NM shared 不下发地址，脚本兜底 `ip addr add`。
  - sta：清旧 profile → `nmcli dev wifi connect` → 失败自动恢复热点。
  - sync-time：无 RTC 校时（UDP NTP + HTTP Date 兜底）。
- `wifi_portal.py`：80 端口配网页（`/` 页面、`/api/scan`、`/api/status`、POST 保存并连接），
  扫描走 nmcli。
- `chiform-wifi-portal.service`：systemd 托管 portal（替代不稳定的 setsid 启动）。

Aura 侧依赖与冲突处理：`apt install dnsmasq`（NM shared 的 DHCP/DNS），
**停用系统 dnsmasq 服务**（与 NM 的实例抢 53）；**停用 nginx**（占 80，且 S50nginx
在 /oem 里，已改名为 .disabled）。

## 9. 待办（LVGL 界面）

- LVGL 工程 `LF40-720720-ARK/luckfox_pico_lvgl_example` 需为 Aura 重建：
  板端已有 gcc14/cmake，需 `apt install libdrm-dev`，显示改 640x480 DSI、触摸改 GT911(evdev)、
  UI 布局从 720x720 适配到 640x480、路径改 `/root/yolov8s-pose/`、预览尺寸改 640x360
- 配网页面手机实测
- TTS（aplay 本地 WAV）验证
