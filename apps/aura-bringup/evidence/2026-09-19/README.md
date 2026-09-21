# Wi-Fi 配网与摄像头链路修复验收（2026-09-19）

目标板：USB serial `a5ccf098e8d689c4`，Luckfox Aura / RV1126B，Debian 13，Linux 6.1.141。
本次仅更新应用及应用 systemd 服务；未刷写分区，未修改 DTB、传感器驱动、IQ 文件或 DDR。
主机为 macOS；Go 应用 `CGO_ENABLED=0 GOOS=linux GOARCH=arm64` 编译，测试在板端执行。
这不是 SDK 构建验收。

## 确认的原因

1. `rkipc` 并非仅提供 3A：官方 `rkipc/common/network/network.c:rk_network_get_cable_state`
   在网卡 link 事件上启动 `udhcpc`、清空 `/etc/resolv.conf`，与 NetworkManager 冲突。
   基线实测 AP 激活不到 1 秒，但脚本总计约 17 秒；原脚本用固定睡眠和补 IP 掩盖冲突。
2. HTTP 门户启动晚于 AP；手机首次联网探测时 HTTP 尚未就绪。DNS 仅配置 A 通配，
   实板 dnsmasq 2.91 的 AAAA 探测返回错误。按
   [dnsmasq 官方手册](https://thekelleys.org.uk/dnsmasq/docs/dnsmasq-man.html)
   为专用热点 DNS 同时配置 `local=/#/` 和 `no-resolv`，AAAA/HTTPS 返回无记录且不转发外网。
   冷启动时厂商会写入上游 DNS，单独 `local=/#/` 仍返回 REFUSED；已在此条件下复现并回归。
3. `rga_info.rotation` 被写成十进制 `180`，实际应为 `HAL_TRANSFORM_ROT_180=0x03`，
   并且只设置源描述符。板端四色图测试确认旧代码真实 RGA 输出方向错误。
4. 摄像头帧在推理前就 QBUF，预览再使用全局“最近一帧”fd；缓冲已可被摄像头覆盖，
   还可能与正在解码的骨骼属于不同帧。改为持有当前 job 帧，预览/录制后才 QBUF。
5. 录制视频 640×360，而模型反变换属于摄像头 1280×720。旧代码按录像宽高归一化，
   中心关键点变成 `(1,1)`，正确应为 `(0.5,0.5)`。已修正并用实际写出路径回归。
6. 冷启动厂商 `initd-seq.service` 会启动 rkipc；健身应用必须排在它完成之后再接管。
7. 用户反馈手机仍未弹窗后复查，板端 nft 规则集为空：仅通配 DNS 不能处理手机缓存的公网
   探测地址或指定的外部 DNS。另据 [AOSP NetworkMonitor 实现](https://android.googlesource.com/platform/packages/modules/NetworkStack/+/refs/heads/main/src/com/android/server/connectivity/NetworkMonitor.java)，
   部分 Android 配置会拒绝 RFC1918 探测解析结果，根本不发 HTTP。后者是兼容性风险，
   尚未取得用户手机型号或探测包，不能认定为该手机的已证实根因。

## 修复后行为

- 独立 `rkaiq_3A_server` 提供 AE/AWB，使用本板原来的 `/etc/iqfiles`，不再常驻 rkipc。
- 网页先启动，NM 激活 AP 后验证 HTTP、A/AAAA/HTTPS DNS；不再固定睡眠 8 秒或反复手工补 IP。
- 同一热点重复点击不重新断网；锁串行化切换，状态轮询不覆盖 STARTING/CONNECTING。
- 门户对 Apple 探测返回完整配网 HTML（200）；对 Android/Windows 探测及未知 Host 返回无缓存
  重定向，支持 HEAD。Apple 响应方式已按 RV1106 健身项目对齐，真实 iPhone 弹窗仍待确认。
- 热点 DNS A 返回保留测试地址 `198.18.0.1`；仅 wlan0 入站的 HTTP 80、UDP/TCP DNS 53
  经应用专属 `ip chiform_portal` 表 DNAT 到实际门户 `192.168.4.1`。不拦截 TLS；
  切回 STA/停止热点时删除该表，不改其他防火墙表。通过板内 `libnftables.so.1` 执行，无需 nft 命令。
- 配网页读取 NM 缓存扫描结果，不在 AP 模式强制扫描；可手工填写网络名称。
- STA 失败不再被 `set -e` 提前退出，重试后回到热点。
- 图像链路：1280×720 → 416×234 + 上下各 91 灰边 → YOLOv8s；
  预览/录像 640×360；LVGL 已有 480×270 显示区，保持 16:9。
- HTTP MJPEG 无人使用时不编码，录像先于 MJPEG 标注，避免把预览骨架写进原始录像。

## 实测

| 检查 | 结果 |
|---|---|
| 基线热点启动 | 约 17 秒 |
| 修复后从关闭热点到页面与 DNS 就绪 | 4.64 秒（一次实测，非最坏情况保证） |
| 已就绪热点重复点击 | 0.79 秒，未重建 AP |
| HTTP GET/HEAD 探测 | Apple：200 + 非 Success 配网 HTML；Android/Windows：302 → `http://192.168.4.1/`，通过 |
| DNS（弹窗问题追加修复后） | A → 198.18.0.1；AAAA/HTTPS 无记录、NOERROR，通过 |
| 不存在的 SSID | 实板重试两次后恢复热点，通过 |
| 真正的 STA 成功连接 | 用户 iPhone 手动打开门户后提交成功；板端确认连接 LDKJ，192.168.31.110/24，AP/门户及 DNAT 已关闭 |
| Python 回归 | 4 项通过：Apple HTML、其他 HTTP 探测、转义 SSID、`set -e` 失败回退；含真实 dnsmasq 上游 DNS 场景 |
| 独立客户端网络集成 | 实板 veth/netns：未加规则时缓存公网地址请求失败；加入生产 DNAT 后，公网地址/198.18.0.1/AP 地址 HTTP 均 302，外部 8.8.8.8 DNS 被转至门户，AP 页面 200；1 项通过 |
| 重启自启 | 排在厂商 initd-seq 后接管，ISP 与健身应用 active，约 30 FPS；无 rkipc/udhcpc 残留 |
| 板端 C ABI 检查 | `rga_info=704`、rotation offset 72、V4L2 format/buffer=208/88 |
| Go 回归 | 模型/预览旋转、真实 RGA 四色图、录制坐标中心映射通过 |
| 20 秒实录 | 598 视频帧 + 598 pose 帧；pose 时间轴 19923 ms；视频 640×360 |
| 连续摄像头推理 | 约 29.9 FPS，NPU run 约 26–28 ms；已有人物检出 |

硬件 RGA 回归应在 `systemctl stop chiform-fitness` 后执行。摄像头与模型常驻时额外分配
测试 DMA 缓冲曾触发 `cannot allocate memory`；停应用释放 CMA 后完整通过。

## 尚未通过的现场验收

- 手机系统是否自动打开配网页：用户反馈第一版仍未弹窗，已追加 DNS/HTTP DNAT 修复并部署。
  实板隔离客户端网络测试通过，但用户随后确认 iPhone 仍不自动弹窗，只能手动打开门户。
  该次 portal 日志没有 Apple 系统探测；不能用模拟探测通过代替手机弹窗验收。
- 完整人体和 17 点实时识别：目前源图主要是天花板和边缘头部，缺少完整人体。
- 光学清晰度：直接采集的 1280×720 NV12 源图也模糊，说明屏幕缩放不是唯一原因。
  修复旋转、比例和帧同步后仍应检查镜头洁净、对焦及固定机位；不能声称软件已修复光学虚焦。

## 复现

```sh
# 主机应用编译及部署（仅当前已初始化的 Aura）
sh apps/aura-bringup/provisioning/deploy.sh
# 主机构建 Go 回归可执行文件
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go -C apps/aura-bringup/app/src test -c -o /tmp/aura-regression.test .
adb push /tmp/aura-regression.test /tmp/aura-regression.test
# 板端运行（随后启动应用恢复界面）
adb shell 'systemctl stop chiform-fitness; AURA_RGA_TEST=1 /tmp/aura-regression.test -test.v; systemctl start chiform-fitness'
# Python 回归需要复制 test_wifi.py 到与 wifi_portal.py/wifi_provision.sh 同一目录
adb shell 'cd /root/yolov8s-pose && python3 -m unittest -v test_wifi'
# 复制 test_captive_network.py 后验证真实客户端路由（root；仅创建临时网络命名空间）
adb shell 'cd /root/yolov8s-pose && AURA_NET_TEST=1 python3 -m unittest -v test_captive_network'
```

板端旧应用及脚本备份：`/root/chiform-backup-20260919/`，原录制文件也单独保存在其中。
仓库内早期预编译二进制是历史交付物；应从 `app/src` 构建新版，不能覆盖回旧二进制。

最终实板摘要见 [validation.txt](validation.txt)。部署二进制 SHA-256：
`9f00ca568bb770324f4138538a1890cf7751b15f2e38bdae77c4d6e18646f6a7`。
弹窗追加修复的 4 项回归、服务状态与转发表见 [captive-validation.txt](captive-validation.txt)。

注意板上无可靠 RTC：重启后离线时间回到 2026-04-13，日志日期不代表主机验收日期。
本记录日期采用主机时间；帧率/时长使用运行期测量。未新增联网凭据，也未上传测试视频。

## iPhone 与热点生命周期跟进

- 用户说明“不点击也能检测到热点”发生在上一次调试之后。上轮为等手机验证保留了活动 AP，
  没有超时关闭；这与开机自动启动是两件事。当前 AP profile 为 `autoconnect=no`，成功配网后已停用。
- 发现旧 `chiform-wifi-portal.service` 有 `multi-user.target` 开机启用链接，已禁用并移除 Install
  段，部署脚本同步迁移。它只启动 HTTP，本身不会启动 AP，不将此误认作热点广播的根因。
- 每次打开配网都为复用的旧热点 profile 强制 `connection.autoconnect no`，避免旧配置遗留。
- 探测日志增加 User-Agent/方法并区分门户访问；不记录查询参数、POST 正文或凭据。
- 3 项门户回归通过，实际 service 为 `static/inactive`，路由器连接保留。手机现场探测继续核查。
- 本次用户通过页面主动保存了自己的 Wi-Fi 凭据；文件仅在板端，不读取/归档密码。

## 竖装相机跟进（2026-09-20）

- 实拍确认摄像头物理方向为竖装。链路改为 RGA 顺时针旋转 90°：1280×720 源经方向变换为
  720×1280，模型 letterbox 为 234×416、左右各 91 像素；预览和录像为 360×640。
- LVGL 使用 225×400 的 9:16 显示区，屏幕 framebuffer 实测没有拉伸或裁切。
- 板端 RGA 四色角点、竖屏坐标反变换和录像坐标归一化回归均通过。
- 20 秒录像实测生成 599 帧 H.264，分辨率 360×640；pose 文件含 header 和 599 个 frame。
- 当前镜头朝向天花板且明显失焦，因此该次真人帧数为 0。官方本地资料明确写明可旋转镜头调焦；
  完整人体和 17 点现场验收需重新对准人体并完成手动对焦后进行。

## RV1106 参考对照

用户指定 `/Volumes/ML/RV1106/`（Echo-Mate，commit `99adea8`）中未找到配网门户。
沿当前健身应用移植来源找到 `/Volumes/ML/ai- exercise-tutor/Luckfox_Ultra_W/wifi_provision.py`
与 `wifi_provision.sh`（仓库 HEAD `14b6149`）。参考目录只读，未修改。

- Python 参考 SHA-256：`27e0d108f3ac5c82f345a8bbb95e94915f6c84972a63cf579d008ed24bb148c0`。
- Shell 参考 SHA-256：`390358287ee228ee1f50d60b243f0ba0846ad31334278d7e2a35099f1dc51e11`。
- 参考方案为 hostapd + dnsmasq 通配 DNS + HTTP 所有普通 GET 返回配网页 200，没有专用 302
  或 HTTPS Captive Portal API。其文档验收写的是手动打开 IP，不把它视为已取得 iPhone 弹窗证据。
- Aura 继续使用本板 NetworkManager，不复制 RV1106 驱动/网络管理脚本；对 Apple 的
  `/hotspot-detect.html`、`/library/test/success.html` 对齐非 Success HTML 200。
- 页面提交地址固定为板子 IP；CNA 在 Apple 域名下展示页面时可跳至本地页面并继续扫描/提交。
- 新增 Apple GET/HEAD 回归修改前失败（302 != 200），修改后通过；网络命名空间集成增加
  模拟 Apple 探测经 DNAT 收到完整 HTML。实板共 4 项门户回归 + 1 项客户端网络集成通过。
- 此次验收尚未收到真实 iPhone 的 Apple 探测；不能宣称自动弹窗问题已经闭环。

## Setup 后热点未出现的修复

- 清除保存凭据后，用户点击 Setup，`/tmp/fitness_wifi.log` 实测为
  `portal not ready: wrong HTTP service`、`FAILED: cannot start setup page`。
  journal 报 `OSError: [Errno 98] Address already in use`，`ss` 确认 80 端口被
  `/oem/usr/sbin/nginx` PID 1665/1666 占用（所属 initd-seq.service）。
- 厂商 `/oem/usr/bin/RkLunch-RKIPC_RV1126B.sh:5` 遍历 `S??*`，原先仅改名为
  `S50nginx.disabled` 无法禁用。新增 `prepare_wifi_portal.sh`，由门户 ExecStartPre 执行：
  将该启动文件完整保存在 `/oem/usr/etc/init.d/chiform-disabled/`，并按可执行文件路径
  停止厂商 nginx。未知 HTTP 服务不自动杀掉。未修改 SDK、DTB 或原启动脚本内容。
- 状态轮询覆盖错误的回归修改前失败（FAILED 被写成 IDLE），修复后通过；现在断开时保留错误，
  下次点击/停止或真正连接后才更新。门户启动失败时停止 systemd 重启循环。
- 实板恢复 Setup：`HOTSPOT READY`，80 端口由门户 Python PID 1359 监听；重复 Setup 后
  AP 保持活动。5 项门户回归 + 1 项客户端网络测试通过；语法和 diff 检查通过。
- 确认保存凭据文件不存在，Wi-Fi profile 仅保留 CHIFORM-SETUP；本次修复未恢复旧路由器信息。
  保持用户本次请求的热点开启，供手机连接。启动文件已移出通配范围，本轮未重启整板。
