# 二维时序轨迹＋YOLOv8s-pose 硬件交付说明

版本：`20260915.1`；合同：`chiform.cv.temporal_trajectory.v0_2`。已生成与本地验证，尚未发送或在目标板部署。独立于此前 RTMO-s 交付包，不覆盖原交付物。

- [完整交付包](chiform-trajectory-yolov8s-20260915.zip)（13.7 MiB）
- [SHA256 校验文件](chiform-trajectory-yolov8s-20260915.zip.sha256)

**硬件负责相机采集、YOLOv8s-pose 量化与推理；交付包接收 COCO-17 骨骼序列，实时返回阶段、次数和规则反馈。** 设备只需对应架构的 `edge-stream` 与两份 YAML。固定单人正面徒手深蹲。

包内包含 YOLOv8s 专属静态/时序配置、纯 Go 源码与离线 vendor 依赖、Linux amd64/arm64/armv7 程序、macOS arm64 联调程序、66 份新推理参考骨骼及冻结输出、接入/反馈/标定/验证文档和逐文件指纹。不含姿态模型权重、用户录像或服务凭证；量化模型由硬件侧准备。

解压后先阅读根目录 `README.md`，再执行：

```bash
python3 scripts/verify.py --quick
python3 scripts/verify.py
python3 scripts/verify.py --quick --quantized-contract
scripts/run-pose.sh < fixtures/pose/air_squat_S02_v0_correct_t1.jsonl > result.jsonl
```

输入协议沿用 RTMO 包的 header/frame JSONL，但模型身份、参考 base、规则配置使用本包的 YOLOv8s 值。`docs/HARDWARE_INTEGRATION.md` 给出完整接入样例；`docs/MODEL_AND_PORTING.md` 记录模型指纹与 YOLO 预后处理，避免混用 RTMO 的输入归一化。

规则由相同教练参考视频重新推理后标定，具体效果及可分性见 `docs/CALIBRATION.md`。66 份参考回放及转换身份协议回放验证实现一致性，不能当作人工真值、独立准确率或 INT8 精度。静态深度/膝规则与时序膝模式可能冲突，应按 `docs/FEEDBACK.md` 保留 review/unable，不把未判项目报成正常。

ARM 仅完成交叉编译；量化精度、真实采集时戳、端到端时延和实际语音仍需真板验收。本版同时修复恰好 800 ms 周期被浮点比较误删的问题，0.8 s 门槛不变。新程序对旧 RTMO-s 参考的 65/66 输出不变，另外一条恢复一个同类 800 ms 周期；这是数值修复的明确差异，原 RTMO-s ZIP 保持不变。测试明细见包内 `docs/VALIDATION.md`。

ZIP SHA256：`026b2560ea2ed67d8591f3e97380b1092c01e00ac24414086b14d3e95e9b07ce`
