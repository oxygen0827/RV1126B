# 项目初始化与验收记录

## 本次完成

- 初始化本地 Git 仓库，默认分支 `main`。
- 创建 `upstream/`、`reference/luckfox-aura/`、`docs/`、`scripts/` 目录边界。
- 浅克隆三个官方/芯片生态仓库：Aura 文档、RKNN Toolkit2、RKNN Model Zoo。
- 创建可重复执行的官方资源下载脚本，支持断点续传和已存在文件跳过。
- 从官方 Wiki、Aura 原理图和产品页整理 SKU、接口、供电、SDK 环境和外设芯片证据。

当前 `luckfox-aura-docs` main 快照未包含 README 规划的 `Code/` 目录；示例代码以 SDK、RKNN Model Zoo 和后续官方 Drive 资源为准。Hynetek HUSB311 页面公开附件经标题核验实际为 HUSB238/参考设计，已隔离，不作为 HUSB311 依据。

## 实物识别更新（2026-09-16）

已归档 [板卡档案与照片](hardware/BOARD.md) 以及 2026-09-15 Loader 只读查询转录。USB Loader 可通信，板载 eMMC 容量约 8 GB；内存初步推测为 1 GB，待核验。官网配置与实物有差异，正式 SKU 未确认。本次仅归档既有证据，未重新查询或刷写设备。

## 尚未完成的硬件验证

- 已验证 USB Loader 和存储信息读取；正常 Linux 登录、DDR 容量、UART、网络、Wi-Fi/BT、USB 正常模式、CSI/DSI、RTC 和音频仍未验证。
- 尚未在 Ubuntu 22.04 x86_64 中编译 SDK；当前 macOS 副本只作为资料索引和代码阅读环境。
- Aura 官方资料包只提供 RV1126B SoC datasheet，外设芯片 datasheet 需要继续向各原厂或板卡供应方补齐。

## 复现命令

```sh
./scripts/fetch-official-resources.sh
git -C upstream/luckfox-aura-docs rev-parse HEAD
git -C upstream/rknn-toolkit2 rev-parse HEAD
git -C upstream/rknn_model_zoo rev-parse HEAD
```
