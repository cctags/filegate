# Filegate 会话恢复记录

更新时间：2026-10-23。本文保存开发上下文，供新会话接手；具体实现以源码、Git 历史和当前配置为准。

## 1. 从这里恢复

1. 阅读本文，再读 `AGENTS.md` 和 `README.md`。
2. 运行 `git status --short`、`git log -5 --oneline`，核对本文之后是否还有变更。
3. 运行 `./tmp/filegate --version` 查看本地构建；二进制不存在时先 `make build`。
4. 确认用户下一步意图。**上传功能、HTML 渲染预览都只讨论过，尚未要求实现。**
5. 不要重新初始化已有的 `tmp/demo/config.json`，不要清理用户文件、状态或黑名单。

可直接给新会话的提示：

> 请先阅读当前目录的 SESSION_STATE.md、AGENTS.md 和 README.md，检查 Git 状态后继续。项目是 Go 标准库实现的 Filegate，所有临时文件放仓库 tmp/，提交采用 Linux kernel 风格且不要 Signed-off-by。上传功能与 HTML 渲染预览尚未实施；最近已提交控制台/文件双输出、轮转及保留期限日志；最新 CLI 已把操作改成普通子命令，选项仍用双横线；最新版本显示仅去掉单独的长哈希行，保留 git describe。请根据我接下来的要求推进。

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

用户认为当前发布版本是 v0.1，但仓库当前没有标签。尚未创建发布标签，需管理员选定发布提交后再决定。原逻辑在标签上只显示标签，在标签之后显示标签+提交数量+短哈希，没有标签时退回短哈希，有未提交改动时加 `-dirty`；不增加 `--long` 或其他 Git 参数。上述旧二进制快照里的 `commit:` 仅供历史参考。

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

### 默认配置与本地现有配置不同

新生成配置默认：`0.0.0.0:8080`，根目录 `content`，状态目录 `state`，黑名单 `blacklist.txt`，日志目录 `logs`，目录映射包括：

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
- 手动黑名单支持 IPv4、IPv6 和 CIDR，永久生效，每次请求重读；文件缺失或格式错误时拒绝全部 Web 请求。
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
- 全局 CSP 默认禁止脚本，X-Frame-Options/嵌入策略也限制 iframe；不要简单放开 HTML MIME 或脚本而忽略隔离。
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

构建环境实际为 Go 1.25.2 linux/amd64。默认构建：

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

当前 CSP 会阻止脚本，且 iframe 被响应头禁止；支持渲染不能只改 Content-Type，还需要设计预览专用策略和资源响应类型。CSS/JS 当前作为源码返回。完整脚本运行需要更强的来源隔离，HttpOnly 并不能阻止同源脚本携带 Cookie 请求文件。服务器目录授权仍有效，不要误称“同源脚本可以直接绕过目录权限”。

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
