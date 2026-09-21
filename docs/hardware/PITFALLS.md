# 通用坑清单

> 更新：2026-09-16；板卡：Luckfox Aura RV1126B，SKU/PCB 版本待核对。
> 固件/U-Boot：待核对；工具：upgrade_tool v2.44 macOS；SDK 归档：260521，未构建。

| 分类 | 现象 | 根因 | 规则/对策 | 影响范围 | 证据 | 记录日期 |
| --- | --- | --- | --- | --- | --- | --- |
| 板型识别 | 官网列 2/4 GB + 64 GB，实板存储约 8 GB | 具体版本差异待核对 | 当前实物以 BOARD.md 为准，不套用 02064/04064；1 GB RAM 仅是料号推测 | 镜像、DDR、容量预算 | [实测]+[实物照片] 见 BOARD.md | 2026-09-16 |
| 存储识别 | RID 返回 EMMC 字符串 | Rockchip 部分 U-Boot 根据 IF_TYPE_MMC 返回固定字符串 | 必须结合照片、无 SD 卡条件和容量确认板载 eMMC | 存储类型 | [官方] [U-Boot 实现](https://github.com/rockchip-linux/u-boot/blob/next-dev/drivers/usb/gadget/f_rockusb.c)，仅说明接口局限，未证明与板端版本一致 | 2026-09-16 |
| 启动诊断 | Loader 可见，正常复位后 ADB 不可见 | 待核对，不能据此判定系统损坏 | 获取串口或网络登录证据再判断；不以盲刷代替诊断 | 系统恢复 | [实测] 2026-09-13 会话查询 | 2026-09-16 |
| 供电 | 更换独立适配器后在 Loader 可识别 | 此前不枚举的单一原因未确定 | 使用官方要求的 5 V/3 A 电源；PWR 与 USB 数据口区分；额定电流不等于实测电流 | 整板 | [官方]+[用户确认] [供电说明](../../reference/luckfox-aura/wiki/Introduction.html) | 2026-09-16 |

已排除的假设：本板一定在官网四个 SKU 中；8 GB 等于 64 GB；未出现 ADB 就等于没有系统。前两项与实物/读取证据不符，后一项证据不足。内存是否 1 GB 仍待核验。[实测]+[实物照片]+[推测]

## 2026-09-19 应用集成实测补充

- 2026-09-20：生产启动携带 `-jsonl /tmp/fitness_live.jsonl`，持续一夜写到 486 MB，
  占满 494 MB tmpfs，配网状态写入报 I/O error。生产启动已移除逐帧诊断输出并清理旧文件；
  20 秒会话的独立 pose 记录仍保留。实板恢复后 /tmp 使用 2%，STA 与 HTTPS 验证通过。
- OEM 启动器 `RkLunch-RKIPC_RV1126B.sh` 遍历 `/oem/usr/etc/init.d/S??*`，
  `S50nginx.disabled` 仍会执行。必须移出该匹配范围；当前保存在 `chiform-disabled/`。
  已实测 nginx 抢占 80 导致配网页和热点启动失败；门户 ExecStartPre 仅停止厂商 nginx。
- Wi-Fi 状态轮询在断开状态下应保留 `FAILED:`，否则真实错误会被 `IDLE: disconnected` 覆盖。
- `rkipc` 不只是 ISP 3A 服务；它在网卡 link 事件中启动 DHCP 客户端、清空 DNS，
  会破坏 NetworkManager 热点。当前应用改用官方 `rkaiq_3A_server`，保留原 IQ 文件。
- `rga_info.rotation` 是 HAL 枚举，180° 对应 `0x03`，不是整数 `180`。
  本板 aarch64 字段偏移 72，必须用板端头文件和图像测试验证。
- 摄像头缓冲 QBUF 后就可被覆盖；必须等当前帧预览/录像读取完成再归还，
  不能用全局“最近帧 fd”替代当前推理任务的帧。
- 模型反 letterbox 后的坐标属于摄像头尺寸；不能除以缩小后的录像尺寸。
- 强制门户不能只测板内 HTTP：手机可能使用缓存的探测 IP 或外部 DNS，必须验证客户端路由。
  当前增加仅热点入站的 HTTP/DNS DNAT，DNS 返回保留测试地址，避免部分 Android 对 RFC1918
  探测结果直接判无网络；客户端网络模拟已通过，真实手机弹窗仍待确认。
- 证据与验收范围：[2026-09-19 修复记录](../../apps/aura-bringup/evidence/2026-09-19/README.md)。
