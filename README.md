# SiYuan Unlock + SFTP

本仓库维护 SiYuan 的解锁补丁、SFTP 云存储适配及自动发布流程。
跟踪 [appdev/siyuan-unlock](https://github.com/appdev/siyuan-unlock) 的稳定 release，
构建时拉取 [官方 SiYuan](https://github.com/siyuan-note/siyuan) 对应版本 tag，再应用补丁。
仓库不保存完整上游源码或第三方依赖副本。

下载：[GitHub Releases](https://github.com/lovitus/siyuan-unlock-sftp/releases)。
容器镜像：`ghcr.io/lovitus/siyuan-unlock-sftp`，仅发布到 GHCR。

## 仓库结构

| 路径 | 用途 |
| --- | --- |
| `patches/siyuan/` | 上游解锁补丁及版本兼容修正 |
| `patches/sftp/` | SFTP 配置、界面、provider 注册及 Go 依赖变更 |
| `patches/siyuan-android/`、`patches/siyuan-ios/` | 移动端构建补丁 |
| `overlays/kernel/sftpcloud/` | SFTP 存储适配实现及集成测试 |
| `scripts/` | 拉取源码、应用补丁、跟踪 release 与验证脚本 |
| `.github/workflows/` | 定时跟踪、独立测试及各平台发布流程 |
| `docs/SFTP.md` | SFTP 配置、发布配置与验证说明 |

同步、冲突处理、快照、备份与恢复复用 SiYuan 的 DejaVu 公共组件。
SFTP 适配层实现存储接口，不维护另一套同步算法。

## SFTP 配置

设置 → 同步 → SFTP，填写服务器、端口、用户名、密码、远程路径和服务器 SHA256 主机密钥指纹。
**必须显式指定已有、可读写的绝对远程 path**，例如 `/srv/siyuan`；不默认使用 SSH 主目录。
目前支持密码认证。详细语义和限制见 [SFTP 说明](docs/SFTP.md)。

## 自动发布

GitHub Actions 每 6 小时检查一次上游稳定 release，也支持手动运行
[Track upstream releases](https://github.com/lovitus/siyuan-unlock-sftp/actions/workflows/release-cron.yml)。
所有新稳定版本都会自动尝试应用当前补丁，无需加入版本白名单。
先检查桌面端和容器补丁；patch 失败时停止后续构建，等待人工修复兼容性。
打补丁并构建成功后，在 `lovitus/siyuan-unlock-sftp` 发布 release；失败保留草稿以便重试。
已发布版本跳过，不会因修改补丁而自动覆盖。无需本地定时器。

保留的构建产物：

- Linux amd64 tar.gz / AppImage、Linux arm64 tar.gz
- macOS amd64 / arm64 DMG、Windows amd64 EXE
- Android arm64 原版 / AppDev APK、iOS IPA
- 容器 linux/amd64、linux/arm64、linux/arm/v7、linux/arm/v8

Android 签名使用仓库 secrets `KEYSTORE` 和 `KEYSTORE_PASSWORD`。
完整发布流程与 GHCR 配置见 [自动发布说明](docs/SFTP.md#automated-github-release)。

## 本地验证

需要 Git、Bash 和对应上游版本要求的 Go；前端验证另需 Node.js 和 pnpm。
Go、前端依赖及版本由拉取源码中的清单管理；SFTP 新增依赖通过
`patches/sftp/provider.patch` 修改 `kernel/go.mod` 和 `kernel/go.sum`。

```sh
bash scripts/prepare-sftp-source.sh v3.8.3 /tmp/siyuan-sftp-build
cd /tmp/siyuan-sftp-build/kernel
go test -race ./sftpcloud
go build -tags 'fts5 sqlcipher' .
```

在本仓库验证发布脚本与 workflow：

```sh
python3 scripts/test-release-tracking.py
actionlint
```

修改发布源码时，更新 `patches/` 或 `overlays/`；临时拉取的完整源码不提交到本仓库。
`Test SFTP patch` workflow 独立验证 SFTP 及真实 DejaVu 备份恢复流程。

## 来源与许可

基于 [SiYuan](https://github.com/siyuan-note/siyuan) 和
[appdev/siyuan-unlock](https://github.com/appdev/siyuan-unlock)，保留 [AGPL-3.0 许可证](LICENSE)。
构建使用的上游源码可由版本 tag 和本仓库补丁重建。
