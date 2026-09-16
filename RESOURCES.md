# Luckfox Aura RV1126B 资料索引

本索引记录 2026-09-13 对 Luckfox 官方 Wiki、官方 GitHub 和官方 Google Drive 目录的核验结果。外部链接可能更新；`upstream/` 和 `reference/luckfox-aura/` 提供本地可复现快照。大文件默认不入 Git，下载与校验由 `scripts/fetch-official-resources.sh` 负责。

## 芯片与板型识别

- **当前实物（2026-09-16 更新）**：[板卡档案及本地证据](docs/hardware/BOARD.md)。RV1126B、板载 8 GB eMMC 已确认；1 GB RAM 仅为料号推测，正式 SKU 待确认。下列官网配置不代替实物档案。
- **用户原始目标**：Luckfox Pico RV1126B。
- **官网规范名称**：Luckfox Aura RV1126B。Luckfox Pico 官网入口实际对应 RV1103/RV1106，本项目不要沿用该名称。
- **SoC**：Rockchip RV1126B，四核 Cortex-A53 @ 1.6 GHz，NPU 3 TOPS，4K H.265/H.264 编解码，最大 12M@30fps ISP 输入。
- **SKU**：`Luckfox-Aura-02000` / `04000` / `02064` / `04064`；分别对应 2/4 GB LPDDR4X 和是否包含 64 GB eMMC。必须以板上丝印确认。
- **系统**：Buildroot、Debian 13；官方 SDK 编译支持 Ubuntu 22.04 x86_64。

## 官方入口

| 入口 | 用途 |
| --- | --- |
| [Aura 官方 Wiki](https://wiki.luckfox.com/zh/Luckfox-Aura/) | 产品、上板、登录、Pinout、SDK、Buildroot、Debian、RKNN、下载 |
| [Aura 官方下载页](https://wiki.luckfox.com/zh/Luckfox-Aura/Downloads) | 官方 GitHub 与 Google Drive 资源入口 |
| [Aura 官方 GitHub 资料仓库](https://github.com/LuckfoxTECH/luckfox-aura-docs) | Docs、Hardware、Code、工具说明；本地 `upstream/luckfox-aura-docs/` |
| [Aura 官方 Google Drive](https://drive.google.com/drive/folders/1n5aovHVldUqts--or2wogNnBuv_bwVRR?usp=sharing) | SDK、系统镜像、硬件/文档镜像、工具 |
| [LuckfoxTECH GitHub 组织](https://github.com/LuckfoxTECH) | 官方板卡与示例仓库 |
| [RKNN Toolkit2](https://github.com/airockchip/rknn-toolkit2) | 模型转换、量化、API 和运行库；本地 `upstream/rknn-toolkit2/` |
| [RKNN Model Zoo](https://github.com/airockchip/rknn_model_zoo) | 官方模型/C/Python 推理例程；本地 `upstream/rknn_model_zoo/` |

## 本地官方仓库

| 本地路径 | 上游 | 本次核验 commit | 用途 |
| --- | --- | --- | --- |
| `upstream/luckfox-aura-docs/` | `https://github.com/LuckfoxTECH/luckfox-aura-docs.git` | `7264f6eab0562195b1d0c53969fff6cb7df360c9` | Aura Docs、SoC 手册、硬件原理图、CAD、3D、英文/中文技术文档 |
| `upstream/rknn-toolkit2/` | `https://github.com/airockchip/rknn-toolkit2.git` | `59a913d172e7f5ff03c9076e2ec7b1b1288ffd08` | RKNN Toolkit2 转换与运行库 |
| `upstream/rknn_model_zoo/` | `https://github.com/airockchip/rknn_model_zoo.git` | `bad6c7334531becaf90a561988519b7bec34d0ab` | RKNN 模型部署例程 |

## 硬件开发资料

| 优先级 | 资料 | 本地位置 | 官方来源/备注 |
| --- | --- | --- | --- |
| 必需 | RV1126B SoC Datasheet V1.1 (2025-04-28) | `upstream/luckfox-aura-docs/Docs/datasheets/Rockchip_RV1126B_Datasheet_V1.1-20250428.pdf` | [GitHub 文件](https://github.com/LuckfoxTECH/luckfox-aura-docs/blob/main/Docs/datasheets/Rockchip_RV1126B_Datasheet_V1.1-20250428.pdf) |
| 必需 | Aura 原理图 | `upstream/luckfox-aura-docs/Hardware/Schematic/Luckfox-Aura.pdf` | [GitHub 文件](https://github.com/LuckfoxTECH/luckfox-aura-docs/blob/main/Hardware/Schematic/Luckfox-Aura.pdf)；板载器件与接口事实 |
| 必需 | RV1126B GPIO 用户手册 | `upstream/luckfox-aura-docs/Docs/zh/bsp/RV1126B_User_Manual_GPIO.pdf` | [GitHub 文件](https://github.com/LuckfoxTECH/luckfox-aura-docs/blob/main/Docs/zh/bsp/RV1126B_User_Manual_GPIO.pdf) |
| 必需 | Aura 产品规格/SKU | `reference/luckfox-aura/wiki/Introduction.html` | [官方产品页](https://wiki.luckfox.com/zh/Luckfox-Aura/Introduction/)；2/4 GB、eMMC、供电、接口 |
| 推荐 | 3D STEP | `upstream/luckfox-aura-docs/Hardware/3D Models/Luckfox Aura.step` | [GitHub 文件](https://github.com/LuckfoxTECH/luckfox-aura-docs/blob/main/Hardware/3D%20Models/Luckfox%20Aura.step) |
| 推荐 | CAD 顶/底板 DWG | `upstream/luckfox-aura-docs/Hardware/CAD/` | [Hardware 目录](https://github.com/LuckfoxTECH/luckfox-aura-docs/tree/main/Hardware/CAD) |
| 推荐 | GPIO、IIC、POE、PWM、RTC、SPI、UART、USB Pinout | `reference/luckfox-aura/wiki/Aura-pinout-*.html` | [官方 Pinout](https://wiki.luckfox.com/zh/Luckfox-Aura/Pinout) |
| 推荐 | CSI/DSI、ISP、媒体、USB、Wi-Fi/BT 设计指南 | `upstream/luckfox-aura-docs/Docs/zh/` | 官方 Rockchip Developer Guide；按需阅读，不要把邻近芯片结论直接套用 |

## 软件开发资料

| 优先级 | 资料 | 本地位置 | 官方来源/备注 |
| --- | --- | --- | --- |
| 必需 | Aura SDK 260521 | `reference/luckfox-aura/sdk/Luckfox_Aura_SDK_260521.tar.gz` | [官方 Drive SDK 目录](https://drive.google.com/drive/folders/1tUdki0eWttQA0BSPb3XZw5D_3XN4tLPn)；Ubuntu 22.04 x86_64 |
| 必需 | SDK 镜像编译/配置文档 | `reference/luckfox-aura/wiki/SDK-Image-Compilation.html` | [官方 Wiki](https://wiki.luckfox.com/zh/Luckfox-Aura/SDK-Image-Compilation) |
| 必需 | Buildroot 配置 | `reference/luckfox-aura/wiki/Buildroot-Configuration.html` | [官方 Wiki](https://wiki.luckfox.com/zh/Luckfox-Aura/Buildroot-Configuration) |
| 推荐 | Linux 内核配置 | `reference/luckfox-aura/wiki/Kernel-Configuration.html` | [官方 Wiki](https://wiki.luckfox.com/zh/Luckfox-Aura/Kernel-Configuration) |
| 推荐 | RV1126B IPC SDK quick start/release notes | `upstream/luckfox-aura-docs/Docs/zh/ipc/` | 官方 Rockchip 文档；SDK 目录已含对应内容 |
| 必需 | RKNN Toolkit2 | `upstream/rknn-toolkit2/` | [官方 GitHub](https://github.com/airockchip/rknn-toolkit2)；转换/量化/运行库 |
| 推荐 | RKNN Model Zoo | `upstream/rknn_model_zoo/` | [官方 GitHub](https://github.com/airockchip/rknn_model_zoo)；C/Python 例程 |
| 推荐 | Aura Code/示例 | 当前 `upstream/luckfox-aura-docs` 快照 | [官方 Code 目录](https://github.com/LuckfoxTECH/luckfox-aura-docs/tree/main/Code)；README 规划了 `Code/`，但当前 main 快照实际未检出该目录，需以后续 release/Drive 内容为准 |

## 烧录编程 / 量产测试资料

| 优先级 | 资料 | 本地位置 | 官方来源/备注 |
| --- | --- | --- | --- |
| 必需 | Buildroot/Debian 13 MicroSD 镜像 | `reference/luckfox-aura/firmware/standard/` | 官方 Drive `Images` 目录；与 eMMC 版本不可混用 |
| 必需 | Buildroot/Debian 13 eMMC 镜像 | `reference/luckfox-aura/firmware/standard/` | 官网下载集；当前 8 GB eMMC 实板的 DDR/板级配置及容量适配尚待核验 |
| 必需 | upgrade_tool Linux/macOS | `reference/luckfox-aura/tools/upgrade_tool_v*.zip` | [官方 Wiki 资源](https://wiki.luckfox.com/Luckfox-Aura/Downloads) |
| 必需 | SocToolKit V2.2 | `reference/luckfox-aura/tools/SocToolKit_V2.2.zip` | 官方 GitHub Release；Windows 镜像/分区刷写 |
| 推荐 | RKDevTool v3.31 | `reference/luckfox-aura/tools/RKDevTool_Release_v3.31.zip` | 官方 Drive Tools 目录；Windows Loader/MaskROM |
| 推荐 | DriverAssistant v5.13 | `reference/luckfox-aura/tools/DriverAssitant_v5.13.zip` | 官方 GitHub Release；RK USB 驱动 |
| 推荐 | balenaEtcher / SDCardFormatter | `reference/luckfox-aura/tools/` | 官方 Release 工具；MicroSD 写卡/格式化 |
| 可选 | adb_fastboot、MobaXterm、yuvplayer | `reference/luckfox-aura/tools/` | 登录、串口/SSH、YUV 调试辅助 |

## 系统镜像清单

| 文件 | Google Drive 文件 ID | 大小（官网目录核验） | 用途 |
| --- | --- | ---: | --- |
| `Luckfox_Aura_Buildroot_eMMC_260606.zip` | `1GJ1QLeWLedw_4oJthfTzBYGH6Djouwp2` | 238,904,525 B | Buildroot + eMMC |
| `Luckfox_Aura_Buildroot_MicroSD_260606.zip` | `1gPHcMNJyVkzNLTJ7WUY-dc79AjzFcMm3` | 339,477,605 B | Buildroot + MicroSD |
| `Luckfox_Aura_Debian13_eMMC_260606.zip` | `1bmGTj7a8mZelMOPfOHfDAmRaxChS6q_1` | 766,191,850 B | Debian 13 + eMMC |
| `Luckfox_Aura_Debian13_MicroSD_260606.zip` | `1x4MPURlHa8MIFAGD92Koa2AQ6PphKJ4a` | 1,397,164,410 B | Debian 13 + MicroSD |

显示适配包也已纳入下载脚本：5-inch、7-inch、13.3-inch DSI，以及 10.1-inch DSI-TOUCH-B 的 `boot.img` 和 `luckfox-config-arm64` 覆盖文件。它们不是默认上板恢复镜像，只有接对应显示器时才使用。

## 外设芯片手册状态

Aura 原理图确认的型号如下。官方 Aura 资料包没有随附完整外设 datasheet，因此这些条目目前记录“型号已识别、手册待补”，不使用随机 PDF 镜像替代。

| 型号 | 功能 | 已核验官方入口 | 状态 |
| --- | --- | --- | --- |
| CH334F | USB 2.0 Hub | [WCH CH334/CH335 产品页](https://www.wch.cn/products/CH334.html) | 产品入口已核验；具体版本手册待补 |
| HUSB311BLA | USB Type-C PD/TCPC | [Hynetek HUSB311 产品页](https://en.hynetek.com/2421.html) | 产品入口已核验；该页公开附件当前解析为 HUSB238/参考设计，已放入 `reference/luckfox-aura/peripherals/hynetek-unmatched/`，不能当作 HUSB311 手册 |
| RTL8211F-CG / MAE0621A-Q3C | 千兆以太网 PHY/网络变压器 | [Aura 原理图](https://github.com/LuckfoxTECH/luckfox-aura-docs/blob/main/Hardware/Schematic/Luckfox-Aura.pdf) | 型号已识别；官方配套手册待补 |
| SKI.WB800D80S.1 | Wi-Fi 6/Bluetooth 5.4 模组 | [Aura 产品页](https://wiki.luckfox.com/zh/Luckfox-Aura/Introduction/) | 型号已识别；模组规格书/射频指南待补 |
| WS3221C、DIO7003HEST5、SGM2590D | 电源/USB 路径开关与保护 | [Aura 原理图](https://github.com/LuckfoxTECH/luckfox-aura-docs/blob/main/Hardware/Schematic/Luckfox-Aura.pdf) | 型号已识别；厂商原始手册待补 |

## 最小下载集

1. RV1126B Datasheet、Aura 原理图、产品规格和 Pinout。
2. `Luckfox_Aura_SDK_260521.tar.gz` 与 SDK 镜像编译文档。
3. 至少一套与实物介质匹配的 MicroSD 或 eMMC 官方镜像。
4. `upgrade_tool`；Windows 再准备 DriverAssistant、RKDevTool 或 SocToolKit。
5. RKNN Toolkit2、RKNN Model Zoo。
6. 需要摄像头/显示/音频/USB/以太网开发时，再按接口加载对应 Rockchip 文档和外设手册。

## 推荐阅读顺序

1. 产品介绍 → SKU、供电、启动介质和接口。
2. 原理图 → 40-pin、CSI/DSI、USB Hub/OTG、PHY、Wi-Fi/BT、电源。
3. Getting Started → MicroSD/eMMC、Loader/MaskROM、登录。
4. SDK 镜像编译 → Ubuntu 22.04、lunch、BoardConfig、Buildroot/Debian。
5. Pinout/接口指南 → 单项外设上板。
6. RKNN Toolkit2/Model Zoo → 模型转换与板端推理。

## 缺失或风险项

- 尚未确认用户手上具体 Aura SKU；镜像选择必须在上板前完成。
- 已在本板完成 USB Loader 查询并归档容量/分区表；正常启动、DDR、串口、网络、无线及其余外设待验证，供电方式由用户确认但电气参数未实测。
- 外设芯片 datasheet、BOM 变体、PCB 布线文件、无线天线/认证资料未在官方 Aura 包中提供，需要向原厂/板卡供应方补齐。
- SDK 与大镜像来自官方 Google Drive，文件可能随时间替换；本地下载后应保留 SHA-256 与文件大小。
- 当前 `luckfox-aura-docs` main commit `7264f6e` 的实际树只有 `Docs/` 与 `Hardware/`，板级例程不能假设已随仓库提供。
- macOS 大小写不敏感卷不适合作为 Linux SDK 编译环境；正式编译使用 Ubuntu 22.04 x86_64/ext4。
