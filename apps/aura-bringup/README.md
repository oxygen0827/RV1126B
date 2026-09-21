# Luckfox Aura 上板与部署记录（2026-09-18）

**2026-09-20 更新：** Wi-Fi 配网与摄像头链路已改用独立 ISP 3A，修复 RGA 旋转、
帧缓冲持有和录制骨骼归一化。当前实现及验收边界见
[修复验收记录](evidence/2026-09-19/README.md)，应用源码以 `app/src/` 为准。
下文保留早期 bring-up 历史，rkipc 共用、固定等待补 IP 等方案已被替换。

手机未弹窗的追加修复：`wifi_redirect.py` 用板内 libnftables 为热点添加 HTTP/DNS DNAT，
兼容缓存探测 IP 与外部 DNS；通配 DNS 使用 `198.18.0.1`，页面地址仍为
`http://192.168.4.1/`。已通过板端隔离客户端网络测试，真实手机重新连接验收待完成；
下文早期“自动弹登录网络”的描述不能作为手机验收证据。
Apple 探测响应进一步参照 RV1106 健身项目改为 `200 + 完整配网 HTML`；
`/hotspot-detect.html` 和 `/library/test/success.html` 均覆盖，表单提交固定到本机 IP。
当前 4 项门户回归和 1 项客户端网络集成通过，真实 iPhone 自动弹窗仍待现场确认。

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
- 竖装相机经 RGA 顺时针旋转为 360x640（9:16），并修正模型、预览、录像和骨骼坐标映射
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

- **光学对焦**：当前模块无 V4L2 focus 控制；官方页面说明通过旋转镜头手动调焦。
  软件已保持 9:16 比例，但现场仍需把完整人体放入画面并调清镜头。
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

**实板验证**：早期横屏链路录制 5s → `video.h264`（640x360，MPP 硬编）→ 本地保存
`/userdata/fitness/sessions/<id>/{video.mp4,pose.jsonl.gz,session_meta.json}` →
云上传 `report_ready`（会话 ds_ae735c3ef982ce42130c6a0b）。

运行方式：

```bash
/root/yolov8s-pose/yolosrv-new -model yolov8s_pose_416_w8a8.rknn -v4l2 /dev/video13 \
  -vw 1280 -vh 720 -frames 2147483647 -conf 0.4 -smooth 0.5 -rotate90 \
  -demo -session-file /tmp/fitness_session.trigger -session-seconds 20 \
  -movement air_squat -correction-fps 25 8080
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

## 9. LVGL 界面移植（2026-09-18，已能跑通显示）

工程源码（`ai-exercise-tutor` 的 `LF40-720720-ARK/luckfox_pico_lvgl_example`）已推到板子
`/root/lvgl-app` 并**板端原生编译通过**（gcc14 + cmake + 系统 libdrm/libcjson），
截图见 `lvgl/screenshot-fitness-ui-640x480.png`（FITNESS/SQUAT/START 20 SEC/WIFI SETUP 全显示）。

复现步骤（`lvgl/` 下脚本）：

```bash
apt-get install -y libdrm-dev libcjson-dev
# 1) 打包源码（注意 lib/lv_conf.h 和 lib/lv_drv_conf.h 在 lib 根目录，别漏）
# 2) 板端解压到 /root/lvgl-app
# 3) 给 lvgl 子目标补 lv_conf.h 包含路径：
python3 patch2.py
# 4) 接受 640x480 横向面板（原程序只认正方形，SCALE=1.0 渲染在左侧 480px）：
python3 patch_lcd.py
# 5) 触摸设备改 /dev/input/event1（GT911；event0 是电源键）：
sed -i 's|/dev/input/event0|/dev/input/event1|' lib/lv_drv_conf.h
# 6) 配置+构建：
sh build_lvgl.sh        # 产物 build-native/luckfox_lvgl_demo
sh run_lvgl.sh          # 跑 10 秒自测（会先 pkill rkipc 腾出显示）
```

**当前状态与待办**：

- 显示：fbdev 通路可用（跑 LVGL 前需停 rkipc 或 `enable_vo=0`）
- **布局已适配 640×480**（`lvgl/patch_ui_aura.py`）：
  - 显示尺寸改为面板实际尺寸（`PANEL_W/PANEL_H`），UI 按百分比/居中自动铺满
  - 会话页使用左侧 225×400 预览和右侧状态区，适配 640×480 面板
  - 预览尺寸为 360×640 源 → 225×400 显示区（9:16，不拉伸）
  - 路径改 `/root/yolov8s-pose/`（upload 目录、配网脚本）
  - 触摸指向 GT911（`/dev/input/event1`）
- 截图：`lvgl/screenshot-select-page.png`（选择页）、`lvgl/screenshot-session-page.png`
  （会话页：实时预览 + 倒计时 + 本地纠错状态，已验证 UI↔后端集成）
- 一键启动：`/root/yolov8s-pose/run_fitness_app.sh`（独立 ISP 3A + yolosrv 后端 + LVGL UI）
- **TTS 已配置**：`/root/yolov8s-pose/tts.env`（`ZHIPU_API_KEY=...`，600 权限，不入库）→
  `run_fitness_app.sh` 自动带 `-tts-api-key` + 缓存目录 `/userdata/fitness/tts`；
  实测云端报告总评已合成并播放（报告"视频内容为天花板照明灯具…"→ 缓存 wav 376KB）。
  无 key 时可用预录 WAV（`-tts-audio-dir`，ok/knee/hip/elbow/depth.wav）。
- **开机自启**：`chiform-fitness.service`（oneshot，enabled）→ 冷启动后自动拉起
  rkipc 3A + yolosrv + LVGL UI + Wi-Fi 状态循环（已实测重启生效）
- **配网自动化（强制门户）**：
  - 热点启动时写 `/etc/NetworkManager/dnsmasq-shared.d/chiform-captive.conf`
    （`address=/#/192.168.4.1`，所有域名解析到板子）
  - 配网页对 `/generate_204`、`/hotspot-detect.html`、`/ncsi.txt`、`/connecttest.txt`
    返回 302 → 手机连上热点后自动弹"登录网络"
  - NM shared 模式在 Aura 上不下发 IP：脚本等 NM activated 后再补 192.168.4.1/24 并复查
  - `sta` 失败会自动恢复热点（已实测：密码错误 → 回退热点 → 重输）
- **配网链路修复记录（2026-09-19）**：
  - UI 点击失败 `Permission denied`：adb push 后脚本丢失可执行位；UI 改用 `sh <script>` 调用，
    同时启动脚本统一 `chmod +x`
  - 切换被 systemd 杀：portal 的 POST 子进程随 portal 服务停止被清（改用 systemd-run 独立单元）
  - 配网页秒开 + 扫描异步；`/generate_204` 等强制门户路径 302
  - `portal_up` restart 竞态兜底；`do_sta` 重试 4 次
  - `rkipc` 启动失败：systemd/非登录 shell 无 `LD_LIBRARY_PATH`，会加载系统 2021 旧 MPP
    （缺 `mpp_enc_cfg_init_k`）→ 启动脚本显式 `LD_LIBRARY_PATH=/oem/usr/lib:/oem/lib`，
    且 rkipc 用绝对路径 `/oem/usr/bin/rkipc`
- **待办**：手指触摸实测（SQUAT/START/WIFI 按钮）
