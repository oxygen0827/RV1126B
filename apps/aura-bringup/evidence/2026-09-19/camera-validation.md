# 摄像头与模型分段实测

目标：Aura RV1126B，ADB `a5ccf098e8d689c4`。本记录不等同于真实人体完整验收。

## 输入和显示几何

- 实板 media topology：IMX415 3864×2192，有效区域 3840×2160；ISP 全幅裁切 3840×2160。
- `/dev/video13`：NV12 1280×720，stride 1280，buffer 1382400 bytes。
- 推理按比例 letterbox 到 416×234，上下 padding 各 91；预览 640×360，LVGL 480×270。
- 以上有效图像均为 16:9。当前没有把 16:9 源画面拉满 4:3 面板；光学畸变尚未用标定板测量。

## 相机源端

停止健身应用、启动独立 ISP，然后使用 v4l2-ctl 采集，跳过 30 帧让 3A 收敛：

```sh
v4l2-ctl -d /dev/video13 --set-fmt-video=width=1280,height=720,pixelformat=NV12 \
  --stream-mmap=4 --stream-skip=30 --stream-count=1 --stream-to=/tmp/aura-camera-source.nv12
```

[未经过应用缩放的源图](camera-source.png) 同样明显模糊、偏紫，没有完整人体。
说明模糊发生在应用 RGA/屏幕缩放之前，尚不能通过此画面判断真实人体识别质量。
该图只做 NV12 解码，未做锐化、几何修正或人工添加关键点。

- ISP 使用原 `/etc/iqfiles/imx415_default_default.json`，传感器配置 `CISFlip=3`，
  实测 hflip/vflip 均为 1；应用仍使用此前已验证的 180° RGA 旋转。
- 传感器列出的控制含曝光/增益/翻转，无 focus 控制；media topology 未列出镜头马达实体。
- ISP 中 AWBGAIN/DEBAYER/CCM 开启，Interrupt ErrCnt=0。未修改 IQ、驱动或传感器控制。
- 需要现场排除镜头遮挡、保护膜、失焦及摆位问题，再验证偏色和完整人体。

## 板载 NPU 与骨骼解码

使用既有 RV1106 健身项目 `ai-correction/yolov8_pose/testdata/bus.bmp`（640×640）作为
清晰测试输入。保持比例缩放到 720×720，居中放入 1280×720 灰色画布，不做人体拉伸。

```sh
LD_LIBRARY_PATH=/oem/usr/lib:/oem/lib ./yolosrv-new \
  -model yolov8s_pose_416_w8a8.rknn -video /tmp/model-test-1280x720.bgr \
  -vw 1280 -vh 720 -frames 30 -conf 0.4 \
  -anno /tmp/model-test-pose.png -jsonl /tmp/model-test-pose.jsonl
```

- 实板连续 30 帧均检测到 2 人，置信度约 0.818 / 0.500。
- 每人输出 17 点；高于 0.5 的关键点分别 15 / 15 个。
- NPU 平均 run 25.8 ms；包含一次 PNG 标注写出的短测试整体 23.8 FPS。
- [真实板载推理结果](camera-model-test-pose.png) 显示骨骼与人物对齐。
  这是测试图的模型结果，**不是当前摄像头已经拍清并识别人体的证明**。
- 测试后恢复 chiform-fitness 和 chiform-isp，两者 active，摄像头恢复约 29.8 FPS。

## 其他处理与待完成

- 启停脚本 `pkill -f yolosrv-new` 会误杀命令行包含程序名的 ADB 调试 shell。
  已改为 `pkill -x yolosrv-new` 并部署，推理测试与随后应用恢复完成。
- Wi-Fi 配置此前按用户要求清空。目前仍处于 CHIFORM-SETUP 热点，未获得新的 STA 凭据；
  尚未完成本轮路由器联网，不从日志或旧备份恢复用户要求删除的密码。
- 等待现场调整摄像头并让完整人体入画，再进行真实相机、屏幕与 20 秒录制联调。

## 2026-09-20 跟进

- 用户重新提供凭据后已连接 LDKJ，DHCP 地址 192.168.31.110，外网 HTTPS 返回 200。
  密码仅保存板端配置，不记录在本报告。
- 配网前发现 `/tmp` 494 MB tmpfs 已满：连续逐帧 `fitness_live.jsonl` 占 486 MB，导致
  状态文件写入失败。生产启动移除 `-jsonl`，在停止写入进程后清除旧诊断文件；
  会话录像自己的 pose 数据路径不受影响。部署重启后 /tmp 占用 5.5 MB（2%）。
- 最新摄像头预览仍是模糊近景，无完整人体；真实人体识别验收继续等待现场镜头/机位调整。

## 2026-09-20 竖屏链路实测

- 现场帧显示摄像头为竖装，旧链路把人体横放。应用现用 `HAL_TRANSFORM_ROT_90=0x04`
  在 RGA 源端顺时针旋转，模型视图为 720×1280。
- 模型输入为 234×416、左右各 91 像素；预览/录像为 360×640；LVGL 为 225×400。
  三段均保持 9:16，屏幕 framebuffer 已核对，无非等比拉伸。
- 板端硬件回归通过：四色角点方向、模型 letterbox、录制归一化坐标均符合预期。
- 从 UI 模拟点击 `START 20 SEC` 成功，生成 599 帧 360×640 H.264 和 599 个 pose frame，
  后端持续约 29.9 FPS。
- 验收时实时画面仅有天花板灯，人物不在画面内，故该会话 `person_rows=0`。
  直接源与录像都明显虚焦；该相机没有 V4L2 focus 控制，官方本地页面写明可旋转镜头调焦。
