# Filegate

只使用 Go 标准库的私有文件服务。管理员在服务器终端生成一次性访问码，用户通过浏览器输入后，只能访问授权目录。**当前支持 Linux、IPv4，构建需要 Go 1.27.3 或更新版本；运行机器不需要安装 Go。**

功能变化、版本范围和升级注意事项见 [CHANGELOG.md](CHANGELOG.md)。

安全维护范围、漏洞报告方式和部署边界见 [SECURITY.md](SECURITY.md)。

2026-10-24 的[安全审查记录](SECURITY_REVIEW.md)保留旧版本问题及本轮修复状态。当前已升级构建要求、修复慢登录与配置校验、限制下载资源，并去除 IPv6 支持。发布时应持续使用受支持 Go 分支的最新安全补丁重新构建；“只使用标准库”不代表无需跟进安全更新。

## 快速开始

```bash
make build
./tmp/filegate --config tmp/demo/config.json init
# 把需要分享的文件放进 tmp/demo/content/ab/、cd/ 或 文件夹/
./tmp/filegate --config tmp/demo/config.json check
./tmp/filegate --config tmp/demo/config.json serve
```

浏览器访问 `http://服务器IP:17342`。另开终端，以服务运行用户执行：

```bash
# 使用配置中的默认有效期（初始为 10 分钟）
./tmp/filegate --config tmp/demo/config.json code ab
# 本次有效期覆盖为 30 分钟
./tmp/filegate --config tmp/demo/config.json code ab --ttl 30m
```

排查问题时运行 `./tmp/filegate --version`，提供 Git 描述版本、工作区修改标记、UTC 构建时间、Go 版本和目标平台，不再单独输出长哈希 `commit:` 行。此命令不需要配置文件或正在运行的服务。`make build` 仍通过 `git describe --tags --always --dirty` 获取版本：恰好位于标签时显示标签（如 `v0.1`），标签之后的提交显示类似 `v0.1-3-ga2e373a`，没有标签时显示短提交号。发布标签需由管理员选定发布提交后创建，程序不固定版本号，也不自动创建标签；可用 `make build VERSION=v0.1.0` 指定发布版本。未提交的源码构建会显示 `-dirty` 和 `modified: true`；无法获取 Git 描述时版本回退为 `dev`，工作区修改标记显示 `unknown`。直接 `go build` 的版本为 `dev`、构建时间为 `unknown`。

命令输出 `code`、`id` 和 `expires`。把完整 `code` 发给用户，例如 `ab01234567`；保留 `id` 以便撤销。明文访问码只在创建时返回，不写入日志；`status` 也不能找回它。代码固定为别名加八位安全随机数字，保留前导零，成功登录一次即失效。一个目录可以生成多张独立访问码；每个别名最多保留 10 个未使用且未过期的码，达到上限后先撤销旧码或等待到期。升级前已经存在的更多访问码不会被自动撤销，管理员应自行清理。

`init` 不覆盖已有配置。所有相对配置路径相对于配置文件目录解析。构建产物、Go 缓存和测试临时文件均位于仓库的 `tmp/`，不进入版本控制。部署时可以把 `tmp/filegate` 复制到其他位置，另行初始化运行目录。

## 一次性访问链接

保留手动输入访问码，也支持管理员发送链接。发码时明确指定浏览器实际可访问的服务器地址：

```bash
./tmp/filegate --config tmp/demo/config.json code ab --ttl 30m \
  --base-url http://192.0.2.10:17342
```

将 `192.0.2.10` 换成服务器实际 IPv4 地址；HTTPS 也可使用指向 IPv4 的域名。`--base-url` 仅接受 http(s) 的协议、主机和可选端口，不接受账号密码、子路径、查询或片段，也不接受 `0.0.0.0` 或 IPv6 字面地址。省略时输出与以前相同；指定时，JSON 额外返回 `url`，例如：

```text
http://192.0.2.10:17342/access?code=ab01234567
```

也可以把已有完整访问码放入上述 URL；该码仍与手动输入共用同一次登录机会，不会生成另一份授权。撤销链接使用发码返回的 `id` 执行 `revoke`。

用户打开链接后，服务设置独立的待确认 Cookie，再以 303 跳转到不带访问码的 `/access` 确认页。页面显示目标目录别名，点击“确认进入”才通过 POST 验证并消费访问码、创建会话。GET/HEAD 不消费码，普通链接预览不会抢先登录；会自动提交表单的工具仍可能触发确认，这不是人机验证机制。已有登录在打开链接时保留，确认页明确提示确认后会替换当前浏览器的登录。

待确认状态只在内存中保存 HMAC 摘要，不保存原始访问码；有效期固定 5 分钟，绑定待确认 Cookie 和来源 IPv4，全局最多 1000 项、每 IP 最多 10 项。Cookie 为 HttpOnly、SameSite=Lax、Path=/access，HTTPS 时 Secure；Lax 用于从外部链接跳转，POST 另有同源和随机 CSRF 检查。需要启用 Cookie。同一浏览器只保留最近打开的待确认链接，多标签页旧确认会失效；IPv4 改变、服务重启、确认失败或页面过期时，请重新打开原链接或使用输入框。待确认到期不延长原访问码的有效期。

确认仍使用原有封禁、失败计数、频率限制、过期、撤销、目录映射及持久化检查；准备链接时不提前判断码是否有效，因此出现确认页不代表码仍可用。语言切换在干净地址上完成；登录和确认操作不依赖 JavaScript。生成页面附带仅用于记录时区的固定脚本，CSP 仅按哈希允许这段脚本，文件预览继续禁止脚本。

链接本身是敏感凭据。应用日志省略查询、Cookie 和表单正文；链接入口使用 `Referrer-Policy: origin`，后续请求只建议携带来源，不携带完整带码 URL，其他页面继续使用 same-origin。清理地址不能保证消除浏览器历史、消息平台、初始代理日志等留痕。HTTP 下的访问码和会话仍是明文，跨不可信网络应使用 HTTPS。

## 界面语言

Web 登录页、目录页和程序自身的错误提示支持简体中文、英文。首次访问根据浏览器的 `Accept-Language` 及其优先级自动选择；中文语言变体统一显示简体中文，英文变体显示英文，无匹配或没有语言设置时使用简体中文。

页面顶部可点击 **简体中文 / English / 跟随浏览器**。手动选择通过独立 Cookie 记住 180 天，登录、退出后继续有效；“跟随浏览器”清除手动偏好。同一浏览器访问该站点的用户共用这项偏好，与访问码和目录权限无关。浏览器禁止 Cookie 时，手动选择只能作用于当前页面，后续请求仍按浏览器语言选择。

也可使用 `/?lang=en`、`/?lang=zh-CN` 或 `/?lang=auto`，目录页面支持相同参数。切换时保留当前目录；登录失败页面切换时返回登录页。语言参数仅在登录页和目录页的 GET 请求生效，不作为登录凭据。

翻译随二进制编入，不需要额外部署语言文件或修改配置。文件名、目录名及文件内容保持原样；命令行帮助和日志保持英文，默认配置注释保持中文。Go 标准库生成的协议级错误（例如不支持的 HTTP 方法、无效 Range）仍使用英文。

## 部署用户与权限

正式部署统一使用专用系统用户 `filegate`，禁用交互登录；开发测试可以使用开发用户。服务不需要 root，默认端口 `17342` 可由普通用户监听。管理员负责安装、配置和更新，服务进程使用受限权限运行。

### 创建禁止交互登录的用户

以下适用于提供 `useradd` 的常见 Linux 发行版。先检查账号是否存在；已有账号应核对用途和权限，不要重复创建或直接修改其他服务使用的账号：

```bash
getent passwd filegate
```

确认不存在后，由有 sudo 权限的管理员执行：

```bash
sudo useradd --system --user-group --no-create-home \
  --home-dir /nonexistent --shell /usr/sbin/nologin filegate
sudo passwd --lock filegate
```

`--system` 创建系统账号，`--user-group` 同时创建同名主组；不创建 home，`/nonexistent` 仅作为账号记录中的 home 路径。`nologin` 拒绝常规交互式 shell 登录，密码锁定禁止密码认证；不要为该账号设置登录密码或添加 SSH 公钥。部分系统的 nologin 位于 `/sbin/nologin`，可用 `command -v nologin` 查找并替换上述路径。

核对账号、组和密码锁定状态：

```bash
getent passwd filegate   # 最后一项应为 nologin 的绝对路径
id filegate
sudo passwd --status filegate
```

禁止交互登录不妨碍管理员通过 `sudo -u filegate` 执行具体程序，也不妨碍 systemd 的 `User=filegate`。账号名不应加入 sudo 管理组。创建账号不会安装二进制或准备运行目录；按下面的权限说明部署后，可以前台运行：

```bash
sudo -u filegate /usr/local/bin/filegate \
  --config /srv/filegate/config.json serve
```

### 运行目录与服务权限

建议二进制安装到 `/usr/local/bin/filegate`，部署目录使用 `/srv/filegate`，配置路径为 `/srv/filegate/config.json`。文件权限按以下方式分配：

| 对象 | 建议权限 |
| --- | --- |
| 二进制 | root 拥有，`filegate` 只读、可执行，不可修改 |
| 部署目录、配置 | root 拥有，组为 `filegate`；目录 `0750`、配置 `0640`，服务只读 |
| `content/` | 管理员维护；服务可读取文件、遍历目录，无须写权限 |
| `state/`、`logs/` | `filegate` 拥有，目录 `0700`，内部私有文件 `0600` |
| 黑名单 | 服务可读取和更新；原子替换也要求其所在目录可写 |
| TLS 私钥（如启用） | 仅管理员和服务用户可读，放在内容目录之外 |

为了让部署目录保持只读，建议在配置中设置 `"blacklist": "state/blacklist.txt"`。`init` 默认仍生成部署目录下的 `blacklist.txt`，不会自动调整部署权限；新部署应在首次启动前移动文件并修改配置，已有部署应停服迁移并保留全部黑名单内容。配置文件所在目录不能允许服务用户写入，否则即使配置文件只读，也可能被替换。

用服务用户执行 `init`，随后由管理员设置上述所有权和权限；不要让 root 初始化出的私有状态和日志文件阻止服务用户访问。已有部署不要重新初始化。管理命令也使用服务用户，例如：

```bash
sudo -u filegate /usr/local/bin/filegate \
  --config /srv/filegate/config.json code ab --ttl 30m
```

长期运行建议使用 systemd；服务单元的关键配置如下（部署示例，并非程序自动安装的服务）：

```ini
[Service]
User=filegate
Group=filegate
WorkingDirectory=/srv/filegate
ExecStart=/usr/local/bin/filegate --config /srv/filegate/config.json serve
Restart=on-failure
UMask=0077
NoNewPrivileges=true
```

`serve` 在参数校验后检查有效用户 ID；若为 `0`，在读取配置、创建文件或监听端口前报错退出，提示切换服务用户。`--help`、`--version`、`init`、`check` 和本地管理命令仍允许 root 执行，但建议以服务用户初始化和检查，避免生成 root 拥有的运行文件或遗漏权限问题。普通用户不强制命名为 `filegate`，不提供 root 豁免选项；程序不会自动创建用户或降权。

## 命令行选项

`./tmp/filegate --help` 显示简短总览，按命令或主题查看详细帮助：

```bash
./tmp/filegate code --help        # 发码参数、默认值与示例
./tmp/filegate serve --help       # 服务启动、信号与重载规则
./tmp/filegate check --help       # 启动或重载前的只读检查
./tmp/filegate --help=config      # 配置默认值和路径规则
./tmp/filegate --help=examples    # 完整教程，包括新增目录、重载和发码
./tmp/filegate --help=limits      # 固定安全限制
./tmp/filegate --help=all | less  # 全部帮助，按需分页或搜索
```

所有帮助均不读取配置或连接服务；也支持 `--help=code`、`--help code` 和 `code ab --help`。命令帮助可省略必填操作参数，错误选项和不合法的组合仍然报错。终端配色为：标题青色加粗、命令绿色、选项名及配置字段蓝色、参数值/占位符黄色、说明白色、示例 `#` 注释亮青色（不使用暗灰或淡化效果）；`--option=value` 的名称和值分别着色。URL 片段和引号内的 `#` 不作为注释。重定向/管道为纯文本，设置 `NO_COLOR`（包括空值）或 `TERM=dumb` 时关闭颜色。帮助颜色不受配置中的 `logging.color` 影响；程序不自动启动分页器。

`--config` 默认使用当前工作目录的 `config.json`；省略 `--ttl` 时采用运行中服务的 `code_ttl`（初始化值为 `10m`），不是固定覆盖为 10 分钟。帮助中的配置默认值指 `init` 创建的新配置，不代表当前已修改的配置。

每次选择一个操作，例如 `serve`、`code ab` 或 `status`；配置使用 `--config PATH`，访问码有效期使用 `--ttl 30m`，生成完整链接可使用 `--base-url http://服务器地址:17342`（仅用于 `code`）。选项可放在子命令及其参数之前、之间或之后，带值选项也支持 `--config=PATH` 和 `--ttl=30m`。重复选项、多个操作、缺失值和超出子命令要求的位置参数均会报错。

操作使用普通子命令，例如 `serve`、`code ch`、`block IP`；选项仍使用 `--config`、`--ttl`，帮助和版本保留 `--help`、`--version`。不再接受旧的 `--serve`、`--code` 等操作写法，也不接受 `-config`、`-version` 或位置形式的有效期 `code ab 30m`，已有脚本需相应更新。

## 配置与部署检查

在启动或重载前，以服务用户执行：

```bash
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json check
# 开发环境使用当前用户：
./tmp/filegate --config tmp/demo/config.json check
```

`check` 校验 JSONC、字段和运行参数、实际目录映射（包括中文目录、重复/嵌套及符号链接）、敏感路径隔离、黑名单、已有状态文件，以及配置的 TLS 证书/私钥能否解析并配对。成功输出 `Configuration check passed` 并返回 0，失败返回非零。缺少配置、内容目录、已映射目录或黑名单会失败；首次启动前缺少状态/日志目录及 `state.json` 可以通过，但不会创建它们。已有状态按启动时相同的结构和密钥规则校验，不会清理过期条目、发码或消费访问码。

检查不启动日志、不创建锁或 socket、不修改文件内容、权限或修改时间，可在停服或运行中执行，也允许 root 执行。读取可能更新文件系统访问时间。请使用服务用户检查读取权限；root 的检查结果不能代表服务用户权限。检查不尝试写入、不测试端口或锁是否可用，也不验证 TLS 证书的信任链、有效期和主机名。结果仅代表检查时刻，不保证稍后启动或重载成功；它不比较正在运行的配置，修改需要重启的字段仍不能通过 SIGHUP 生效。

## 本机管理

所有管理命令通过权限为 `0600` 的 Unix socket 与正在运行的服务通信；不提供 Web 管理页面或 HTTP 管理接口。需使用同一系统用户或 root，且服务必须正在运行。

```bash
./tmp/filegate --config tmp/demo/config.json status
./tmp/filegate --config tmp/demo/config.json revoke 访问码ID
./tmp/filegate --config tmp/demo/config.json logout ab
./tmp/filegate --config tmp/demo/config.json block 203.0.113.25
./tmp/filegate --config tmp/demo/config.json block 198.51.100.0/24
./tmp/filegate --config tmp/demo/config.json unblock 203.0.113.25
```

- `revoke` 撤销尚未使用的码；已登录用户请使用 `logout 别名`，撤销该目录所有会话。
- `block` 永久封禁单个 IP 或 CIDR 网段，同时撤销来源 IP 匹配的现有会话。
- `unblock` 删除完全匹配的永久黑名单条目，并清除覆盖范围内的自动封禁。若还有更大的黑名单网段覆盖此 IP，仍然禁止访问。失败计数保留，继续失败可能再次封禁。
- `unblock` 的 JSON 输出包含 `unblock` 结果：`blacklist_removed` 为删除的永久条目数，`temporary_removed` 为清除的有效临时封禁数，`message` 给出英文说明。没有匹配记录时，两项均为 0，提示 `No matching ban records found.`，重复执行仍成功；不会为未匹配的条目改写黑名单。单个 IP（含 `/32`）还返回 `still_blocked`，若为 true，`covering_blacklist` 列出仍覆盖它的永久条目。网段操作不返回这个布尔值，不能据此宣称整个网段全部解封。结果反映执行当时的状态，之后仍可能重新封禁。
- 黑名单也可直接编辑 `blacklist.txt`，一行一个 IP/CIDR，支持 `#` 注释，每次请求重新读取。建议用临时文件加重命名更新，避免读到半写文件。修改不需要重启；文件缺失或格式错误时拒绝全部 Web 请求并记录错误，修复文件即可恢复。
- 手动编辑黑名单阻止后续请求；已开始的传输不会立即中断。直接编辑不会主动清除内存中的全部相关会话，需要彻底撤销时使用命令。
- 仅支持 IPv4 地址和 CIDR；IPv6、带作用域的地址和 IPv4-mapped IPv6 写法均拒绝。升级前若黑名单含 IPv6 条目，请先手工移除这些条目，否则会按无效黑名单拒绝启动或 Web 请求；程序不会替你改写运行文件。历史 IPv6 自动封禁记录不再匹配任何可接受来源，到期后随状态清理。

## 配置

`init` 生成的配置采用 JSONC 语法，文件名仍为 `config.json`。支持字段上方的 `//` 和 `/* … */` 注释，也能读取无注释的标准 JSON；可用 `--config config.jsonc` 指定其他文件名。不支持尾随逗号或 JSON5 扩展，未知字段仍会报错。

```jsonc
{
  // 仅支持 IPv4 地址和数字端口；不接受主机名或 IPv6。
  "listen": "0.0.0.0:17342",

  // 相对路径以本配置文件所在目录为基准。
  // 状态目录、黑名单和 TLS 私钥必须放在工作根目录之外。
  "root": "content",
  "state_dir": "state",
  "blacklist": "blacklist.txt",

  // 键是访问码前缀：1–16 个小写英文字母。
  // 值是 root 下的相对目录，支持中文；新增映射前先创建目录。
  "directories": {
    "ab": "ab",
    "cd": "cd",
    "ch": "文件夹"
  },

  // 每个别名最多保留 10 个未使用且未过期的访问码。
  // 默认访问码有效期，可由 --ttl 覆盖；成功登录一次即失效。
  // 与登录后的会话期限无关。时长格式支持 30s、10m、8h。
  "code_ttl": "10m",

  // 按最后一次授权请求计算空闲时间。
  // 仅阅读页面或播放已下载的音频，不会自动续期。
  "idle_timeout": "30m",

  // 从登录开始计算的最长会话时间，不因持续访问而延长。
  "max_session": "8h",

  // 同一 IP 在此时间窗口内达到 max_failures 次错误，
  // 或一分钟内超过 attempts_per_minute 次登录提交，会被临时封禁。
  "failure_window": "10m",
  "max_failures": 5,
  "attempts_per_minute": 10,

  // 自动封禁的持续时间；管理员添加的永久黑名单不自动到期。
  "ban_duration": "15m",

  // 日志目录与 content、state 平级，不能放在工作根目录内。
  // 控制台高亮，文件不带颜色；按 UTC 日期或大小轮转。
  // max_files 是归档数量，retention_days 是归档保留天数，两者同时生效。
  // color 支持 auto、always、never；重定向时 auto 不着色。修改 logging 需重启。
  "logging": {
    "directory": "logs",
    "level": "info",
    "color": "auto",
    "max_size_mb": 10,
    "max_files": 10,
    "retention_days": 7
  }

  // 默认使用 HTTP。需要 HTTPS 时，在 logging 对象后加逗号并启用：
  // "tls_cert": "server.crt",
  // "tls_key": "server.key"
}
```

`directories` 的键是用户输入的别名（1–16 个小写英文字母），值是工作根目录内的相对目录，可以包含中文。例如 `"ch": "文件夹"`，生成时使用 `code ch`。`init` 会创建示例中的三个内容目录及 state、logs 目录；后续新增映射需先创建目录。程序拒绝根目录、包含 `..` 分量的路径，以及实际指向同一目录或相互嵌套的映射（包括符号链接别名）；启动和 SIGHUP 都会检查。配置文件的实际位置也必须在内容根目录外，不能通过外部符号链接把内容目录内的配置引用进来。时长使用 `30s`、`10m`、`8h` 等格式，必须为正数。

`listen` 必须是明确的 IPv4 地址和 1–65535 的数字端口，例如 `0.0.0.0:17342` 或 `127.0.0.1:17342`；不接受 `localhost`、省略地址、IPv6 或端口 0。网络监听固定使用 TCP/IPv4。

每个 IP 在 `failure_window` 内达到 `max_failures` 次错误，或一分钟超过 `attempts_per_minute` 次登录提交，自动封禁 `ban_duration`。成功登录重置错误次数，但不重置提交频率。所有 IP 合计还限制每分钟 1000 次登录提交。普通文件请求不会计入登录错误次数。

## 信号与配置重载

修改配置后，对正在运行的服务发送 `SIGHUP`：

```bash
./tmp/filegate --config tmp/demo/config.json check
kill -HUP "$(pidof -s filegate)"
# -1 与 -HUP 等价
```

先确认目标 PID 对应 `serve` 进程；存在多个实例时应明确指定 PID。旧版本程序不支持此信号，请先替换二进制并正常重启一次。

- 可热更新：`directories`、`code_ttl`、`idle_timeout`、`max_session`、`failure_window`、`ban_duration`、`max_failures`、`attempts_per_minute`。
- 需要重启：`listen`、`root`、`state_dir`、`blacklist` 路径、`logging` 全部字段、`tls_cert`、`tls_key`。更新证书文件内容也需要重启。二进制升级需要重新启动进程。
- 重载先验证配置并打开所有目录；JSON 错误、目录不存在、状态落盘失败或包含需要重启的变更时，整次重载失败，继续使用旧配置。检查日志 `configuration reloaded` 或 `configuration reload failed` 确认结果，发送信号本身不代表成功。
- 新增别名立即可用；删除别名、修改映射、或目录实际被替换时，撤销相关会话和未使用访问码，并先持久化访问码撤销。未变化目录的授权保留。已打开的文件可继续传输，后续请求使用新配置。
- 新访问码使用新的默认有效期；现有访问码和临时封禁保留原到期时间。会话超时采用新规则，缩短超时可能导致已有会话过期。失败计数保留。
- PID、监听连接和管理 socket 保持不变。永久黑名单内容仍然每次请求读取，无须信号。`SIGINT`/`SIGTERM` 仍用于优雅退出；`SIGHUP` 不会重启进程或加载新二进制。

## 会话、文件与持久化

- 主动退出、达到空闲超时、达到最长会话期限或管理员撤销时，需要重新输入访问码。空闲按授权请求计算，静静阅读或本地播放不会自动续期。验证码到期不影响已建立会话。关闭浏览器不能作为可靠退出依据。
- 每次目录和文件请求都验证会话及黑名单。会话仅在内存中保存，服务重启后全部失效。Cookie 使用 HttpOnly、SameSite=Strict，HTTPS 下增加 Secure。
- 文件通过 `os.Root` 在授权目录内打开，防止路径穿越和符号链接越界。列表隐藏符号链接和隐藏文件；指向授权目录内部的符号链接仍可能通过已知路径访问。管理员应维护可信的只读内容，不放置设备文件、硬链接到敏感文件或特殊挂载。
- 点击文件名优先预览：TXT、日志、Markdown、JSON/JSONC、配置文件和源码显示为纯文本；常见图片、PDF、音视频（包括 MP3、MP4、WebM 等）交给浏览器显示或播放。HTML、SVG、XML 和脚本显示源码，不执行，也不渲染为网页；Markdown 不转换排版。文本默认按 UTF-8 显示，带 BOM 的 UTF-16 文本也支持。
- 没有扩展名或扩展名未知的文件，会检查前 512 字节，识别为文本或支持的媒体时尝试预览；压缩包、Office 文档等无法直接预览的二进制文件仍下载。目录列表中的“下载”链接强制下载，也可以在文件 URL 后加 `?download=1`。预览与下载使用相同的授权、路径隔离和封禁检查，并继续支持 Range 请求。
- 服务器通过 Content-Type 指定类型，Content-Disposition 的 inline/attachment 建议预览或下载；最终行为仍取决于浏览器支持、媒体编码及用户设置（例如将 PDF 设置为始终下载）。
- 已开始的传输允许完成，超时/封禁阻止后续请求；退出无法收回已经下载的数据。
- `state/state.json` 仍采用不带注释的标准 JSON，保存访问码 HMAC 摘要、摘要密钥和自动封禁。状态目录为 `0700`，文件为 `0600`；文件内容仍然敏感，不能向用户分享。配置、状态、黑名单和 TLS 私钥应放在内容根目录之外。
- 服务独占写状态，使用互斥锁和同步后替换文件；落盘失败不会建立新会话。单实例通过文件锁保证。不要运行多个实例共享状态，不要手改状态文件，使用本地磁盘。恢复旧备份可能重新启用历史访问码，恢复后应清理旧码。
- 日志同时输出到标准错误和独立日志文件，详见下节；日志不记录明文访问码、Cookie 或会话标识。

## 请求与资源限制

以下是编入程序的固定保护上限，不是可加入 config.json 的配置字段：

| 对象 | 上限或行为 |
| --- | --- |
| TCP 连接 | 128，包括 TLS 握手和空闲连接；达到上限暂停接受新连接 |
| 同时处理的 HTTP 请求 | 128，包括 HTTP/2 流；超过返回 429 |
| 文件请求（含预览、下载、HEAD） | 全局 32，同一 IPv4 地址 4；超过返回 429 |
| 响应写入等待 | 每次最多 30 秒，分块写入时刷新截止时间；并非总下载时长限制 |
| Range | 只接受单个范围、单条且不超过 128 字节的请求头；超限返回 400，保留正常音视频拖动 |
| 单个目录列表 | 最多 10000 个原始条目，包含隐藏项；超限返回 503，建议拆分子目录 |
| 每个别名的活跃访问码 | 最多 10 个；全局仍最多 10000 个 |
| 待确认链接 | 5 分钟，全局最多 1000 项、每 IPv4 最多 10 项；不授予文件权限 |

请求头读取超时 5 秒、请求读取超时 15 秒、连接空闲超时 60 秒。慢登录不再持有全局状态锁，读取完成后重新验证封禁、映射、撤销和到期；访问码仍先持久化消费再发会话。文件请求达到并发上限时，目录导航和退出可以继续；同一 NAT 下的用户共享该 IPv4 的文件并发额度。

升级后需重新启动二进制。已有 IPv6、根目录或重叠目录映射的配置需要管理员调整；不会自动改写配置、黑名单或内容。文件已下载或已打开的传输仍遵守原有撤销边界。资源上限能约束进程占用，不能替代网络层的抗流量攻击措施。

## 日志

服务同时向控制台（stderr）和文件输出文本日志。默认目录为配置文件旁的 `logs/`，与默认 `content/`、`state/` 平级；例如 `tmp/demo/logs/filegate.log`。旧配置省略 `logging` 时也采用默认值，无须补字段。相对日志路径仍以配置文件目录为基准；目录不能放在内容根目录内，包括经符号链接进入内容根目录的情况。

- `level` 支持 `debug`、`info`、`warn`、`error`，默认 `info`，同时作用于两个输出。
- `color` 默认 `auto`：仅实际终端着色，重定向或管道中关闭；`NO_COLOR` 环境变量存在或 `TERM=dumb` 时也关闭。可显式设置 `always` / `never`。DEBUG 为青色、INFO 为绿色、WARN 为黄色、ERROR 为红色；文件始终不带 ANSI 颜色控制符。
- 文件名固定为 `filegate.log`；达到 `max_size_mb`（默认 10 MiB）或 UTC 日期变化时归档，归档名含 UTC 时间和随机后缀。保持日志条目完整，单条超大记录可能超过文件大小阈值。
- `max_files` 默认 10，指最多保留 10 个归档，另有一个活动文件；`retention_days` 默认 7，根据归档的最后修改时间清除超期文件。数量与时间限制同时生效，只处理本程序命名的归档，其他文件不删除。
- 启动和轮转时清理，后台每 15 分钟维护一次；没有新请求时也会轮转跨日的非空活动文件并清理旧归档。保留期限不是精确到秒的删除定时器。
- 日志目录权限 `0700`、文件 `0600`；日志含 IP 和文件路径，应限制读取。日志目录有独立写入锁，不能被多个服务实例共用。
- 启动时无法打开日志目录会拒绝启动；运行时文件写入或轮转失败，控制台继续记录，并最多每分钟输出一次文件日志错误。正常退出会同步并关闭文件。不要在运行中手工删除日志目录或用外部程序同时轮转活动文件。
- 修改 `logging` 任意字段需重启；SIGHUP 会拒绝这类配置变更并保留原配置。管理命令仍向 stdout 返回 JSON，不单独打开文件日志；管理操作由服务进程记录。

默认记录启动/停止、登录/退出、封禁、配置重载和管理操作。HTTP 访问日志包括 IP、方法、路径、响应状态、字节数、耗时和可识别的目录别名；文件请求还记录实际是否按附件返回以及是否显式要求下载。成功请求为 INFO，4xx 为 WARN，5xx 为 ERROR；`status` 成功查询仅在 DEBUG 记录。

客户端审计信息自动加入登录成功/失败、来源拒绝、封禁、退出事件和 HTTP 访问记录，无须修改配置：

| 字段 | 含义与来源 |
| --- | --- |
| `request_id` | 服务端生成的单次请求编号；同时通过 `X-Request-ID` 响应头返回，用于关联事件和访问记录，不是登录凭据 |
| `ip`、`ip_source` | 实际 TCP 对端 IPv4，来源固定为 socket；不采用代理转发头，NAT/VPN/代理下可能是中间节点地址 |
| `browser`、`browser_version`、`os` | 按 User-Agent 和已发送的平台 Client Hints 识别；版本只保留有界数字格式，未知客户端记 unknown |
| `device`、`client_kind` | 推测 desktop/mobile/tablet，以及 browser/command_line/bot；均不是设备或真人身份认证 |
| `client_language` | Accept-Language 中可识别且优先级最高的语言标签，与手动选择的界面语言可以不同 |
| `timezone`、`utc_offset_minutes` | 浏览器自报的时区名及 UTC 偏移分钟数；东区为正，例如 Asia/Shanghai 通常为 480 |
| `timezone_source` | form_reported 为本次表单自报；session_reported 为有效会话中最近一次已知值；unavailable 为未提供；unverified_form 为无效或无法验证的表单值 |
| `http_version`、`https` | HTTP 协议版本和是否直接使用 TLS |
| `operation`、`outcome`、`reason` | 区分表单登录、链接确认、退出等操作，以及成功、失败、拒绝和异常原因 |

浏览器、系统、语言和时区属于客户端自报或推测数据（`client_info_source=client_reported`），可以缺失或被伪造，不能作为授权或封禁判断依据。User-Agent 可能被浏览器简化，Windows 10/11 等也不能可靠区分；这里只记录通用系统名称，不猜测精确系统版本。不直接保存原始 User-Agent、Client Hints 或 Accept-Language，以减少任意头内容进入日志的风险。

时区不是浏览器默认发送的 HTTP 信息。页面内的固定本地脚本使用 Intl.DateTimeFormat 和 getTimezoneOffset，在用户提交登录、链接确认或退出表单时填写两个隐藏字段；脚本不自动提交表单。生成页面的 CSP 只允许该脚本的 SHA-256 哈希，不允许任意同源脚本或任意内联脚本；文件响应继续保持原来的严格 CSP 和源码预览策略。

时区仅用于日志；已知值随会话在内存中保留，供后续浏览请求审计使用，不写入状态文件，也不新增 Cookie。脚本被禁用、接口不可用、未完成表单读取或请求在来源/封禁检查阶段被拒绝时，时区可能为 unknown 或已有会话缓存值；元数据缺失/不合法不会阻止正常登录，原有请求大小限制仍生效。服务端验证时区名使用本机时区库，缺少 tzdata 时可能无法验证名称，但可记录合法的数字偏移。日志主时间戳仍是服务端时间。

失败原因包括 invalid_used_or_expired_code、invalid_origin、invalid_or_expired_confirmation、ip_blocked、ip_rate_limit、too_many_invalid_codes、global_login_rate_limit 和 persistence_failure 等。已使用和无效访问码不保留明文历史，因此不强行区分“错误、已用、过期”。

访问日志省略查询字符串、请求正文、Cookie、Authorization 和会话标识；管理日志不保存访问码。来源检查日志只保留 Origin 的协议和主机，丢弃异常用户信息、路径与查询；Fetch-Site 只记录已知枚举值。日志中的文件路径仅说明发生了什么，不代表绕过权限读取内容。浏览器已下载的内容仍无法撤回。

```bash
# 查看运行日志（无需停止服务）
tail -f tmp/demo/logs/filegate.log
# 查看归档
ls -lh tmp/demo/logs/
```

## 证书可不可以不要？

可以。默认不配置证书，直接使用 HTTP。证书用于 HTTPS：配合 TLS 加密访问码、Cookie 和文件传输，并让浏览器验证服务器身份。HTTP 明文传输，能监听或篡改网络的人可能取得访问权限，所以仅在可信内网使用。

需要 HTTPS 时，在配置中增加：

```json
"tls_cert": "server.crt",
"tls_key": "server.key"
```

然后访问 `https://服务器IP:17342`。证书需要覆盖访问的 IP/域名并受浏览器信任；自签证书默认会有信任提示。不提供自动证书申请和续期。此版本直接面对客户端，不信任 `X-Forwarded-For` 等代理头；不要直接放在共享反向代理后，否则 IP 限制会作用于代理地址。

## 开发与验证

### 登录提示“无效请求来源”

此提示来自登录之前的来源检查，与访问码是否正确无关，也不会消耗访问码或增加输错计数。旧版页面的 `Referrer-Policy: no-referrer` 会使普通表单 POST 发送 `Origin: null`，进而被自身的来源检查拒绝。已改为 `same-origin`，保留来源校验并避免向其他来源发送 Referer。更新程序、重启服务后，请重新打开或刷新登录页，再提交表单；不要直接重新提交旧页面的 POST。

若仍失败，查看日志 `request origin rejected` 中的 `host`、`origin`、`https`，检查是否经过代理修改地址或终止 HTTPS。日志不会记录访问码、Cookie 或请求正文。输入框需要填写生成命令返回的完整 `code`，不是用于撤销的 `id`。

### 构建与测试

```bash
make test   # 并发竞争检测及安全/功能测试，需要本机 C 编译器供 race 使用
make vet
make build  # CGO_ENABLED=0，输出 tmp/filegate
make test-integration  # 非 root：实际进程、CLI、IPv4/TLS、信号与重启
make test-browser      # 可选：Node 20+ 和本机 Chrome，无需 npm install
make fuzz FUZZ=FuzzAccessAlias FUZZTIME=10s
```

`make build` 默认优化交付体积：使用 `-trimpath` 去除本机路径，并通过链接参数 `-s -w` 去除符号表和 DWARF 调试信息，保持单文件静态链接及全部功能。构建时同时通过 `-X` 写入版本和构建时间。生成的程序不适合源码级调试；需要调试时，可在相同构建环境下去掉链接参数中的 `-s -w` 重新编译。不使用额外压缩工具或第三方依赖。

运行服务不依赖 C 运行库。测试覆盖一次性码并发消费、重启持久化、目录越界、退出/超时、封禁、Range 响应、本机管理通信和失败落盘。网络受限沙箱可能禁止 Unix socket 测试，需在允许本机 socket 的环境执行完整测试。

测试审查基线、后续补充结果和仍未验证的范围见 [TEST_REVIEW.md](TEST_REVIEW.md)。操作步骤见 [tests/README.md](tests/README.md)：默认测试包含 fuzz 种子，但不启动实际服务进程或浏览器；可选集成测试仅使用仓库 tmp 下的独立数据和回环 IPv4，不操作已有 demo。浏览器回归验证启用/禁用脚本的登录、跨站链接确认、CSP、时区和退出；支持 `CHROME_BIN=/usr/bin/chromium make test-browser`，Firefox 仍需独立验收。Go 检查范围限定产品包，避免扫描 tmp 中正在编译的临时源码。

公开发布、部署配套及后续维护的对比建议见 [项目评估](docs/PROJECT_REVIEW.md)；该文档区分已有能力和待实施建议，许可证已选定为 MIT，CI 和发行物等仍待实施。

Codex 的日常功能开发按改动影响选择相关测试，不要求每次都跑全套；认证、隔离、持久化或并发等共享逻辑变更应扩大回归范围。创建/移动版本标签或发布前，必须针对目标版本运行完整自动化检查：test、vet、build、进程集成、Chrome 浏览器及四个 fuzz 入口各 30 秒。失败或缺少环境时先报告，不能把跳过视为通过；Firefox 等人工验收单独记录。详细执行规则已写入 AGENTS.md 和测试指南。测试后检查进程、监听与浏览器 profile 的清理情况，报告和构建缓存默认保留。

代码入口在 `cmd/filegate/`，配置、状态、管理通信与 Web 请求逻辑在 `internal/gate/`。没有第三方模块依赖；修改后使用 `gofmt`，重点保持授权和状态写入规则可审查。

### 可选漏洞扫描

`make vulncheck` 调用官方 `govulncheck` 检查产品包和标准库调用路径。它是单独安装的开发工具，不加入 go.mod，也不成为二进制的运行依赖。安装示例（需要网络）：

```bash
make prepare
env TMPDIR="$PWD/tmp/test-tmp" GOTMPDIR="$PWD/tmp/build" \
  GOCACHE="$PWD/tmp/go-cache" GOMODCACHE="$PWD/tmp/go-mod-cache" \
  GOBIN="$PWD/tmp/tools" go install golang.org/x/vuln/cmd/govulncheck@latest
PATH="$PWD/tmp/tools:$PATH" make vulncheck
```

扫描需要访问漏洞数据库；结果需结合功能是否启用、调用方式和触发前提分析。发布前先完成测试与扫描，再构建交付文件；安装新 Go 后仍需重编译并重启，旧二进制不会自动获得标准库修复。

## 许可证

本项目采用 [MIT License](LICENSE)，版权署名为 `Filegate contributors`。允许使用、修改、商业使用及分发，须保留版权和许可声明；软件按原样提供，不附带担保。完整条款见 LICENSE。
