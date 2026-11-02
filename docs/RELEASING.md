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

推送标签只触发该标签提交已经包含的工作流；工作流没有追溯作用。手动运行则使用所选维护分支的工作流定义，另行检出输入标签的源码。仓库须允许 Actions，且发布任务的 `GITHUB_TOKEN` 允许 `contents: write`。这里不提供个人访问令牌或自动修改仓库设置。

## 手动编译并发布

从 v0.2.2 起，Release 支持 `workflow_dispatch`。先将本版工作流推送到默认分支，否则页面不会出现手动运行按钮。操作者需要仓库写权限，目标版本标签也必须已经存在于 GitHub；此功能不创建或移动标签。

1. 打开仓库 **Actions → Release → Run workflow**。
2. 在 **Use workflow from** 选择包含本版发布流程的维护分支，通常是默认分支。
3. 在 **tag** 输入目标标签，例如 `v0.2.2`，再点击 **Run workflow**。
4. 等待完整测试、两个原生架构构建和发布全部成功，在 **Releases** 下载附件及 `SHA256SUMS`。

也可使用 GitHub CLI（替换 OWNER/REPO 和实际维护分支）：

```bash
gh workflow run release.yml --repo OWNER/REPO --ref main -f tag=v0.2.2
```

所选分支提供工作流定义，`tag` 决定源码版本：先严格解析 `refs/tags/版本` 得到完整提交哈希，随后测试、打包和发布说明均检出该提交。不会将同名分支当作标签，标签缺失或格式不符会失败。目标源码须包含本流程所需的构建/校验脚本、独立 BUILDINFO 产物及对应带日期的 CHANGELOG 段落；旧发布流程不是自动迁移对象。发布前还核对远端标签指向，运行中标签被改动不会被悄悄接受。

**已有同名 Release（含草稿）时仍拒绝覆盖。** 手动触发会重新验证/编译，但不能用来替换已发布 v0.2.1 的附件；新产物可在此次 Actions 的 artifacts 中查看，不能据此宣称已更新 Release。需要重新发布改动时使用新版本，勿删除原 Release 或移动标签来绕过检查。成功创建 Release 后立即公开，预发布标签会标记 prerelease；没有单独的发布确认按钮。

手动触发与标签自动触发按目标版本串行执行，避免同时发布。失败后可以重跑，但已经创建的 Release 不会自动修复或覆盖，需先核对失败位置。

## 发布流水线

`.github/workflows/release.yml` 在 `v*` 标签 push 或手动指定已有版本标签时运行：

1. 检查版本格式、标签指向、干净工作区和对应 CHANGELOG 段落。
2. 执行完整 Validate：竞态测试、vet、构建、打包回归、进程、Chrome 和四个 fuzz 各 30 秒。
3. 分别在 `ubuntu-24.04`/`ubuntu-24.04-arm` 原生构建 Linux amd64/arm64，并运行解包后的 `--version`、帮助、独立 `init` 和 `check` 烟雾检查。完整应用回归在 amd64 执行，arm64 不宣称完成同等全套回归。runner 配额/可用性不足会使任务失败，不回退到未验证交叉编译。
4. 汇总两个 `.tar.gz`、两个架构各自的 `.BUILDINFO` 和独立 `LICENSE`，生成覆盖这五个附件的 `SHA256SUMS`，从 CHANGELOG 提取本版本发布说明。只有最后的 `publish` 任务有仓库写权限；它重新核对远端标签的提交，不运行源码中的构建脚本。
5. **自动发布 Release**：`gh release create` 不带 `--draft`，无需管理员再点击发布；预发布后缀会标记 prerelease，同样可见。已有同名 Release（包括草稿）不覆盖，失败重跑也不使用 `--clobber`；若留下不完整草稿或 Release，先人工核对再处理，不假定跨 API 操作具有事务性。

### 自动公开的影响与生效范围

公开仓库的匿名访客可看到发布说明，并下载两个二进制归档、SHA256SUMS、独立的 LICENSE 和两份 BUILDINFO；BUILDINFO 包含版本、完整来源提交、构建时间、Go 版本和目标平台。文档和 systemd 单元留在源码仓库。源码及标签本身的可见性不因这个开关改变。私有仓库仍受其访问权限限制，不会因移除 `--draft` 变成公开仓库。

归档继续采用文件白名单，不包含真实用户配置、内容、状态密钥、访问码、日志、TLS 私钥或浏览器资料；编入二进制的默认配置示例仍然存在。测试、校验、标签检查与 token 写权限范围不变，但取消了发布前最后一次人工确认。因此说明及包内文件需要在打标签前完成审核，发布后不能撤回他人已经下载的副本。Actions 中间产物仍是独立对象，不会因此取消它们原有的下载权限要求。

自动触发的新策略仅对包含本次工作流的新标签生效。修改默认分支或重跑旧标签任务不会使旧标签改用新文件；显式手动运行可使用维护分支的工作流，但仍不覆盖已有 Release。现有 v0.2.0 草稿仍须管理员核对附件后手工发布。不要移动旧标签，也不要通过重复创建同名 Release 强制补发。当前无论常规版本还是 prerelease，工作流都没有额外的人工发布闸门。

本次修订的 v0.2.1 起，每个 `.tar.gz` **恰好包含根目录下的一个可执行文件 `filegate`，没有外层版本目录或其他文件**。LICENSE 和 `filegate_VERSION_linux_ARCH.BUILDINFO` 为独立附件；使用或再分发二进制时请一并保留许可说明。用户/安全/部署文档及 systemd 单元从对应版本源码取得。用户明确要求将这次修改合并到本地 v0.2.1 提交并重新打标签；此前已发布的附件不会自动替换，本地重打标签也不会更新远端标签。对其他既有标签与资产仍保持不覆盖策略。

归档不含用户配置、内容、状态、日志、密钥或浏览器资料。配置由安装后的 `init` 为新部署生成，已有部署不得重新初始化。Actions 中间产物保留 7 天；实际限制仍受仓库策略控制。浏览器失败可能留下测试凭据，不自动上传测试目录。

构建使用 `CGO_ENABLED=0`、`-trimpath`、`-s -w`，发布版本来自标签，构建时间取该提交的 UTC 时间。`BUILDINFO` 记录完整来源提交、Go 版本和平台；二进制显示标签而非额外拼接长哈希。归档排序并规范权限/时间及 gzip 头；未完成跨机器相同工具链复现验收，不承诺逐字节可复现。

## 下载、校验和安装

从实际仓库的 Releases 页面下载对应架构的包和 `SHA256SUMS`，不要把 Actions 的临时产物当作已发布版本。以下以实际下载文件名替换占位符；先校验再解包：

```bash
# 将对应包和 SHA256SUMS 放在同一目录；只验证已下载架构的条目。
awk '$2 == "filegate_vX.Y.Z_linux_amd64.tar.gz" {print}' SHA256SUMS > selected.sha256
sha256sum --check selected.sha256
tar -xzf filegate_vX.Y.Z_linux_amd64.tar.gz
./filegate --version
```

请在空的工作目录中解压，避免覆盖已有程序；旧包仍按其原有外层目录进入。校验条目必须与完整文件名匹配；命令应报告 `OK`，空选取或未验证不能算成功。如需验证下载的 LICENSE 和 BUILDINFO，选择它们的完整文件名对应条目。SHA256 能核对下载字节，不提供独立身份认证；目前未提供发行物签名或 provenance attestation。

预编译用户无需 Go：按 [systemd 部署指南](../deploy/README.md)准备专用账号和权限，跳过指南中的 `make build`，从解包目录安装 `./filegate`，再执行首次初始化或已有部署的升级步骤。源代码构建仍使用 `make build`。

已有部署的停服备份、二进制回退和恢复后的旧码撤销，按 [运维指南](OPERATIONS.md)操作；不要在升级时重新运行 `init`。

## 本地验证打包

```bash
make test-release
# 下面必须在包含对应 CHANGELOG 且标签指向 HEAD 的干净源码树运行。
make dist VERSION=vX.Y.Z RELEASE_ARCH=amd64
make dist VERSION=vX.Y.Z RELEASE_ARCH=arm64
```

`make dist` 创建 `tmp/dist/` 下的单架构包及独立 BUILDINFO，不创建标签、Release 或上传文件，也不代替发布前测试；正式发布还附上对应提交的 LICENSE。可交叉编译；原生烟雾验证使用 `bash scripts/smoke-release.sh VERSION ARCH`，要求主机架构匹配。已有同名归档或 BUILDINFO 会拒绝覆盖，重新打包前只移除已核对的旧产物。`make test-release` 在独立临时 Git 仓库创建虚构标签，验证脏工作区、标签/CHANGELOG 不匹配、精确的单文件归档、独立元数据、原生烟雾和 CI 范围选择，不改变当前仓库标签。

流程参考 GitHub 官方的 [复用工作流](https://docs.github.com/en/actions/how-tos/reuse-automations/reuse-workflows)、[runner 镜像清单](https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md) 和 [gh release create](https://cli.github.com/manual/gh_release_create)。本地验证不等于已在 GitHub 云端运行，首次接入后应检查真实 Actions 结果。

手动操作依据 GitHub 官方的 [手动运行工作流](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow?tool=webui)。
