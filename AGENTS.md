# RV1126B/Aura 开发工作区约束

## 事实边界

- 本工作区对应 Luckfox Aura（官方型号页命名为 Luckfox-Aura-RV1126B），不是 Luckfox Pico RV1103/RV1106 系列。
- SoC 为 Rockchip RV1126B，四核 Arm Cortex-A53 @ 1.6 GHz，NPU 标称 3 TOPS，支持 Buildroot 和 Debian 13。
- 官方板型有 `Luckfox-Aura-02000`、`04000`、`02064`、`04064` 四种 SKU；差异主要是 2/4 GB LPDDR4X 与是否 64 GB eMMC。先以板上丝印确认 SKU，再选镜像。
- 官方资料仓库是 `upstream/luckfox-aura-docs/`；其中的 `Docs/`、`Hardware/` 是上游快照，不在里面直接改产品代码。
- `reference/luckfox-aura/` 保存可复现的 SDK、镜像、烧录工具和 Wiki 快照；这些大文件默认不入 Git。

## 开发边界

- 没有实板、串口日志和当前 SKU 证据时，不修改设备树、PMIC、DDR、摄像头、电源、以太网 PHY 或启动介质配置。
- 不把 RV1106 的单核/小内存/设备树结论复制到 RV1126B。RV1126B 的板载外设和接口必须以 Aura 原理图、官方 Wiki 和实测为准。
- SDK 官方编译环境为 Ubuntu 22.04 x86_64。macOS 只做资料索引、源码阅读和轻量脚本，不在大小写不敏感卷上声称完成 SDK 编译。
- 默认优先使用 MicroSD 镜像进行恢复；eMMC 镜像只用于确认 SKU 含 eMMC 的板子。刷写前核对镜像类型、介质和备份策略。
- 所有新增驱动或应用先放在 `apps/`、`scripts/` 或独立工作树；不要直接改 `upstream/`。

## 资料更新

- 重新抓取官方资料：`./scripts/fetch-official-resources.sh`
- 下载脚本必须可断点续传、可重复执行；新增来源时在 `RESOURCES.md` 记录官方 URL、版本/日期、用途和 SHA-256。
- 外设芯片若只有原理图型号、没有厂商原始数据手册，应标记为“待补证据”，不能用搜索站或无版本 PDF 作为主要依据。

## 验证

- 资料验收至少检查：Git commit、归档文件大小、`file` 类型、SHA-256、压缩包列表（不解压覆盖工作区）。
- 修改文档或脚本后运行对应的 shellcheck/语法检查；涉及板端行为时必须附串口或实测证据。
