# 首次上板与恢复

## 0. 上板前核对

1. 读取板上丝印，确定 `02000`、`04000`、`02064` 或 `04064` SKU。
2. 只有 `02064/04064` 选择 eMMC 镜像；无 64 后缀先使用 MicroSD 镜像。
3. 准备可靠的 Class 10 或更高等级 MicroSD 卡。官方建议优先使用已验证的 SanDisk 或 SmartQuickly 卡。
4. 准备高品质 5 V/3 A Type-C 电源。不要用电脑 USB、移动电源、PD/快充充电器替代默认电源。
5. USB 数据线直连电脑，避免扩展坞；串口默认速率为 `1500000`，但需以实际 USB-UART 芯片能力和日志为准。

## 1. MicroSD 恢复（默认路径）

1. 在 `reference/luckfox-aura/firmware/standard/` 选择与板型和系统匹配的 `Buildroot_MicroSD_260606.zip` 或 `Debian13_MicroSD_260606.zip`。
2. 先校验 SHA-256，再使用 balenaEtcher 或同等工具将镜像写入 MicroSD；不要把 ZIP 文件当作普通文件复制到卡中。
3. 断电插卡，接入 5 V/3 A 电源，连接串口；记录完整启动日志。
4. 首次启动后执行 `uname -a`、`cat /proc/device-tree/model`、`lsblk`、`ip link`，保存到 `logs/`（该目录默认不入库）。
5. 按最小验收顺序测试：启动介质 → 串口登录 → 以太网 → Wi-Fi/BT → USB Hub/OTG → 40-pin → CSI/DSI → 音频/RTC。

## 2. eMMC / MaskROM 恢复

1. 仅对含 eMMC 的 `02064/04064` SKU 使用 eMMC 镜像。
2. Windows 可使用官方 `DriverAssitant_v5.13.zip`、`RKDevTool_Release_v3.31.zip` 或 `SocToolKit_V2.2.zip`；Linux/macOS 使用官方 Wiki 链接的 `upgrade_tool`。
3. 按官方 Getting Started 页面进入 Loader/MaskROM。若设备未出现，先断电、核对 BOOT/恢复键和 USB 线，再查看 `lsusb`/设备管理器；不要反复写入未知分区。
4. 刷写前记录原有分区和序列号；完成后先只验证 boot/rootfs，再继续改系统配置。

## 3. 证据留存

- 保存镜像文件名、SHA-256、板上 SKU、启动介质、刷写工具版本、串口日志和 `dmesg`。
- 出现启动失败时，不要先改设备树或擦除全部存储；先比对介质、镜像、SKU、供电和进入的刷写模式。
