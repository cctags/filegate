# Filegate

只使用 Go 标准库的私有文件服务。管理员在服务器终端生成一次性访问码，用户通过浏览器输入后，只能访问授权目录。**当前支持 Linux，构建需要 Go 1.25 或更新版本；运行机器不需要安装 Go。**

## 快速开始

```bash
make build
./tmp/filegate -config tmp/demo/config.json init
# 把需要分享的文件放进 tmp/demo/content/ab/ 或 cd/
./tmp/filegate -config tmp/demo/config.json serve
```

浏览器访问 `http://服务器IP:8080`。另开终端，以服务运行用户执行：

```bash
# 使用配置中的默认有效期（初始为 10 分钟）
./tmp/filegate -config tmp/demo/config.json code ab
# 本次有效期覆盖为 30 分钟
./tmp/filegate -config tmp/demo/config.json code ab 30m
```

命令输出 `code`、`id` 和 `expires`。把完整 `code` 发给用户，例如 `ab01234567`；保留 `id` 以便撤销。明文访问码只在创建时返回，不写入日志；`status` 也不能找回它。代码固定为别名加八位安全随机数字，保留前导零，成功登录一次即失效。一个目录可以生成多张独立访问码。

`init` 不覆盖已有配置。所有相对配置路径相对于配置文件目录解析。构建产物、Go 缓存和测试临时文件均位于仓库的 `tmp/`，不进入版本控制。部署时可以把 `tmp/filegate` 复制到其他位置，另行初始化运行目录。

## 本机管理

所有管理命令通过权限为 `0600` 的 Unix socket 与正在运行的服务通信；不提供 Web 管理页面或 HTTP 管理接口。需使用同一系统用户或 root，且服务必须正在运行。

```bash
./tmp/filegate -config tmp/demo/config.json status
./tmp/filegate -config tmp/demo/config.json revoke 访问码ID
./tmp/filegate -config tmp/demo/config.json logout ab
./tmp/filegate -config tmp/demo/config.json block 203.0.113.25
./tmp/filegate -config tmp/demo/config.json block 198.51.100.0/24
./tmp/filegate -config tmp/demo/config.json unblock 203.0.113.25
```

- `revoke` 撤销尚未使用的码；已登录用户请使用 `logout 别名`，撤销该目录所有会话。
- `block` 永久封禁单个 IP 或 CIDR 网段，同时撤销来源 IP 匹配的现有会话。
- `unblock` 删除完全匹配的永久黑名单条目，并清除覆盖范围内的自动封禁。若还有更大的黑名单网段覆盖此 IP，仍然禁止访问。失败计数保留，继续失败可能再次封禁。
- 黑名单也可直接编辑 `blacklist.txt`，一行一个 IP/CIDR，支持 `#` 注释，每次请求重新读取。建议用临时文件加重命名更新，避免读到半写文件。修改不需要重启；文件缺失或格式错误时拒绝全部 Web 请求并记录错误，修复文件即可恢复。
- 手动编辑黑名单阻止后续请求；已开始的传输不会立即中断。直接编辑不会主动清除内存中的全部相关会话，需要彻底撤销时使用命令。
- IPv4、IPv6 均支持；IPv4 映射地址的 CIDR 请使用普通 IPv4 表示法。

## 配置

`init` 生成的配置示例：

```json
{
  "listen": "0.0.0.0:8080",
  "root": "content",
  "state_dir": "state",
  "blacklist": "blacklist.txt",
  "directories": {"ab": "ab", "cd": "cd"},
  "code_ttl": "10m",
  "idle_timeout": "30m",
  "max_session": "8h",
  "failure_window": "10m",
  "ban_duration": "15m",
  "max_failures": 5,
  "attempts_per_minute": 10
}
```

`directories` 的键是用户输入的别名（1–16 个小写英文字母），值是工作根目录内的相对目录。可以设置 `"a": "long-directory-name"`。目录必须提前存在；不要配置互相嵌套、权限重叠的目录。配置变更需重启，黑名单除外。时长使用 `30s`、`10m`、`8h` 等格式，必须为正数。

每个 IP 在 `failure_window` 内达到 `max_failures` 次错误，或一分钟超过 `attempts_per_minute` 次登录提交，自动封禁 `ban_duration`。成功登录重置错误次数，但不重置提交频率。所有 IP 合计还限制每分钟 1000 次登录提交。普通文件请求不会计入登录错误次数。

## 会话、文件与持久化

- 主动退出、达到空闲超时、达到最长会话期限或管理员撤销时，需要重新输入访问码。空闲按授权请求计算，静静阅读或本地播放不会自动续期。验证码到期不影响已建立会话。关闭浏览器不能作为可靠退出依据。
- 每次目录和文件请求都验证会话及黑名单。会话仅在内存中保存，服务重启后全部失效。Cookie 使用 HttpOnly、SameSite=Strict，HTTPS 下增加 Secure。
- 文件通过 `os.Root` 在授权目录内打开，防止路径穿越和符号链接越界。列表隐藏符号链接和隐藏文件；指向授权目录内部的符号链接仍可能通过已知路径访问。管理员应维护可信的只读内容，不放置设备文件、硬链接到敏感文件或特殊挂载。
- 文本、常见图片、PDF、MP3/WAV/OGG 可直接打开；HTML、SVG 和其他格式默认下载，避免执行不可信脚本。支持 Range 请求。页面提供文件列表，实际预览能力取决于浏览器。
- 已开始的传输允许完成，超时/封禁阻止后续请求；退出无法收回已经下载的数据。
- `state/state.json` 保存访问码 HMAC 摘要、摘要密钥和自动封禁。状态目录为 `0700`，文件为 `0600`；文件内容仍然敏感，不能向用户分享。配置、状态、黑名单和 TLS 私钥应放在内容根目录之外。
- 服务独占写状态，使用互斥锁和同步后替换文件；落盘失败不会建立新会话。单实例通过文件锁保证。不要运行多个实例共享状态，不要手改状态文件，使用本地磁盘。恢复旧备份可能重新启用历史访问码，恢复后应清理旧码。
- 登录失败、成功、自动封禁等事件输出到标准错误，建议交给 systemd 等管理。日志不记录明文访问码、Cookie 或会话标识。

## 证书可不可以不要？

可以。默认不配置证书，直接使用 HTTP。证书用于 HTTPS：配合 TLS 加密访问码、Cookie 和文件传输，并让浏览器验证服务器身份。HTTP 明文传输，能监听或篡改网络的人可能取得访问权限，所以仅在可信内网使用。

需要 HTTPS 时，在配置中增加：

```json
"tls_cert": "server.crt",
"tls_key": "server.key"
```

然后访问 `https://服务器IP:8080`。证书需要覆盖访问的 IP/域名并受浏览器信任；自签证书默认会有信任提示。不提供自动证书申请和续期。此版本直接面对客户端，不信任 `X-Forwarded-For` 等代理头；不要直接放在共享反向代理后，否则 IP 限制会作用于代理地址。

## 开发与验证

```bash
make test   # 并发竞争检测及安全/功能测试，需要本机 C 编译器供 race 使用
make vet
make build  # CGO_ENABLED=0，输出 tmp/filegate
```

`make build` 默认优化交付体积：使用 `-trimpath` 去除本机路径，并通过链接参数 `-s -w` 去除符号表和 DWARF 调试信息，保持单文件静态链接及全部功能。生成的程序不适合源码级调试；需要调试时，可在相同构建环境下去掉 `-ldflags="-s -w"` 重新编译。不使用额外压缩工具或第三方依赖。

运行服务不依赖 C 运行库。测试覆盖一次性码并发消费、重启持久化、目录越界、退出/超时、封禁、Range 响应、本机管理通信和失败落盘。网络受限沙箱可能禁止 Unix socket 测试，需在允许本机 socket 的环境执行完整测试。

代码入口在 `cmd/filegate/`，配置、状态、管理通信与 Web 请求逻辑在 `internal/gate/`。没有第三方模块依赖；修改后使用 `gofmt`，重点保持授权和状态写入规则可审查。
