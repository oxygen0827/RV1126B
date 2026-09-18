# yolov8s-pose 416 · RV1126B 板端工作副本

来源：`chiform/yolov8s-pose-416-rv1126b(1).tar.gz`（交付包，2026-09-15）。
本目录是 2026-09-18 在 Luckfox Aura 实板部署时使用的**修复版工作副本**，供回归和后续集成。

## 修复内容（相对交付包）

见 `yolov8s-pose-fixes.patch`：

1. **`src/live_stream.go` · `boardJPEG` 越界崩溃**
   原实现把 3 字节/像素的 BGR 缓冲直接塞进 `image.NewNRGBA`（4 字节/像素）并设
   `Stride = w*3`，JPEG 编码器按 4 字节/像素读取时越界：
   `panic: slice bounds out of range [::921604] with capacity 921600`。
   v4l2 模式下每帧都会走 MJPEG 叠加，进程必崩。修复为 `image.NewRGBA` 正确填充 4 字节/像素。
2. **`src/main.go` · v4l2 模式 HTTP 路由缺失**
   `/stream`、`/infer`、`/health` 只在「无 -video/-v4l2 的默认服务模式」注册；
   v4l2 模式下虽然起了 `http.ListenAndServe`，但没有任何路由，全部 404。
   修复为 `registerHTTPHandlers()`（sync.Once）在任何带端口参数的模式下注册。

## 实板验证（2026-09-18，Luckfox Aura / Debian 13 / kernel 6.1.141）

| 场景 | 结果 |
|---|---|
| raw BGR 视频（416×416，99 帧） | **29.8 FPS**，NPU run ≈30 ms，人物检出正常 |
| IMX415 实时摄像头（1280×720 NV12，RGA 零拷贝） | **17.3 FPS**（含 MJPEG 叠加），score 0.83+，COCO-17 关键点完整 |
| MJPEG 预览 `http://<ip>:8080/stream` | 正常（检测框 + 骨架 + FPS） |

截图：`screenshot-live-camera.png`。

## 板端运行

```bash
# 依赖：/oem/usr/lib/librknnrt.so（本板为 librknnrt.so，交付包写的 librknnmrt.so 需符号链接）
ln -sf /oem/usr/lib/librknnrt.so /oem/usr/lib/librknnmrt.so

# 视频文件模式
./yolosrv -model yolov8s_pose_416_w8a8.rknn -video test.bgr -vw 416 -vh 416 -frames 99 \
  -conf 0.5 -smooth 0.5 -jsonl out.jsonl

# 摄像头 + MJPEG 预览（最后一个参数是 HTTP 端口）
./yolosrv -model yolov8s_pose_416_w8a8.rknn -v4l2 /dev/video13 -vw 1280 -vh 720 \
  -frames 100000 -conf 0.4 -smooth 0.5 -jsonl live.jsonl 8080
```

## 已知问题 / 待办

- **摄像头 3A**：直接 v4l2 采集时没有 AE/AWB（3A 在 rkipc 进程内）。实测传感器
  `/dev/v4l-subdev4` 手动曝光/增益可出图，但默认偏绿（无 AWB）。正式 App 需要：
  跑 rkipc 的 3A 后复用 ISP、或固定曝光/白平衡、或集成 rkaiq。
- 交付包 README 写 `/oem/usr/lib/librknnmrt.so`，本板 Debian13 镜像只有 `librknnrt.so`，
  需软链（或改 `src/main.go:39` 的 `libPath`）。
- 交叉编译命令按 Makefile 在包根目录执行会失败（go.mod 在 `src/` 下），应：
  `cd src && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ../yolosrv .`
