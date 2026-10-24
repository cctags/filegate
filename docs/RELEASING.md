# CI 与版本发布

## 自动检查

源码中的 `.github/workflows/ci.yml` 在分支 push、pull request 时运行，也可在 Actions 页面手动触发。使用 GitHub 托管的 Ubuntu 24.04、`go.mod` 指定的 Go 版本、Node 24 和 runner 已安装的 Chrome；Go 生产代码仍无第三方依赖。不使用 `pull_request_target`，不向 PR 测试授予写权限或传递部署密钥。

- 仅 Markdown/许可证变更跳过 Go 测试；仍执行范围判定和最终 `result` 状态检查。首个 push 或无法读取比较基线时按全部源码处理。
- 其他变更执行格式、竞态测试、vet、构建及打包回归；CLI、认证/状态和测试基础设施变更增加进程测试，HTTP/共享 gate 逻辑和浏览器/CI 基础设施变更增加 Chrome。
- 手动运行 CI 时默认启用完整模式，额外运行进程、浏览器和四个 fuzz 目标各 30 秒。
- `validate.yml` 供 CI 与发布共用。测试必须以非 root 用户运行，必要 Chrome/Go 不存在则失败，不把跳过当作通过。浏览器和进程测试只操作仓库 `tmp/` 下的独立数据。

分支保护可要求 `CI / result`；应在实际仓库确认状态名称及规则。Actions 全部固定提交 SHA，升级时核对官方来源、输入参数和 runner 要求。Go 下载遵守 `go.mod`，不可因 runner 未缓存或下载失败而悄悄降级；需定期更新受支持的 Go 安全补丁。当前尚未把 govulncheck 加入自动发布门槛。

## 准备版本

发布采用新的 `vMAJOR.MINOR.PATCH` 标签，可追加 `-rc.1` 等预发布后缀；不接受历史的两段式 `v0.1` 作为新发布格式。不要移动已发布的标签。现有 `v0.1` 仍指向历史提交，工作流不会为它补建当前版本发行物。

1. 将 CHANGELOG 中本次 Unreleased 条目归入 `## [vX.Y.Z] - YYYY-MM-DD`，填写实际版本和日期，保留新的 Unreleased 区域；核对升级和安全说明。
2. 按源码仓库的 `AGENTS.md` 和 [测试指南](../tests/README.md) 对拟发布提交完成全套检查。提交所有改动，确认工作区干净。手动浏览器、媒体及 systemd 主机验收单独记录，自动检查不能替代它们。
3. 由维护者确认目标提交，再创建附注标签并推送该标签到实际 GitHub 仓库。本次开发不会创建标签、配置远端或推送。

示例中的 `vX.Y.Z` 必须替换为实际新版本，`origin` 必须是已核对的仓库：

```bash
git status --short
git log -1 --oneline
git tag -a vX.Y.Z -m 'Filegate vX.Y.Z'
git push origin vX.Y.Z
```

推送标签只触发该标签提交已经包含的工作流；工作流没有追溯作用。仓库须允许 Actions，且发布任务的 `GITHUB_TOKEN` 允许 `contents: write`。这里不提供个人访问令牌或自动修改仓库设置。

## 发布流水线

`.github/workflows/release.yml` 在 `v*` 标签 push 时运行：

1. 检查版本格式、标签指向、干净工作区和对应 CHANGELOG 段落。
2. 执行完整 Validate：竞态测试、vet、构建、打包回归、进程、Chrome 和四个 fuzz 各 30 秒。
3. 分别在 `ubuntu-24.04`/`ubuntu-24.04-arm` 原生构建 Linux amd64/arm64，并运行解包后的 `--version`、帮助、独立 `init` 和 `check` 烟雾检查。完整应用回归在 amd64 执行，arm64 不宣称完成同等全套回归。runner 配额/可用性不足会使任务失败，不回退到未验证交叉编译。
4. 汇总两个 `.tar.gz` 和 `SHA256SUMS`，从 CHANGELOG 提取本版本发布说明。只有最后的草稿创建任务有仓库写权限；它重新核对远端标签的提交，不运行源码中的构建脚本。
5. 创建 **Draft Release**。管理员核对说明、资产、校验和及验证结果后，在 GitHub 手工发布；预发布后缀会标记 prerelease。已有同名 Release 不覆盖，失败重跑也不使用 `--clobber`；若留下不完整草稿，先人工核对再处理。

每个包包含二进制、`BUILDINFO`、MIT 许可、用户/安全/变更文档及 systemd 部署文件；不含用户配置、内容、状态、日志、密钥或浏览器资料。配置由安装后的 `init` 为新部署生成，已有部署不得重新初始化。Actions 中间产物保留 7 天；实际限制仍受仓库策略控制。浏览器失败可能留下测试凭据，不自动上传测试目录。

构建使用 `CGO_ENABLED=0`、`-trimpath`、`-s -w`，发布版本来自标签，构建时间取该提交的 UTC 时间。`BUILDINFO` 记录完整来源提交、Go 版本和平台；二进制显示标签而非额外拼接长哈希。归档排序并规范权限/时间及 gzip 头；未完成跨机器相同工具链复现验收，不承诺逐字节可复现。

## 下载、校验和安装

从实际仓库的 Releases 页面下载对应架构的包和 `SHA256SUMS`，不要把 Actions 的临时产物当作已发布版本。以下以实际下载文件名替换占位符；先校验再解包：

```bash
# 将对应包和 SHA256SUMS 放在同一目录；只验证已下载架构的条目。
awk '$2 == "filegate_vX.Y.Z_linux_amd64.tar.gz" {print}' SHA256SUMS > selected.sha256
sha256sum --check selected.sha256
tar -xzf filegate_vX.Y.Z_linux_amd64.tar.gz
cd filegate_vX.Y.Z_linux_amd64
./filegate --version
```

校验条目必须与完整文件名匹配；命令应报告 `OK`，空选取或未验证不能算成功。SHA256 能核对下载字节，不提供独立身份认证；目前未提供发行物签名或 provenance attestation。

预编译用户无需 Go：按 [systemd 部署指南](../deploy/README.md)准备专用账号和权限，跳过指南中的 `make build`，从解包目录安装 `./filegate`，再执行首次初始化或已有部署的升级步骤。源代码构建仍使用 `make build`。

## 本地验证打包

```bash
make test-release
# 下面必须在包含对应 CHANGELOG 且标签指向 HEAD 的干净源码树运行。
make dist VERSION=vX.Y.Z RELEASE_ARCH=amd64
make dist VERSION=vX.Y.Z RELEASE_ARCH=arm64
```

`make dist` 只创建 `tmp/dist/` 下的单架构包，不创建标签、Release 或上传文件，也不代替发布前测试。可交叉编译；原生烟雾验证使用 `bash scripts/smoke-release.sh VERSION ARCH`，要求主机架构匹配。已有同名归档会拒绝覆盖，重新打包前只移除已核对的旧产物。`make test-release` 在独立临时 Git 仓库创建虚构标签，验证脏工作区、标签/CHANGELOG 不匹配、包隔离、原生烟雾和 CI 范围选择，不改变当前仓库标签。

流程参考 GitHub 官方的 [复用工作流](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows)、[runner 镜像清单](https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md) 和 [gh release create](https://cli.github.com/manual/gh_release_create)。本地验证不等于已在 GitHub 云端运行，首次接入后应检查真实 Actions 结果。
