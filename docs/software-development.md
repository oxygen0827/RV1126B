# 软件开发基线

## 官方 SDK

Aura SDK 当前官方包为 `Luckfox_Aura_SDK_260521.tar.gz`，Wiki 明确支持并测试 `Ubuntu 22.04 x86_64`。SDK 目录通常包含：

```text
build.sh       # 顶层编译脚本
media/         # 多媒体、ISP 等
sysdrv/        # U-Boot、kernel、rootfs
project/       # 板级配置、示例和脚本
output/        # 编译产物
docs/          # Rockchip 原厂文档
tools/         # 打包与烧录工具
```

建议为 Buildroot 和 Debian 13 使用独立 SDK 副本。编译前执行 `./build.sh lunch`，按 SKU/启动介质选择 `BoardConfig`；Buildroot 编译不要使用 `sudo`，Debian live-build 流程按官方文档要求使用 root 权限。

## 应用与 AI

- `upstream/rknn-toolkit2/`：官方 RKNN Toolkit2，包含转换、量化、API 文档和运行库；模型转换环境按其版本说明配置。
- `upstream/rknn_model_zoo/`：官方 RKNN Model Zoo，包含 C/Python 推理示例和模型适配。
- `upstream/luckfox-aura-docs/`：Aura 文档仓库中的板级资料、示例代码目录和官方链接。
- 后续本地应用放在 `apps/`；脚本放在 `scripts/`。不要直接修改上游仓库，也不要把模型/固件二进制提交进主仓库。

## 推荐开发顺序

1. 先用官方镜像验证启动、串口、网络、USB 和存储。
2. 在 Ubuntu 22.04 上用官方 SDK 完成一次未修改的镜像构建，保留 commit、配置和日志。
3. 单独验证 RKNN C/Python 最小推理，再接入 CSI/ISP/媒体链路。
4. 最后建立应用、服务、自定义 rootfs 和量产刷写流程。
