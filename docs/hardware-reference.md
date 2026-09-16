# Luckfox Aura RV1126B 硬件参考

## 当前实物（2026-09-16 更新）

以 [BOARD.md](hardware/BOARD.md) 为本板配置来源：RV1126B、8 GB eMMC 已确认；`ONLP4D256M32H` 内存初步推测 1 GB，尚无手册/启动日志确认。照片中的无线模组顶标为 `VS6621S80`，不能直接等同于原理图型号。正式 SKU 和 PCB 版本待核对。

## 官网配置参考（2026-09-13 快照，不代表当前实物）

| 项目 | 官方资料 | 备注 |
| --- | --- | --- |
| SoC | Rockchip RV1126B | 四核 Arm Cortex-A53 @ 1.6 GHz |
| NPU | 3 TOPS | 支持 int4、int8、int16、FP16；实际模型性能需上板测量 |
| 内存 | LPDDR4X | `02000/02064` 为 2 GB；`04000/04064` 为 4 GB |
| 闪存 | 依 SKU | `02064/04064` 含 64 GB eMMC；无后缀 64 的 SKU 依 MicroSD 启动 |
| 系统 | Buildroot、Debian 13 | 官方镜像按 eMMC/MicroSD 分开发布 |

官方产品页列出四个板型：`Luckfox-Aura-02000`、`Luckfox-Aura-04000`、`Luckfox-Aura-02064`、`Luckfox-Aura-04064`。在下载镜像、核对分区或诊断容量前，先读取板上丝印和 `lsblk`，不要仅凭目录名猜型号。

## 板载接口

- 1 个千兆以太网口；原理图器件注释列 `MAE0621A-Q3C/RTL8211F-CG`，不据此认定两颗器件同时装配，实装 PHY 待核对。
- 2 个 MIPI CSI 4-lane 摄像头接口；1 个 MIPI DSI 显示接口。
- USB 3.0 OTG Type-C；CH334F USB 2.0 Hub 扩展 4 个 USB Host 口。
- 2.4/5 GHz Wi-Fi 6、Bluetooth 5.4/BLE 模组，原理图标注 `SKI.WB800D80S.1`；SDIO 与 UART/PCM 信号需以设备树和实测为准。
- 40-pin 扩展口，兼容部分 Raspberry Pi HAT，但复用、电平和电源能力仍以 Pinout 页面/原理图为准。
- 贴片麦克风、3.5 mm 耳机/麦克风接口、SH1.0-2P RTC 接口、MicroSD 卡槽。
- USB-C 供电为 5 V；官网要求高品质 5 V/3 A 电源，不支持快充/闪充/PD 协议、主机 USB 供电或移动电源作为默认电源。

## 原理图识别到的外设芯片

以下是官方 `Luckfox-Aura.pdf` 原理图中可见的型号，不等同于已取得完整数据手册：

| 型号 | 位置/功能 | 资料状态 |
| --- | --- | --- |
| CH334F | USB 2.0 Hub | WCH 官方产品页可核验；具体封装/配置仍需数据手册 |
| HUSB311BLA | USB Type-C PD/TCPC | Hynetek 官方产品页可核验；该页可下载附件当前为 HUSB238/参考设计而非 HUSB311，已隔离到 `reference/luckfox-aura/peripherals/hynetek-unmatched/` |
| RTL8211F-CG / MAE0621A-Q3C | 千兆以太网 PHY/变压器 | 原理图型号已核验；未在 Aura 官方资料包发现配套 datasheet |
| SKI.WB800D80S.1 | Wi-Fi 6/Bluetooth 5.4 模组 | 官方产品页未给出完整模组规格书，待补证据 |
| WS3221C、DIO7003HEST5、SGM2590D | USB/电源路径保护与负载开关 | 原理图已核验；未在官方 Aura 包中发现对应 datasheet |

开发外设驱动前，优先从 `RESOURCES.md` 的官方器件入口或板厂提供的原始附件补齐手册；不要用不明版本的搜索站 PDF 代替。

## 风险边界

- `RV1126B_Datasheet_V1.1-20250428.pdf` 是当前官方随 Aura 发布的 SoC 数据手册；芯片级电气限制、封装和复用应以它及后续 Rockchip 官方版本为准。
- 原理图 PDF 是硬件事实的起点，但不包含所有器件数据手册、BOM 变体和已装配 SKU 差异；硬件改版前需取得对应 PCB/BOM 和实测电源时序。
- CSI/DSI 高速信号、DDR/LPDDR4X、USB 3.0、千兆 PHY、PoE/电源和 Wi-Fi 天线均属于高风险区域，不能仅凭通用 RV1126 经验修改。
