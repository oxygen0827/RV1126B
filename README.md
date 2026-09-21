# Luckfox Aura RV1126B 开发工作区

这个目录是 Luckfox Aura（Rockchip RV1126B）后续固件、Linux、AI 推理和板级应用开发的资料与代码基线。目录组织参照 `/Volumes/ML/RV1106`，但只迁移可复用的工程方法，不复制 RV1106 的板级事实或业务应用。

## 已整理内容

- [当前实物板卡档案](docs/hardware/BOARD.md)：RV1126B、8 GB eMMC 已确认；1 GB RAM 为待验证推测，正式 SKU 未确认。附实物照片与 Loader 记录。
- [资料索引](RESOURCES.md)：官方仓库、SDK、镜像、烧录工具、硬件资料、Wiki 和外设芯片证据。
- [硬件参考](docs/hardware-reference.md)：Aura SKU、供电、存储、USB、以太网、Wi-Fi/BT、CSI/DSI、40-pin 和已知风险。
- [首次上板与恢复](docs/board-bringup.md)：SKU 核对、MicroSD/eMMC 选择、MaskROM/Loader、串口和最小验收顺序。
- [软件开发基线](docs/software-development.md)：Ubuntu 22.04 SDK、Buildroot/Debian 13、RKNN 和应用目录边界。
- [初始化与验收记录](docs/project-intake.md)：本次初始化动作、来源策略、验证状态和待补项。
- [AI 协作约束](AGENTS.md)：资料、设备树、刷写和板端验证边界。
- [官方资源 HTML 报告](chip-resource-rv1126b.html)：按硬件、软件、烧录/量产三张表快速检索。

## 目录

```text
RV1126B/
├── docs/                         # 提炼后的开发/上板文档
│   └── hardware/                 # BOARD、PINMAP、PITFALLS 与 evidence/ 实物证据
├── scripts/                      # 可重复执行的资料与开发脚本
├── reference/luckfox-aura/       # SDK、镜像、烧录工具、Wiki 快照（不入库）
├── upstream/                     # 官方 GitHub 源码仓库（不入库）
├── apps/                         # Aura 健身应用、配网与模型部署
└── chip-resource-rv1126b.html    # 官方资料检索报告
```

`upstream/` 与 `reference/` 是上游证据层，不在其中直接做产品改动。正式代码应放在 `apps/` 或另建工作树；SDK 编译应在 Ubuntu 22.04 x86_64 上进行。

## 初始化结论

- 资料对象应称为 **Luckfox Aura RV1126B**；Luckfox Wiki 导航中的 Luckfox Pico 入口对应 RV1103/RV1106，不是本板。
- 官方下载页列出 `luckfox-aura-docs`；本地快照实际包含 `Docs/` 和 `Hardware/`，README 规划的 `Code/` 尚未提供。示例见 RKNN Model Zoo 和 SDK。
- 当前官方通用镜像为 2026-06-06 构建的 Buildroot/Debian 13 eMMC 与 MicroSD 版本；显示适配镜像另行归档。
- SDK 文档明确要求 Ubuntu 22.04 x86_64；macOS 本地副本只做阅读和索引，不作为 SDK 编译环境。

## 当前应用

[Aura 健身应用](apps/aura-bringup/README.md)包含 LVGL 界面、Wi-Fi 配网及 YOLOv8s-pose 后端。
最近修复与实板验收见[2026-09-19 记录](apps/aura-bringup/evidence/2026-09-19/README.md)。
