# Filegate 会话恢复记录

更新时间：2026-10-24。本文保存开发上下文，供新会话接手；具体实现以源码、Git 历史和当前配置为准。

## 1. 从这里恢复

1. 阅读本文，再读 `AGENTS.md` 和 `README.md`。
2. 运行 `git status --short`、`git log -5 --oneline`，核对本文之后是否还有变更。
3. 运行 `./tmp/filegate --version` 查看本地构建；二进制不存在时先 `make build`。
4. 确认用户下一步意图。**上传功能、HTML 渲染预览都只讨论过，尚未要求实现。**
5. 不要重新初始化已有的 `tmp/demo/config.json`，不要清理用户文件、状态或黑名单。

可直接给新会话的提示：

> 请先阅读当前目录的 SESSION_STATE.md、AGENTS.md 和 README.md，检查 Git 状态后继续。项目是 Go 1.27.3+ 标准库实现的 Filegate，仅 IPv4，所有临时文件放仓库 tmp/，提交采用 Linux kernel 风格且不要 Signed-off-by。最近的安全加固实现和验证见本文及 SECURITY_REVIEW.md。上传功能与 HTML 渲染预览尚未实施；最近已提交控制台/文件双输出、轮转及保留期限日志；最新 CLI 已把操作改成普通子命令，选项仍用双横线；最新版本显示仅去掉单独的长哈希行，保留 git describe。请根据我接下来的要求推进。

## 2. 项目目标与已确认约束

- 工作目录：`/home/jsh/code/AI/webui-ai`。
- Linux 私有文件浏览服务，浏览器通过服务器 IP 和端口访问。
- **只使用 Go 标准库**，不引入第三方库、前端框架或数据库。
- 单个静态二进制部署；界面 HTML/CSS 编入程序。配置、内容、状态在外部文件中。
- 管理员通过服务器终端管理，**不提供 Web 管理页面**。
- 用户没有 OTP 密钥。最终采用管理员生成并发送的**随机一次性访问码**，不采用 TOTP。
- 访问码为配置别名加八位数字，例如 `ch01234567`；成功登录一次即作废，有效期可配置或生成时覆盖。
- 用户只能浏览该访问码授权的目录；管理员永久黑名单和自动临时封禁均已实现。
- 配置注释采用 **JSONC**，保留默认文件名 `config.json`；也支持显式指定 `.jsonc`。
- **所有生成物、缓存、测试临时文件都放仓库的 `tmp/`，不要使用系统 `/tmp`。** Makefile 已设置相关环境变量。
- 提交信息参考 Linux kernel：`subsystem: Imperative subject`，正文说明原因、行为和验证；**不要添加 `Signed-off-by`**。
- 用户通常在功能完成后单独要求提交；不要把方案讨论当作实现授权。

## 3. 当前 Git 和构建状态

整理本文时分支为 `master`，最新功能提交（文档提交之前的基线）为：

```text
a2e373a3e0902d9f6041f0ed3db16b6b2fc7ba92
cli: Use bare subcommands for actions
```

2026-10-23 版本显示调整开始前实际 HEAD 为 `a2e373a`，工作区干净。此前尝试固定 v0.1 并拼接短哈希的未提交方案已撤回；最终仅移除独立长哈希行，保留原 Git 版本来源；相关代码、测试及文档随本次版本输出提交保存。恢复时须用 `git status`、`git log -1` 核对实际状态。

最近一次已提交功能基线的二进制曾输出如下（版本显示调整已重新构建，当前实际版本请运行 `--version` 核对）：

```text
filegate a2e373a
commit: a2e373a3e0902d9f6041f0ed3db16b6b2fc7ba92
modified: false
built: 2026-10-23T12:50:50Z
go: go1.25.2
platform: linux/amd64
```

这是磁盘二进制的信息，不代表运行中的服务已经换成该版本。新增文档或后续修改后重新构建，可能出现 `-dirty` / `modified: true`，属于预期。

主要提交：

| 提交 | 内容 |
| --- | --- |
| `a2e373a` | 操作改为普通子命令、选项继续使用双横线 |
| `6266aa6` | 彩色 console、轮转文件日志、保留期限及访问记录 |
| `6b195a7` | 文档同步维护规则 |
| `8d2989e` | 保存恢复文档并在贡献指南中添加入口 |
| `101b4c2` | 文件点击优先预览，独立下载链接 |
| `3095309` | JSONC 注释解析、带中文字段说明的默认配置、默认中文目录 |
| `474eb7a` | 帮助中的默认值和完整目录配置流程 |
| `c353af5` | SIGHUP 热重载目录与运行参数 |
| `b01024f` | 修复来源策略导致浏览器表单被拒绝 |
| `7690315` | 更新贡献者指南 |
| `cdb6ec7` | 历史 CLI 曾全部使用双横线；已由本轮子命令调整替代 |
| `f543a75` | `--version` 与构建元数据 |
| `c86ef1c` | `-s -w` 精简二进制 |
| `4b84d86` | 首个可用实现 |

### 当前版本显示调整

用户确认采用最小改动：撤回固定 `v0.1` 和手工拼接版本的方案，保留 Makefile 原有 `git describe --tags --always --dirty`；仅删除 `--version` 的独立 `commit:` 长哈希行和不再使用的读取逻辑，其他元数据保留。启动日志、版本默认值 `dev` 和 Makefile 均不改。

2026-10-24 曾在 `78a9ea8`（`cli: Omit the full commit hash from version output`）创建本地附注标签 `v0.1`；随后按用户明确要求，将标签移到最新提交 `122566e`（`cli: Refuse to serve with root privileges`），替换原指向。多语言开发开始前 HEAD 为 `122566e`、工作区干净，未配置 Git remote，也未推送标签。原逻辑在标签上只显示标签，在标签之后显示标签+提交数量+短哈希，没有标签时退回短哈希，有未提交改动时加 `-dirty`；不增加 `--long` 或其他 Git 参数。上述旧二进制快照里的 `commit:` 仅供历史参考。打标签不会自动更新已有二进制的内嵌版本。

## 4. 源码地图

| 路径 | 作用 |
| --- | --- |
| `cmd/filegate/main.go` | 默认配置模板、初始化、服务启动、信号处理 |
| `cmd/filegate/options.go` | 长选项解析与完整帮助 |
| `cmd/filegate/version.go` | 版本、Git 状态、构建时间、Go 版本、平台 |
| `cmd/filegate/*_test.go` | CLI、帮助、版本和初始化测试 |
| `internal/gate/config.go` | 配置读取、严格字段验证、路径解析、原子写文件 |
| `internal/gate/jsonc.go` | 注释扫描，保留字符串、换行和字节位置 |
| `internal/gate/store.go` | 状态、HMAC、会话、目录句柄、黑名单与清理 |
| `internal/gate/admin.go` | Unix socket 管理通信、管理操作、单实例文件锁 |
| `internal/gate/http.go` | 登录、退出、目录列表、文件请求、来源与权限校验 |
| `internal/gate/reload.go` | 准备、验证、持久化撤销、原子切换配置 |
| `internal/gate/preview.go` | 预览类型选择、头部嗅探、文本编码 |
| `internal/gate/accesslog.go` | HTTP 状态/字节计数与访问日志，不记录查询和正文 |
| `internal/gate/access_link.go` | 链接准备、干净确认页与 POST 确认 |
| `internal/gate/language.go` | 编入程序的中英文文本、浏览器语言选择、独立语言偏好 Cookie |
| `internal/logging/` | 双输出 handler、终端颜色检测、日志锁、轮转、定期清理及测试 |
| `internal/gate/*_test.go` | 状态、隔离、来源、重载、JSONC、预览等测试 |
| `README.md` | 中文使用说明和配置示例 |
| `AGENTS.md` | 贡献者与开发约束；约 400 个英文词 |
| `Makefile` | 构建、测试、vet；所有缓存指向仓库 tmp/ |

## 5. 运行和管理

```bash
make build
./tmp/filegate --help
./tmp/filegate --version

# 仅用于尚无配置的新部署；不会覆盖已有配置
./tmp/filegate --config tmp/new-demo/config.json init

# 实际已有部署
./tmp/filegate --config tmp/demo/config.json serve

# 在另一个终端、以服务用户执行
./tmp/filegate --config tmp/demo/config.json code ab
./tmp/filegate --config tmp/demo/config.json code ab --ttl 30m
./tmp/filegate --config tmp/demo/config.json status
./tmp/filegate --config tmp/demo/config.json revoke CODE_ID
./tmp/filegate --config tmp/demo/config.json logout ab
./tmp/filegate --config tmp/demo/config.json block 203.0.113.25
./tmp/filegate --config tmp/demo/config.json block 198.51.100.0/24
./tmp/filegate --config tmp/demo/config.json unblock 203.0.113.25
```

- 使用返回的 `code` 登录；`id` 仅用于撤销，不能作为登录码。
- 操作使用普通子命令，如 `serve`、`code ch`；选项保留 `--config`、`--ttl`，帮助/版本保留 `--help`、`--version`。不支持旧的 `--serve`、`--code`、单横线选项或位置有效期 `code ab 30m`。选项可在子命令及参数之前、之间、之后。
- 支持 `--option=value` 和任意选项顺序；每次恰好一个操作。
- 管理命令通过状态目录内 `admin.sock` 与运行中的服务通信；不是 HTTP 管理 API。
- Unix socket 权限 `0600`，状态目录 `0700`；需要同一系统用户或 root。
- 管理命令要求服务运行；`--help`、`--version`、`init` 不要求。

### 专用服务用户（2026-10-24）

用户已确定今后正式部署使用专用系统用户，通常命名 `filegate`，禁用交互登录；本地开发可使用开发用户。README 已记录权限分配、sudo 管理命令及 systemd `[Service]` 示例。二进制、配置和部署目录由管理员拥有，内容仅需服务读取，state/logs 由服务用户写入。黑名单原子替换要求父目录可写，推荐部署配置使用 `state/blacklist.txt`；这不是生成模板的新默认值。已有部署迁移须停服保留黑名单，不重做 init。

后续补充了具体账号创建方法：先 `getent passwd filegate` 检查，再使用 useradd 的 --system、--user-group、--no-create-home、--home-dir /nonexistent、--shell /usr/sbin/nologin，并用 passwd --lock 锁定密码。README 说明 nologin 的发行版路径差异、账号/组/密码状态核对，以及 sudo -u 和 systemd 不依赖登录 shell。此轮仅写文档，未实际创建账号、修改系统或启动服务；按本机 useradd 手册和 passwd 帮助核对选项，git diff --check 验证，不重复运行代码测试。账号创建说明与恢复记录随本次文档提交保存。

文档先按用户要求提交为 `a8d1c6d`（`docs: Describe deployment with a dedicated service user`），随后实现启动检查：仅 `serve` 使用 `os.Geteuid()` 检查，有效 UID 为 0 时在参数校验后、配置读取和其他副作用前拒绝启动，提示切换用户；帮助、版本、初始化及本地管理不受限制。允许其他非 root 用户名，不自动降权或创建用户，不提供 root 豁免开关。`--help` 已同步说明。测试通过内部显式传入有效 UID 的入口覆盖 root 和非 root 分支，无须实际更改系统身份。

验证：`go test -race ./... -skip '^TestAdminSocketAndLock$'` 通过（仍跳过沙箱限制的本地 socket 测试）；`make vet`、`make build`、二进制帮助/版本检查和 `git diff --check` 通过。首次 vet 与竞争测试并行运行时误扫描到仓库 tmp/build 中的临时 Go 源码而失败，等待测试完成后单独重跑 vet 通过；后续针对 `./...` 的检查应顺序执行，避免该临时目录扫描冲突。未实际切换 root 身份或监听服务，UID 分支由自动测试覆盖；未创建系统用户、安装服务、移动运行文件或修改部署配置。

### Web 多语言（2026-10-24）

用户已授权并实现简体中文和英文 Web 界面，包括登录、目录列表及应用错误提示。按 `Accept-Language` 的 q 权重选择支持语言，同权重按请求顺序；zh 各变体使用简体中文，en 各变体使用英文；没有匹配时保留默认简体中文。顶部提供简体中文、English、跟随浏览器三种链接，GET `/`、`/browse/...` 的 `lang=zh-CN|en|auto` 手动选择优先于 Cookie，再优先于浏览器；非法值忽略，POST 和文件 URL 不接受语言切换。

语言 Cookie 为 `filegate_language`，Path=/、HttpOnly、SameSite=Strict、HTTPS 时 Secure，有效期 180 天；auto 删除偏好，登录退出不删除。没有新增服务端会话、配置字段或依赖；所有翻译编入二进制。HTML 和应用错误设置 Content-Language 与 Vary: Accept-Language, Cookie。文件原始数据不添加界面语言标记。CLI、日志英文，配置注释中文；Go 标准库协议级错误仍为英文。

目录切换语言保留原目录，中文及含 ?/# 的路径通过 URL 转义；登录错误页切换回 `/`，不在 URL 携带访问码。封禁、来源检查、CSRF、访问码单次消费、目录隔离、下载参数保持原验证。未进行真实浏览器端到端测试，使用 httptest 验证页面及请求流程。发布 Actions 仅讨论过，尚未实现发布脚本或 workflow。

验证：`go test -race ./... -skip '^TestAdminSocketAndLock$'`、`make vet`、`make build`、`git diff --check` 均通过；本地 Unix socket 测试仍按沙箱限制跳过，未运行真实浏览器。用例覆盖语言权重、偏好优先级/清除、Cookie 属性、中英文页面/错误、登录退出、目录隔离、封禁、特殊路径和原始文件下载。检查和测试顺序执行，避免仓库 tmp/build 临时源码扫描冲突。没有改动实际运行配置或重启服务；多语言代码、测试和文档随本次提交保存。

### 默认端口调整（2026-10-24）

用户反馈 8080 容易与其他服务冲突，新部署的默认监听地址改为 `0.0.0.0:17342`。已同步 init 模板、CLI 帮助和 README 的 HTTP/HTTPS 示例。配置加载仍要求显式 listen，不新增省略字段的回退逻辑；已有配置不自动迁移。已有服务要改端口需编辑 listen 并重启，SIGHUP 不支持监听地址变更。没有改动实际运行配置或启动服务；来源校验和重载测试中的 8080 仅为测试地址，不代表产品默认值。

验证：CLI 包竞争检测测试（含初始化配置加载）、`make build`、二进制帮助中的新端口及 `git diff --check` 通过。仅修改模板、帮助和文档，未重复运行 HTTP/状态测试或 vet；端口模板、帮助和文档随本次提交保存。

### 安全审查（2026-10-24）

用户要求仔细检查并对照公开漏洞，已完成手工代码审查、官方公告比对和本地夹具验证。基线 HEAD `d5d57d9`，审查当时二进制 Go 1.25.2。详细结论在 `SECURITY_REVIEW.md`，README 已添加入口；审查阶段仅生成报告，随后用户授权并完成下节所述加固。后续开发应优先阅读报告的修复状态和下节记录。

主要问题：R1 登录持有全局锁读取客户端表单，未授权慢请求可阻塞 Web/管理；R2 Go CVE-2026-39822 在当前工具链可复现尾斜杠符号链接目录列表逃逸，特定 OpenRoot 配置还可越界读文件；R3 2000 段 Range 对 4 KiB 文件产生约 282 KiB 响应，调用路径命中 GO-2026-6609；R4 HTTPS 默认 HTTP2 路径命中 GO-2026-6617，未实测恶意帧崩溃；R5 外部配置符号链接指向 content 的布局被接受；R6 ab/.. 可映射整个根目录、嵌套别名不被拒绝；R7 到期验证用读请求体前的旧时间；R8 scoped IPv6 地址保留 zone，Prefix.Contains 永不匹配，导致永久封禁及会话撤销失效（默认 IPv4 监听不受该路径影响，未实测真实 IPv6 网络）。不要把有管理员配置/符号链接前提的问题描述为匿名任意文件读取。

验证：原有竞争检测套件（跳过 TestAdminSocketAndLock）、make vet 通过；JSONC fuzz 3 秒、34545 次执行通过。11 项额外审查用例通过，包含观察已知问题的断言和正常鉴权/路径/转义防线检查；这不代表问题修复。用例和结果在忽略的 tmp/security-review/，使用 Go overlay，不加入常规测试；源码用 .gotxt 扩展名避免被 go test ./... 扫到。未运行 govulncheck（未安装）、真实浏览器、socket、HTTP2 崩溃或网络压力测试；也未在更新工具链后复验。

审查阶段建议先升级工具链再修应用层；用户随后已升级到 Go 1.27.3，并明确授权实施修复、去除 IPv6。下面记录后续实现，不再将其视为待授权方案。

### 安全加固实现（2026-10-24）

确认实际 Go 为 1.27.3，go.mod 最低版本同步为 1.27.3。保留八位码、JSONC 和单二进制部署。README、AGENTS、帮助和 SECURITY_REVIEW 的修复状态同步更新，未重启实际服务或改写运行配置。

- R1/R7：ParseForm 在状态锁外，consumeLogin 仅串行验证和落盘；完成读取后重查封禁、撤销、实际过期和目录映射，响应在锁外，持久化后创建会话。新增慢请求/慢响应不阻塞管理、读期间到期/封禁/撤销/重载的回归测试。
- IPv4：listen 只接受明确 IPv4 字面地址和 1–65535 数字端口，网络固定 tcp4；HTTP 对端、block/unblock、黑名单均拒绝 IPv6/作用域/映射地址。旧 IPv6 黑名单需管理员手工移除，自动封禁历史记录到期后自然清理，不匹配 IPv4。
- R5/R6：Load 检查配置真实路径和打开文件身份，支持安全外部符号链接和相对配置路径；拒绝 content 内实际配置。openDirectories 基于 os.Root 打开，再用目录 FD 的 metadata-only 父目录遍历及 os.SameFile 检查根目录、重复和嵌套；同时用于 New/Reload，失败保留旧授权。Linux Openat("..") 仅用于已打开目录的身份/祖先检查，不读取或提供用户请求文件。旧 remap 测试改为独立目标目录以保持原测试意图。
- R2/R4：新工具链构建，加入符号链接尾斜杠拒绝测试；内存 pipe 连接跑真实 TLS/HTTP2，无须创建本机网络 socket。保留 HTTPS/HTTP2，没有退回明文规避问题。
- 固定保护上限：128 TCP 连接（含 TLS/空闲）、128 活跃请求（含 HTTP2），文件 32 全局/4 每 IPv4；超限 HTTP 429、连接暂停 accept。32 KiB 分块写入，每次写刷新 30 秒截止；阻塞写 net.Pipe 测试验证超时，不以固定整体超时破坏大文件。单 Range 128 字节，多段/重复头返回 400；原始目录条目最多 10000，超限 503；每别名最多 10 个活跃码，旧有超额码不擅自撤销。这些是代码常量，不是配置字段。
- make vulncheck 为可选开发入口，govulncheck 尚未安装，未联网安装或执行扫描；缓存和安装说明仍使用仓库 tmp。不新增发布 workflow。

最终验证：Go 1.27.3 下原有与新增竞争检测套件（跳过 TestAdminSocketAndLock）、make vet、make build 和 git diff --check 通过；二进制 init、相对路径加载、帮助和版本烟雾检查通过。真实 TLS/HTTP2 和慢写/连接限额使用 net.Pipe，未绑定实际 TCP 端口，不需沙箱外网络权限；仍未执行本机 Unix socket、浏览器、恶意 HTTP2 帧、真实 IPv6 或 govulncheck。提交前 tmp/filegate 的验证版本为 v0.1-2-gd5d57d9-dirty、go1.27.3；提交后重新构建，实际版本请运行 --version 核对。此前的安全报告、本轮代码、测试和文档随本次安全加固提交保存；不要将历史报告里的旧“漏洞仍存在”断言当作新回归标准。

### 一次性访问链接（2026-10-24）

用户已授权“链接 + 确认按钮”的无 JavaScript 方案，保留原输入框；本轮开发开始前 HEAD 为 dad5c4b，工作区干净。实现 GET /access?code=完整码，创建受限的内存待确认记录后 303 到干净 /access；只有 POST /access 的确认按钮才调用与手动输入共享的 consumeDigest。GET 不验证码有效性、不消费、不建立会话、不增加登录失败次数；HEAD 完全不准备。确认页仅显示从码格式提取的别名和随机 CSRF，不包含原始码或摘要。已有登录只在成功确认后替换，并在页面明确提示。

待确认仅保存 Digest/Alias/CSRF/IP/Expires，独立随机 Cookie filegate_pending，Path=/access、HttpOnly、SameSite=Lax、TLS 时 Secure。Lax 保证外部顶级导航后的干净重定向，POST 另校验同源、Cookie、IPv4 和 CSRF；有效确认只允许一次尝试，随后清 Cookie。固定有效期 5 分钟，全局 1000、每 IP 10，cleanup 回收，重启/关闭清空；每浏览器最新链接替换旧准备，旧标签 CSRF 不可消费新链接。IPv4 改变、准备失效/确认失败要重开原链接；原访问码有效期不被延长。密码学摘要使用现有 HMAC 密钥，实际码的到期、封禁、撤销、映射变更、持久化和并发单次消费仍在共享锁阶段重查。手输和链接共用一个码的一次登录机会。

/access 在最外层限流前设置 Referrer-Policy: origin，包括拒绝/过载响应，避免带码 URL 作为完整 Referer 继续传播；其余页面仍 same-origin，不使用曾导致 Origin:null 的 no-referrer。当时未添加脚本；后续客户端日志仅为生成页面增加哈希允许的固定时区脚本，确认流程仍不依赖 JavaScript。初始 URL 仍可能被历史、平台或代理记录，HTTP 风险仍存在，不能声称干净重定向完全抹去凭据。

CLI 新增仅 code 可用的 --base-url http(s)源地址，在参数解析阶段拒绝用户信息、子路径、查询/片段、IPv6/未指定 IP、非法端口，避免发码后才报参数错；由本地 CLI 在受限 socket 返回码后组装 url 字段，不信任 Host/猜测地址、不增加服务端配置和第三方依赖。省略此项保持旧 JSON 输出。

新增 access_link.go/access_link_test.go 与 cmd/filegate/access_url.go/access_url_test.go；扩展 TLS/HTTP2 内存连接测试用真实 CookieJar 完整跟随 GET 干净跳转和 POST 确认。回归测试已覆盖确认前不登录/消费、HEAD、Cookie属性、同源/CSRF/IP绑定、旧标签/重复/并发、码失效/撤销/重载/封禁/持久化失败、准备上限/回收/重启、原输入备用、日志脱敏和 CLI 参数。最终验证：Go 1.27.3 下 `go test -race ./... -skip '^TestAdminSocketAndLock$'`、make vet、make build、git diff --check 和二进制帮助/版本检查均通过。TLS/HTTP2 CookieJar 用 net.Pipe 完整验证干净跳转和确认登录；仍跳过沙箱受限 Unix socket 测试，未运行真实浏览器或邮件/聊天扫描器，也未执行 govulncheck。链接登录代码、测试和文档随本次提交保存；未改实际配置或重启服务。

### 客户端审计日志（2026-10-24）

用户要求在登录成功/失败及其他事件记录 IP、浏览器、系统、时区等。原有代码基线为 78539d8，开始时工作区干净。新增 client_info.go、client_timezone.go、client_info_test.go，统一请求上下文和 request_id（服务端随机 16 位十六进制，X-Request-ID 返回，不信任传入同名头）。访问记录及登录/失败/来源拒绝/自动封禁/退出等事件带同一编号、socket IP、浏览器/数字版本、OS、设备/客户端种类、偏好语言、HTTP/TLS、时区/偏移及来源，另有 operation/outcome/reason。成功登录后的 HTTP 记录使用本次授权别名，403/429/503 及确认失败区分固定原因；无效/已用/过期码仍合并描述，不保存明文历史。

客户端 UA/CH/语言只解析白名单标签与有界数字/语言标签，不输出原始头，浏览器信息可以被伪造或简化。IP 继续只取直接连接 IPv4，不信任转发头。时区不能由 HTTP 自动获得；曾异步询问脚本偏好，未收到回复，随后说明按推荐方案实现最小本地脚本。脚本只监听 POST 表单提交，附带 client_timezone 和 client_utc_offset，不读取访问码/URL/Cookie、不自动提交，登录和链接确认继续支持禁用 JavaScript。

生成页面仅用 script-src sha256 允许静态常量脚本；render 中 template.JS 仅取该常量，绝不插入客户端数据，实际输出字节与 CSP 哈希有回归测试。文件响应仍使用 baseCSP（禁止脚本），HTML/SVG 等仍为源码。时区字段在读完表单后、状态锁外校验：名称最长 64，字符受限，用本机 IANA 数据库验证，Local/绝对路径/非法值拒绝记录；偏移为 -840..840 分钟（东正西负）。失败仅记 unknown/unverified_form，不影响正常码验证。服务端无 tzdata 时名称可能 unknown，合法偏移仍可记录。没有增加配置字段或嵌入庞大时区库。

时区与偏移在有效会话中仅内存保留，后续访问来源标 session_reported；初次表单标 form_reported，无值 unavailable，未验证 unverified_form。未读取表单就拒绝的请求只能使用头信息及可用的旧会话值。时区不作为权限/IP封禁依据，不写入状态或新的 Cookie。日志时间戳仍为服务器时间。

最终验证：Go 1.27.3 下 `go test -race ./... -skip '^TestAdminSocketAndLock$'`、`make vet`、`make build`、二进制 `--version` 和 `git diff --check` 均通过。额外用 Node 在仓库 tmp 校验静态脚本语法和模拟 DOM，验证只在提交时填字段、重复提交不重复追加、GET 表单排除及 Intl 缺失降级。原有确认、隔离、并发、持久化和日志脱敏测试继续通过。仍跳过沙箱受限的 Unix socket 测试；未运行真实浏览器或 govulncheck。未改实际配置或重启服务；用户随后要求提交，本轮代码、测试和文档随客户端审计日志提交保存。

### unblock 结果说明（2026-10-24）

本轮开发开始前 HEAD 为 30f6007，工作区干净。用户指出 unblock 对不在名单的 IP 提示不明确，随后授权修改。新增可选 JSON unblock 对象，返回 blacklist_removed、temporary_removed、message；无匹配记录仍幂等成功，明确说明未删除记录。单个 IPv4（含 /32）附带 still_blocked 和可选 covering_blacklist；只计算剩余永久条目覆盖，不把网段结果概括为全部解封。日志同步记录计数和提示。未匹配永久条目不再改写黑名单，没有临时删除也不额外保存状态；有效临时封禁按 cleanup 后的记录计数。临时删除保存失败时恢复内存封禁并返回错误，不输出成功结果；永久文件与状态文件仍非跨文件事务，保存状态失败可能发生在永久条目已删除之后，应修复错误后检查 status 或重试。

新增无匹配、仅临时/仅永久/两者同时、较大网段覆盖、CIDR 规范化、显式 /32、重复执行、无关记录保留、零计数 JSON 及持久化失败恢复测试。首次保存失败夹具试图直接在已有 state.json 上建目录而失败，修正为先重命名测试状态文件后重跑通过。最终 `go test -race ./... -skip '^TestAdminSocketAndLock$'`、`make vet`、`make build`、帮助输出及 `git diff --check` 均通过；仍跳过沙箱受限 Unix socket 测试，未跑真实浏览器或 govulncheck。README、帮助和安全审查记录已同步，未修改实际配置或重启服务；用户随后要求提交，本轮代码、测试和文档随 unblock 结果说明提交保存。

### 测试完整性审查（2026-10-24）

用户要求全面检查现有单元/集成测试并判断是否需要补充，本轮仅审查和运行验证，没有授权后的补测实施阶段；业务源码、既有测试与 Makefile 均未修改。开始前 HEAD 为 5232c43，工作区干净。新增 TEST_REVIEW.md 保存需求矩阵、优先级、具体测试方案及验证证据，README 添加入口。后续建议均待实施，不应当作已补齐。

盘点 17 个测试文件、87 个 Test 函数和 1 个 fuzz 入口。完整 `go test -race -coverprofile=tmp/test-review-coverage.out ./...` 在沙箱因 Unix socket setsockopt 权限失败，获准在沙箱外重跑后全通过；本轮没有跳过该测试。覆盖率 CLI 60.7%、gate 87.5%、logging 79.5%，总语句 83.3%。沙箱外 `go test -race -shuffle=on -count=10 ./...` 也全通过。JSONC fuzz 2 worker/30 秒通过，292797 次；最终单独 make vet、git diff --check 通过。fuzz 编译期间同时 vet 曾误扫描 tmp/build 内部包，等待结束重跑通过，建议后续把检查范围限定产品包或隐藏生成目录。

发现 status 脱敏测试覆盖 out 后计算空码摘要、并发轮转测试实际数据不足触发轮转、全局登录和下载上限无独立边界断言。用 tmp 中 Go overlay 注入三种单一回归，确认删除 status 的 Digest 清空后包含 Unix socket 的完整产品包测试仍通过，取消全局下载/登录限制后跳过无关 Unix socket 的产品包测试也通过。实际源码没有这些错误；不能把故意注入的错误当作产品漏洞。overlay 初始普通 .go 扩展造成临时包被扫描，修正为 .gotxt 并限定产品包后重跑才计入结论。

建议先补上述断言、发码/撤销/封禁/解封的落盘与重建、管理员 logout、主会话 Cookie 属性、受保护路径与损坏状态；再补 Unix 协议负面路径、实际二进制启动/SIGHUP/SIGTERM/重新启动及少量浏览器回归。Node 夹具只在 tmp，不能当成仓库自带回归；当前真实 TLS/HTTP2 使用 net.Pipe，无真实 TCP/浏览器或信号端到端执行。未修改运行配置、读取真实凭据或重启实际服务。临时原始输出均忽略，持久结论在 TEST_REVIEW.md。

### 测试补强实施（2026-10-24）

用户随后授权按审查建议实施。业务 Go 源码保持原行为，新增 admin_state_test.go、admin_protocol_test.go、boundaries_test.go、audit_boundaries_test.go、rotation_failure_test.go、cmd/filegate/process_test.go，修改原 status 断言和并发轮转测试。共有 23 个 Go 测试文件、118 个 Test 函数、4 个 fuzz 入口，比审查基线增加 31 个 Test 和 3 个 fuzz。覆盖持久化/部分失败/重建、精确配额、窗口恢复、管理员 logout、Cookie 生命周期、状态/路径保护、请求异常/特殊文件、协议错误/断连/超时、元数据与日志故障恢复等；详细对应表见 TEST_REVIEW.md 的后续实施部分。

Makefile 的 test/vet 范围限定 ./cmd/... ./internal/...，避免误扫 tmp 编译文件。新增 make test-integration，FILEGATE_INTEGRATION=1 显式启用实际二进制、CLI、IPv4、TLS/HTTP2、信号与重启测试；平常 make test 默认跳过两个进程测试。进程套件要求非 root，本轮以实际 UID 1003 执行，两个流程均通过。夹具仅在仓库 tmp，用短 socket 路径避免 sockaddr_un 上限，子进程和协程有有界清理，实际服务未动。

新增 tests/browser.cjs、tests/timezone.cjs 和 tests/README.md；make test-browser 只需 Node 内置模块及本机 Chrome/Chromium（可用 CHROME_BIN），不添加 npm 包或生产依赖。独立 Chrome profile/CDP 管道验证实际 Origin、主/待确认 Cookie、启用和禁用 JS 的手输/跨站链接确认/会话切换/隔离/退出、固定脚本 CSP、时区上报、HTML/SVG 源码和凭据脱敏。Chrome 155.0.8059.39 两轮通过；Firefox 未安装，手工验收步骤已保存但尚未执行，不能按 Chrome 结果宣称 Firefox 已测。失败浏览器夹具保留 tmp/browser-*，其中可能有测试访问码，保持忽略；成功自动清理。

最终验证：完整 make test 含 Unix socket 通过；随机重复 5 次通过，后续最后边界变更后再随机重复 3 次通过。make test-integration、make test-browser、make vet、构建、git diff --check 通过。普通覆盖率 CLI 60.7%、gate 90.8%、logging 81.0%、总计 86.0%（原 83.3%）；实际进程的独立二进制执行不计入普通覆盖率。三个新增 fuzz 各 10 秒通过，次数 Alias 103287、RelativePath 98251、ClientMetadata 78498；默认种子仍全部执行。

重新注入 status 摘要暴露、全局下载上限缺失、全局登录频率缺失三个 overlay 错误，新测试均按预期失败，证明原盲区已补上；没有保留故意错误。普通新测试首次在沙箱拒绝 socket，获准重跑；协议的长 socket 路径夹具已修正。没有运行 Firefox、断电/fsync 故障、跨 UID/移动端/真实媒体验收或 govulncheck；没有增加 CI 工作流或修改部署数据。README、AGENTS、测试审查文档均已同步；用户随后要求提交，首轮审查记录、测试补强及开发入口随本次测试提交保存。

### 默认配置与本地现有配置不同

新生成配置默认：`0.0.0.0:17342`，根目录 `content`，状态目录 `state`，黑名单 `blacklist.txt`，日志目录 `logs`，目录映射包括：

```json
{"ab": "ab", "cd": "cd", "ch": "文件夹"}
```

`init` 会创建三个内容目录以及 state、logs 目录。别名只能是 1–16 个小写英文字母，目录名字可以是中文。所有相对路径以配置文件所在目录为基准。

**2026-10-23 实际检查 `tmp/demo/config.json`：**

```json
{
  "listen": "0.0.0.0:17342",
  "root": "content",
  "state_dir": "state",
  "blacklist": "blacklist.txt",
  "directories": {"ab": "ab", "cd": "cd", "cjk": "中文测试"},
  "code_ttl": "10m",
  "idle_timeout": "30m",
  "max_session": "8h",
  "failure_window": "10m",
  "ban_duration": "15m",
  "max_failures": 5,
  "attempts_per_minute": 10
}
```

旧部署保留 `cjk -> 中文测试`，后来仅把生成模板、帮助和测试示例改成 `ch -> 文件夹`，没有擅自重命名用户实际目录或改写现有配置。使用已有部署时应按实际别名 `code cjk`，访问端口 `17342`；后续仍需重新核对配置。

`tmp/jsonc-demo/config.json` 是本会话生成的、带字段上方中文 `//` 注释的新模板示例。`tmp/` 内还有测试、模糊测试和历史构建资料，均被 Git 忽略。不能依赖这些文件存在于新的克隆中。

## 6. 已实现的行为与边界

### 配置格式

- 配置支持 `//` 与 `/* ... */`，支持标准无注释 JSON。
- **不支持尾随逗号、JSON5、单引号键值、未加引号的键。**
- 注释不能影响字符串里的 `https://`、转义引号等内容。
- 注释替换为空白而不是拼接相邻 token，保留换行和字节偏移。
- 文件上限 1 MiB，包含注释；未知配置字段仍报错。
- 曾实现但未提交的 `_comment` 字段方案已按用户要求撤回，当前没有该字段。
- 状态文件仍是严格的标准 JSON，不支持注释。

### 会话、封禁与持久化

- 安全随机访问码；HMAC 摘要和密钥位于敏感的状态文件，不能公开或写入恢复文档。
- 必须在访问码成功消费并落盘后发放会话；并发提交同一码仅一次成功。
- 会话仅内存保存，进程重启后全部退出。
- 默认空闲 30 分钟、最长 8 小时；OTP/访问码到期不终止已建立会话。
- 空闲以授权请求为准；静静阅读、播放已缓存内容不自动续期。
- Cookie 使用 HttpOnly、SameSite=Strict；HTTPS 下加 Secure。
- 默认 10 分钟内 5 次登录错误，或一分钟超过 10 次登录提交，自动封禁 15 分钟；还有固定的全局每分钟 1000 次登录提交限制。
- 成功登录清除失败次数，但不清除请求频率计数。自动封禁持久化，失败计数仅内存保存。
- 手动黑名单仅支持 IPv4 和 IPv4 CIDR，永久生效，每次请求重读；文件缺失或格式错误（包括 IPv6 条目）时拒绝全部 Web 请求。旧 IPv6 自动封禁记录不再匹配来源，到期后清理。
- 管理命令 `block` 会撤销相关现有会话；手工编辑文件只保证后续匹配来源请求被拒绝，不主动遍历撤销全部会话。
- 单服务进程独占写状态，使用 flock 避免重复实例，写入同步后替换；不支持多实例共享状态。
- 恢复旧状态备份可能重新启用历史访问码；恢复后需处理旧码。

### SIGHUP 热重载

```bash
# 先确认 PID 是目标 serve 进程；多个实例不要随便选一个
ps -C filegate -o pid,args
kill -HUP "$(pidof -s filegate)"
```

- `-HUP` 等同 `-1`。当前版本会重载，不会退出或自重启。
- `SIGINT` / `SIGTERM` 优雅退出；不要把 HUP 当作二进制升级。
- 可热更新目录映射、默认访问码有效期、会话超时、登录失败窗口和封禁/限流参数。
- 监听地址、根目录、状态目录、黑名单路径、TLS 路径及 logging 配置变更要求重启；证书文件内容更新也需重启。
- 先加载并验证整个配置、打开全部目录；失败保留旧配置，记录日志。
- 删除/修改目录映射，或实际目录被替换时，撤销该别名下的会话及未用码；先持久化撤销，再切换。
- 未变化目录保留授权；现有访问码和自动封禁保留原到期时间；新会话超时规则作用于现有会话。
- 文件打开前在同一锁内重新校验会话，避免旧会话跨越重载读到新目录。
- 已打开的文件描述符可完成传输，不等待大文件传完才切换。
- 日志：`configuration reloaded` / `configuration reload failed`。发信号成功不代表加载成功。

### 文件预览与安全

- 点击文件名优先预览，另有“下载”链接；`?download=1` 强制附件下载。
- TXT、LOG、Markdown、JSON/JSONC、配置和源码按纯文本显示，Markdown 不做富文本渲染。
- HTML、SVG、XML、JS 等也按**源码文本**显示，尚未实现 HTML 页面渲染。
- 常见图片、PDF、音视频按浏览器原生能力尝试显示或播放。
- 无扩展名/未知扩展名先检查 512 字节；识别为文本或支持的媒体则预览，否则下载。
- 文本默认 UTF-8，支持有 BOM 的 UTF-16；不提供 GBK 等编码转换。
- 最终预览取决于浏览器支持、编码和用户设置，例如 PDF 被设置为总是下载。
- 预览与下载均检查会话、路径隔离和黑名单，支持 Range 和 HEAD。
- `os.Root` 限制路径与符号链接逃逸；不是完整的 OS 沙箱，不防管理员放入危险挂载或敏感硬链接。
- 列表隐藏符号链接与隐藏文件；授权目录内部的符号链接可能通过已知路径访问。
- 文件响应 CSP 禁止脚本；生成页面仅哈希允许固定时区采集脚本。X-Frame-Options/嵌入策略仍限制 iframe；不要简单放开 HTML MIME 或脚本而忽略隔离。
- 文件已下载后无法撤回；封禁/退出不会立刻中断已开始传输。

### 日志（已实现并提交）

用户要求控制台带高亮、同时写文件，日志默认与 content/state 平级，并需要轮转和保留期限。已实现，仍只用标准库：

```jsonc
"logging": {
  "directory": "logs",
  "level": "info",
  "color": "auto",
  "max_size_mb": 10,
  "max_files": 10,
  "retention_days": 7
}
```

- 配置对象可省略或只填部分字段，其余采用默认值；原有 `tmp/demo/config.json` 无须修改，默认使用 `tmp/demo/logs/`。
- 服务向 stderr 和 `logs/filegate.log` 同时输出文本日志；管理 CLI 的 JSON 结果继续输出到 stdout。
- 控制台自动检查实际 TTY（Linux ioctl），重定向时不着色；auto 尊重 NO_COLOR 和 TERM=dumb；可 always/never。DEBUG 青色、INFO 绿色、WARN 黄色、ERROR 红色；文件不带 ANSI。
- 达到大小阈值或 UTC 日期变化时轮转；单条记录不拆分。归档为带 UTC 时间和随机后缀的 filegate-*.log。
- 保留最多 10 个归档，另有活动文件；按归档 mtime 清理超过 7 天的文件，两个条件同时生效。启动、轮转和每 15 分钟维护时清理，闲置时也能跨日轮转与清理。仅处理自己的命名格式，不删除其他文件。
- 日志目录 0700，文件 0600，独立 flock 防止两个服务共用目录；路径不能位于 content 内，包含通过符号链接进入的情形。
- logging 全部配置字段变更要求重启，SIGHUP 拒绝并保留旧配置。
- 启动打不开日志时拒绝启动；运行时文件写入/轮转失败仍输出 console，最多每分钟提示一次错误；退出同步关闭文件。
- HTTP 记录方法、IP、路径、状态、字节、耗时、目录别名；文件请求记录实际 attachment 与显式 download=1。INFO 记录成功、WARN 记录 4xx、ERROR 记录 5xx。
- 记录管理操作、登录/退出、启动/停止、封禁及重载；status 成功仅 DEBUG。失败管理操作不回显原始参数，避免错误地把访问码写进日志。
- 不记录 URL 查询、正文、Cookie、Authorization、会话标识和访问码。异常 Origin 仅保留协议/主机，移除用户信息、路径和查询；Fetch-Site 只记录有效枚举值。日志包含文件路径与 IP，仍需限制读取。
- 测试使用可控时钟调用维护函数，验证轮转和保留期限，无需等待一天或启动网络服务。

### 已修复的浏览器来源问题

旧响应的 `Referrer-Policy: no-referrer` 导致普通表单 POST 的 `Origin` 变成 `null`，再被同源校验拒绝，显示“无效请求来源”。

已改为 `same-origin`，仍拒绝 null 和跨域来源；来源失败不消费访问码、不增加登录错误计数。日志 `request origin rejected` 记录 IP、Host、Origin、Fetch-Site 和 HTTPS 状态，不记录访问码、Cookie、正文。

升级后需要重新打开/刷新登录页，不能重新提交旧页面的 POST。反向代理改 Host 或终止 TLS 也可能导致来源检查失败。当前不信任 X-Forwarded-For 等代理头，直接放在共享代理后会错误地按代理 IP 限流/封禁。

### HTTPS

默认 HTTP，无需证书；仅适用于可信网络，传输为明文。标准库可用配置中的 `tls_cert` 和 `tls_key` 提供 HTTPS。没有自动申请/续期证书；按 IP 访问需要证书覆盖对应 IP，且浏览器信任签发者。

## 7. 验证记录与复现命令

历史构建环境曾为 Go 1.25.2 linux/amd64；本轮安全加固后最低和实际构建版本均为 Go 1.27.3，旧输出仅为历史快照。默认构建：

```bash
make build
# CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X ..." ...
make vet
make test
```

`make test` 使用竞争检测，需要 C 编译器；产品二进制不依赖 C 运行库。历史上完整套件（包括 Unix socket）曾在允许本机 socket 的环境中通过。后期沙箱禁止 Unix socket，用户要求考虑其他测试方式，因此最新各功能使用以下方式验证：

```bash
make prepare
env TMPDIR="$PWD/tmp/test-tmp" \
    GOTMPDIR="$PWD/tmp/build" \
    GOCACHE="$PWD/tmp/go-cache" \
    go test -race ./... -skip '^TestAdminSocketAndLock$'
```

- 日志开发前的预览功能：上述竞争检测测试、`make vet`、构建通过。
- 日志功能：双输出/颜色/级别/属性分组、大小与跨日轮转、数量/时间保留、并发写入、权限和日志锁、符号链接隔离、文件失败仍输出 console、配置默认值与重载限制、访问日志和凭据不泄漏测试通过；采用同一无 socket 竞争检测命令。已在本机用强制颜色模式验证实际双输出并确认文件无 ANSI；未对真实 TTY 自动检测、长时间后台计时或实际部署进程进行端到端验证。
- JSONC：配置生成/读取、字符串保护、错误语法、未知字段、大小限制、重载与失败保留旧配置测试通过；3 秒模糊测试约 41,565 次执行通过。
- 重载：新增中文目录、删除/改映射、实际目录替换、持久化失败回滚、参数更新和并发读取测试通过；没有在用户实际服务上发信号做验证。
- 预览：文本/媒体/HTML 源码、强制下载、中文和特殊字符文件名、无扩展名识别、Range、HEAD、授权和封禁测试通过；没有真实浏览器渲染/编解码验证。
- 曾尝试请求启动无界面 Chrome 复现来源问题，用户中止并要求其他测试；后续改为 httptest 和规范核对，无需浏览器/网络监听。`tmp/` 可能保留该次未执行的检查脚本，不是项目依赖。
- 所有验证材料、缓存、模糊测试数据使用仓库 `tmp/`。不要因恢复会话而去申请浏览器或网络权限，优先沿用已有无监听测试。

模糊测试重跑方式：

```bash
make prepare
env TMPDIR="$PWD/tmp/test-tmp" \
    GOTMPDIR="$PWD/tmp/build" \
    GOCACHE="$PWD/tmp/go-cache" \
    go test ./internal/gate -run '^$' \
    -fuzz '^FuzzStripJSONComments$' -fuzztime=3s -parallel=2
```

- 已提交的 CLI 调整：操作改为子命令，选项双横线保留；默认值与业务权限不变。已更新解析测试、初始化输出、帮助与使用示例。构建、go vet、无 socket 的竞争检测套件及二进制子命令烟雾检查通过；跳过 TestAdminSocketAndLock，未启动真实服务做管理通信测试。

- 本轮版本最小调整：构建、CLI 竞争检测、make vet 和实际 --version 输出检查通过；未改动鉴权或网络逻辑，未重跑完整服务测试。

## 8. 最近讨论、尚未实施

### HTML 渲染预览

用户询问风险，没有授权实现。建议是独立外层预览页，提供返回、源码、下载；内部沙箱只渲染静态 HTML，限制脚本、表单、弹窗及顶部导航。

当前文件响应 CSP 会阻止脚本，且 iframe 被响应头禁止；支持渲染不能只改 Content-Type，还需要设计预览专用策略和资源响应类型。CSS/JS 当前作为源码返回。完整脚本运行需要更强的来源隔离，HttpOnly 并不能阻止同源脚本携带 Cookie 请求文件。服务器目录授权仍有效，不要误称“同源脚本可以直接绕过目录权限”。

### 文件上传（最近一个技术议题）

**用户只询问改动量，尚未要求实现，也未批准具体权限模型。当前没有上传接口或配置字段。**

估算而非承诺：基础单文件表单约 100–200 行；加权限、限额、重名处理、失败清理及重载协调约 300–500 行业务代码；含配置、文档和测试约 500–900 行。不含断点续传、目录上传、进度条、病毒扫描。

建议第一版范围：

- 登录后只能上传到当前授权目录；支持中文文件名，单次一个文件。
- 默认关闭，按别名启用；不覆盖、不删除、不自动解压。
- 流式 multipart 读取，限制请求体/文件大小、并发、频率和存储占用。
- 使用会话和 CSRF 校验；路径限制和符号链接处理必须覆盖写入。
- 临时文件完整接收后才发布，失败/超时清理；不要在网络上传期间长时间持有全局锁。
- 当前 HTTP 服务 ReadTimeout 是 15 秒，需要针对上传设计适当超时。
- 上传完成前重新校验会话和映射：超时、撤销、重载改映射时不能把文件发布到错误目录。
- 需考虑应用用户的磁盘写权限，更新 README 的只读内容假设。
- 用户上传 HTML/脚本后仍维持现有源码预览策略，或另行实现隔离；不能顺便开放脚本执行。

仅为讨论的配置草案（**当前解析器会拒绝这些未知字段，不要直接加入运行配置**）：

```jsonc
"upload_enabled": false,
"upload_aliases": ["ch"],
"upload_max_bytes": 20971520
```

待用户决定：按目录开启意味着所有拿到该目录访问码的人都能上传。如果只允许部分访问码上传，需要把权限绑定到访问码及会话，并扩展管理命令。不要擅自默认所有登录用户可写。

## 9. 保存与后续维护

用户已要求把同步维护规则写入 `AGENTS.md`，后续开发者和新会话必须执行：

- 恢复开发时先阅读本文，核对 Git 状态和实际配置。
- 功能、配置、CLI/帮助和使用方式变化时，同步 `README.md` 及相关示例。
- 开发约束、构建测试和提交规范变化时，同步 `AGENTS.md`。
- 阶段进展、重要决定、验证范围和待办变化时，同步本文；提交或交付相关变更前完成更新。
- 只更新受影响文档，不要求每次把全部 Markdown 改一遍。
- 区分已实现功能与方案讨论，明确记录未运行或跳过的测试。
- 版本记录使用带日期的已知提交基线，不尝试把文档自身的提交号写进同一提交。

这是开发维护规则，不是后台文件同步程序；由接手开发的人或 agent 按规则维护。

本文不包含实际访问码、HMAC 密钥、会话标识、Cookie 或文件内容。不要读取并复制 `state/state.json` 到文档。

本文是开发上下文快照，不是运行时状态备份；浏览器登录会话仍只在服务进程内存中。新会话接手时优先重新核对 Git、构建和配置，不要重复已完成工作。后续功能提交或设计决定发生变化时，应同步更新本文的 HEAD、验证范围和待办。
