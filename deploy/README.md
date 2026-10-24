# 使用 systemd 部署 Filegate

提供的 [filegate.service](filegate.service) 用于 Linux 系统级 systemd，固定使用以下路径。单元按 systemd 249 的指令集验证；其他版本应先用本机 `systemd-analyze verify` 检查。需要管理员权限执行安装，程序仍以非 root 用户运行。

| 对象 | 路径/所有者 |
| --- | --- |
| 服务用户/组 | `filegate`，禁止交互登录 |
| 二进制 | `/usr/local/bin/filegate`，`root:root`、`0755` |
| 部署目录 | `/srv/filegate`，`root:filegate`、`0750` |
| 配置 | `/srv/filegate/config.json`，`root:filegate`、`0640` |
| 内容 | `/srv/filegate/content/`，管理员维护，服务只读 |
| 状态、管理 socket 和黑名单 | `/srv/filegate/state/`，`filegate:filegate`、`0700` |
| 文件日志 | `/srv/filegate/logs/`，`filegate:filegate`、`0700` |

## 首次安装

先按 [README 的账号说明](../README.md#创建禁止交互登录的用户)创建并核对专用非登录用户。下面从仓库根目录执行，**仅用于全新部署**；如果 `/srv/filegate` 已存在，先核对其内容，不要运行初始化、移动黑名单或批量修改权限。

```bash
make build
sudo install -o root -g root -m 0755 tmp/filegate /usr/local/bin/filegate
sudo install -d -o filegate -g filegate -m 0700 /srv/filegate
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json init
sudo mv /srv/filegate/blacklist.txt /srv/filegate/state/blacklist.txt
sudoedit /srv/filegate/config.json
```

编辑配置，将 `blacklist` 改为 `"state/blacklist.txt"`；保留 `state_dir: "state"`、`logging.directory: "logs"`，并按实际需要设置监听地址、目录映射和 HTTPS。配置仍为 JSONC；字符串键和值都需要双引号。默认端口为 `17342`，按需要在主机防火墙中放行。HTTP 仅用于可信网络。

初始化后固定部署权限：

```bash
sudo chown root:filegate /srv/filegate /srv/filegate/config.json
sudo chmod 0750 /srv/filegate
sudo chmod 0640 /srv/filegate/config.json
sudo chown -R root:filegate /srv/filegate/content
sudo find /srv/filegate/content -type d -exec chmod 0750 {} +
sudo find /srv/filegate/content -type f -exec chmod 0640 {} +
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json check
```

`state/`、`logs/` 和已移动的黑名单保留初始化时的服务用户所有权及私有权限。新增内容由管理员复制，继续让服务用户只读；新增目录先创建，再编辑映射、检查和重载。TLS 私钥放在内容目录外，例如 `/srv/filegate/tls/server.key`，由 root 持有，服务组可读，目录 `0750`、私钥 `0640`。

安装并启用服务：

```bash
sudo install -o root -g root -m 0644 deploy/filegate.service /etc/systemd/system/filegate.service
sudo systemd-analyze verify /etc/systemd/system/filegate.service
sudo systemctl daemon-reload
sudo systemctl enable --now filegate.service
sudo systemctl status filegate.service --no-pager
sudo journalctl -u filegate.service -n 50 --no-pager
```

`Type=exec` 确认程序成功执行，不代表 HTTP 已就绪；检查启动日志和实际访问结果。没有 socket activation，也不使用 PID 文件。连续 60 秒内启动失败 5 次后会触发 systemd 频率限制；修正问题后执行 `sudo systemctl reset-failed filegate.service`，再启动。

`After=network.target` 不等待具体 IPv4 地址就绪。默认绑定 `0.0.0.0` 无须等待；若必须绑定动态获取的特定 IPv4，可在 drop-in 中添加 `Wants=network-online.target` 和 `After=network-online.target`，并核对发行版对应的 wait-online 服务。

## 日常操作

```bash
# 同一服务用户通过已有 Unix socket 管理，无须登录该账号。
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json status
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json code ch --ttl 30m

sudo systemctl reload filegate.service
sudo journalctl -u filegate.service -n 50 --no-pager
sudo systemctl restart filegate.service
sudo systemctl stop filegate.service
sudo journalctl -u filegate.service -f
```

启动前自动运行 `check`，失败则不执行 `serve`。重载先执行 `check`，失败则不发送 SIGHUP；检查通过后向主进程发信号。**`systemctl reload` 成功只表示检查和信号发送成功，不保证业务配置已经切换**。查看 `configuration reloaded` 或 `configuration reload failed` 日志确认结果；部署路径、监听地址、日志设置或证书变化需要 `restart`，详见 [重载规则](../README.md#信号与配置重载)。

正常停止发送 SIGTERM，程序最多等待 10 秒结束 HTTP 请求；单元在 30 秒后强制终止整个服务控制组。管理员停止不会触发 `Restart=on-failure`。重启会清空登录会话；未消费访问码保留，受状态文件和原到期时间控制。

控制台输出进入 journal，应用同时写入 `logs/filegate.log` 并按配置轮转。journal 的保留策略由系统 journald 管理，应用的日志保留配置不控制 journal。默认 `logging.color: "auto"` 在 journal 中不输出 ANSI；不要为部署设置 `always`。

## 权限限制和自定义路径

单元以只读文件系统视图运行，仅开放 `state/` 和 `logs/` 写入；不授予 capabilities，保留 IPv4 与本机 Unix socket。`check` 不探测写权限，若黑名单仍在部署目录，启动检查可能通过，但 `block`/`unblock` 的原子替换会失败。因此必须先迁移黑名单并更新配置，不能仅修改文件自身的写权限。

`ProtectHome=yes` 隐藏 `/home`、`/root` 和 `/run/user`，这些位置的内容或 TLS 文件（包括符号链接目标）不可用。`PrivateDevices=yes` 也限制设备访问。不要将目录挂载到敏感路径来绕过隔离；按需选用 `/srv` 下的可信只读内容。低于 1024 的端口在通常需要绑定权限的系统上不可用，建议保留非特权端口。

若调整路径，使用 `sudo systemctl edit filegate.service` 创建 drop-in，同时更新 `WorkingDirectory`、所有 `ExecStartPre`/`ExecStart`/`ExecReload` 和 `ReadWritePaths`。替换这些列表型指令时先用空赋值清除旧值；重建两个 `ExecReload` 步骤并保留 `$MAINPID`。不要把配置、二进制或内容根目录列为可写。更改后运行 `systemd-analyze verify`、`daemon-reload`，然后重启；`daemon-reload` 只重读 systemd 单元，不重载应用配置。

## 升级和回退

完整操作清单和命令见 [升级、备份与恢复指南](../docs/OPERATIONS.md)，包括校验备份、隔离解包、权限恢复及全封禁期间逐项撤销旧码。以下仅为要点，不能代替完整恢复流程。

1. 核对新版本 [CHANGELOG](../CHANGELOG.md)，在独立目录构建/验证二进制，记录 `--version`，保留旧二进制。不要在正在运行的二进制上直接覆盖写入。
2. 停止服务，确认进程已退出。将配置、`state/`（包括密钥及黑名单）、TLS 文件和必要内容备份到内容根目录以外的私有位置，保留权限。敏感备份不要提交到 Git。
3. 服务停止后安装新二进制；不重新运行 `init`，不删除状态、黑名单或日志。使用服务用户执行 `check`，再启动并检查日志、版本和浏览器访问。检查失败时先修正，或按兼容性说明恢复旧二进制。
4. 仅二进制升级无需 `daemon-reload`；更新单元或 drop-in 后才需要。回退不能默认恢复旧状态：旧备份可能让历史访问码重新有效，且新旧状态格式不保证永远兼容。当前没有“一键撤销全部访问码”，需用 `status`/`revoke ID` 逐项处理；重启已使会话失效。

已有部署迁入本单元时，先按以上流程停服备份，保留原配置和状态，再调整路径、权限与黑名单位置；不要用首次安装命令覆盖实际运行数据。

## 卸载

```bash
sudo systemctl disable --now filegate.service
sudo systemctl reset-failed filegate.service
sudo rm /etc/systemd/system/filegate.service
sudo systemctl daemon-reload
```

以上只移除服务单元，保留二进制、账号和 `/srv/filegate` 数据。若配置过 drop-in，应检查并按需移除 `/etc/systemd/system/filegate.service.d/`；其余文件由管理员核对备份和用途后手工清理。不要自动删除状态、私钥、内容或账号。

## 验证范围

`systemd-analyze verify` 检查单元语法、依赖和命令路径，不验证用户是否存在、目录权限或实际命名空间隔离。安装后的首次验收应在目标机器上确认：以服务用户 `check`；服务启动后发码、浏览中文文件、永久封禁和解封；修改映射并 reload；无效配置 reload 保留旧配置；重启后旧会话失效；日志落盘、正常停止后无残留进程/socket。应用进程测试入口为 `make test-integration`，它不等同于在 systemd 限制内完成上述验收。

指令语义参考官方 [systemd.service](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html) 和 [systemd.exec](https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html)。
