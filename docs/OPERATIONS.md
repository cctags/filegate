# 升级、备份与恢复指南

适用于含 `check` 命令的当前 Filegate，采用 [systemd 部署布局](../deploy/README.md)：`/usr/local/bin/filegate`、`/srv/filegate/config.json`，专用非登录用户/组 `filegate`，黑名单为 `state/blacklist.txt`。下列命令供管理员在目标主机执行，不是程序自动执行的维护任务，也不适用于直接恢复到历史 `v0.1`。

示例使用 Bash、GNU tar/coreutils 和 sudo；同一操作的变量保留在同一 Bash 会话中。**执行前核对实际配置路径、挂载、符号链接和服务单元；自定义部署不能只替换一个目录名就照抄。** 旧备份、候选程序和恢复暂存目录均须位于所有内容根目录之外。以下流程不会要求打印或手改 `state.json`。

## 1. 数据与恢复边界

| 对象 | 备份和恢复要求 |
| --- | --- |
| 二进制及 `--version` | 保留升级前的程序和版本记录；仅更新系统 Go 不会更新已编译程序 |
| `config.json` | JSONC 原样保存；恢复时重新审核目录映射，不能恢复已撤销的分享范围后直接发码 |
| `state/state.json` | 包含 HMAC 密钥、未用码摘要及已持久化临时封禁，作为一个敏感文件完整保存；不能从摘要找回访问码 |
| `state/blacklist.txt` | 永久封禁单独保存；恢复旧文件会丢失备份后新增条目，应与最新可信记录合并核对 |
| `content/` | 保留中文名、目录结构和实际文件；外部符号链接目标、其他挂载和外置内容须另行纳入一致性备份 |
| TLS 证书和私钥 | 按实际位置另存；恢复后核对有效期、主机名及信任链，`check` 只验证解析/配对 |
| systemd 单元及 drop-in | 保存实际安装文件和合并视图，恢复前审核路径与写入范围 |
| 日志 | 按审计需求另存；恢复服务不依赖旧日志，journal 不在应用文件日志目录内 |

会话、待确认链接和登录失败计数只在内存中，无法备份，重启后丢失；已落盘的临时封禁按原到期时间继续有效。**恢复旧状态可能重新启用已消费/撤销的访问码**，还可能丢失后续封禁。状态没有 schema 版本或通用迁移工具，`check` 通过不等于新旧版本全部行为兼容；先读 [CHANGELOG](../CHANGELOG.md)。

采用停服备份获取一致快照，同时暂停内容同步、配置编辑、证书更新和其他外部写入者。运行中的单文件原子写入不能保证多个文件来自同一时刻，不把直接复制运行目录称作一致备份。文件系统快照或在线备份需单独设计和验证，不在本文示例范围内。

## 2. 停服备份

先核对容量、服务和实际路径。下面假设配置、状态、黑名单、内容都是上述布局中的实际文件/目录；TLS 如启用，位于 `/srv/filegate/tls/`。其他路径先调整清单，不能遗漏。

```bash
set -euo pipefail
sudo systemctl stop filegate.service
test "$(sudo systemctl show filegate.service -p MainPID --value)" = 0
test "$(sudo systemctl show filegate.service -p ActiveState --value)" = inactive
# 同时确认没有手动启动的实例和其他内容/配置写入者。
sudo install -d -o root -g root -m 0700 /var/backups/filegate
filegate_backup=$(sudo mktemp -d /var/backups/filegate/backup-XXXXXXXX)
sudo test -s /srv/filegate/state/state.json
sudo tar -czf "$filegate_backup/deployment.tar.gz" -C /srv/filegate \
  config.json state/state.json state/blacklist.txt content
sudo install -o root -g root -m 0700 /usr/local/bin/filegate "$filegate_backup/filegate"
sudo /usr/local/bin/filegate --version | sudo tee "$filegate_backup/VERSION.txt" > /dev/null
filegate_units=(filegate.service)
if sudo test -d /etc/systemd/system/filegate.service.d; then
  filegate_units+=(filegate.service.d)
fi
sudo tar -czf "$filegate_backup/systemd.tar.gz" -C /etc/systemd/system "${filegate_units[@]}"
sudo systemctl cat filegate.service | sudo tee "$filegate_backup/systemd-effective.txt" > /dev/null
if sudo test -d /srv/filegate/tls; then
  sudo tar -czf "$filegate_backup/tls.tar.gz" -C /srv/filegate tls
fi
sudo bash -c '
  set -euo pipefail
  cd -- "$1"
  sha256sum deployment.tar.gz filegate VERSION.txt systemd.tar.gz systemd-effective.txt > SHA256SUMS
  if test -f tls.tar.gz; then sha256sum tls.tar.gz >> SHA256SUMS; fi
  chmod 0600 ./*
  sha256sum --check SHA256SUMS
' -- "$filegate_backup"
printf 'Backup directory: %s\n' "$filegate_backup"
```

清单明确排除了 `admin.sock`、`server.lock`、日志锁和临时原子写入文件；它们不是恢复材料。缺少已有状态时示例会停止，不静默产生“完整备份”；未首次运行的新部署可单独备份配置/内容并标注“无初始状态”。不要跳过失败的 tar、磁盘空间或校验错误。

如需审计日志，在停服期间单独归档 `logs/`，排除 `.filegate.lock`，并按要求导出 journal；把新增归档加入校验清单。示例 tar 保留普通 Unix 元数据，但 ACL、扩展属性、SELinux 标签及跨主机 UID/GID 需要另行保存/验收，不能假定自动正确。备份目录 `0700`、文件 `0600`；校验和不能替代来源认证。使用既有可信备份工具做加密、异地副本和保留策略，不把含密钥的包上传到 Git/Release 或放入内容目录。

仅做日常备份时，完成校验后启动原服务并验收；若正在升级/恢复，保持停止并继续下一节。至少保留一个已经演练可恢复的升级前备份，并定期验证异地副本，不能只验证归档能列出文件。

## 3. 常规升级与失败回退

先按 [发布指南](RELEASING.md)校验下载包、架构和来源，解包到私有工作目录。用真实路径替换下面的候选程序路径，暂存到二进制所在文件系统，不直接覆盖正在执行的文件：

```bash
set -euo pipefail
filegate_new=$(sudo mktemp /usr/local/bin/.filegate-upgrade.XXXXXXXX)
sudo install -o root -g root -m 0755 /path/to/verified/filegate "$filegate_new"
sudo "$filegate_new" --version
sudo -u filegate "$filegate_new" --config /srv/filegate/config.json check
```

执行第 2 节停服备份，记录 `$filegate_backup`。如果需要调整配置，先保留旧配置，再按新版本说明编辑；不要 `init`、删除状态或“修复”为一个空 JSON。停服后的最终检查通过才替换：

```bash
sudo -u filegate "$filegate_new" --config /srv/filegate/config.json check
sudo mv -T -- "$filegate_new" /usr/local/bin/filegate
# 仅修改了单元或 drop-in 时，先验证单元并 systemctl daemon-reload。
sudo systemctl start filegate.service
sudo systemctl status filegate.service --no-pager
sudo journalctl -u filegate.service -n 50 --no-pager
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json status
```

确认稳定后按第 6 节验收，重新发放需要的访问码。`systemctl start` 成功不代表 HTTP 已就绪，磁盘上 `--version` 也不证明旧进程已被替换。升级的重启会清空会话，未用码仍按最新状态有效；仅发送 SIGHUP 不会更新二进制。

若升级失败：保持或重新停止服务，保留失败日志及当前数据副本。**优先回退二进制并保留最新状态**，避免把升级后的消费/撤销记录回滚。只有明确确认旧程序兼容当前配置和状态时，才用备份中的旧二进制替换并以服务用户执行 `check` 后启动。也可从已校验旧发行包取得相同程序；备份中的程序需先安装到 root 持有、服务可执行的路径，不能放宽整个备份目录权限。

如果旧程序不能读取新状态/配置、没有 `check` 或缺少安全修复，停止回退，不绕过检查。需要恢复数据时按下一节隔离恢复，不把旧 state.json 直接覆盖到正在运行的服务；回退单元还需人工审核 drop-in、验证并 `daemon-reload`。用过新 `serve` 后应视为可能产生状态变化，不能因启动最终失败就假定数据未变。

## 4. 从备份恢复：默认先封禁，再撤销旧码

适用于磁盘损坏、误删或必须恢复旧数据；先停止服务并确认无写入者。保留现有部署和最新可信黑名单/封禁记录以便核对。备份若疑似遭篡改或密钥泄露，不执行其中的二进制，转到第 5 节。

### 校验并隔离解包

将变量设为选定备份，不使用示例占位符运行。SHA256 全部通过后，仅把自己可信的备份解包到新目录，不向运行目录直接覆盖。普通校验和无法证明恶意归档安全。

```bash
set -euo pipefail
# 标准单元已安装时停止并核对；新主机仍需确认没有手动实例。
if sudo test -f /etc/systemd/system/filegate.service; then
  sudo systemctl stop filegate.service
  test "$(sudo systemctl show filegate.service -p MainPID --value)" = 0
  test "$(sudo systemctl show filegate.service -p ActiveState --value)" = inactive
fi
filegate_backup=/var/backups/filegate/backup-实际目录
sudo bash -c 'set -e; cd -- "$1"; sha256sum --check SHA256SUMS' -- "$filegate_backup"
filegate_restore=$(sudo mktemp -d /srv/filegate-restore.XXXXXXXX)
sudo tar -xzf "$filegate_backup/deployment.tar.gz" -C "$filegate_restore" --no-same-owner
if sudo test -f "$filegate_backup/tls.tar.gz"; then
  sudo tar -xzf "$filegate_backup/tls.tar.gz" -C "$filegate_restore" --no-same-owner
fi
sudo test -s "$filegate_restore/state/state.json"
sudo test -f "$filegate_restore/state/blacklist.txt"
```

在本机已有或新建并核对非登录 `filegate` 账号后，重新设置身份权限，不假定备份中 UID/GID 等于目标主机：

```bash
sudo chown root:filegate "$filegate_restore" "$filegate_restore/config.json"
sudo chmod 0750 "$filegate_restore"
sudo chmod 0640 "$filegate_restore/config.json"
sudo chown -R --no-dereference root:filegate "$filegate_restore/content"
sudo find "$filegate_restore/content" -type d -exec chmod 0750 {} +
sudo find "$filegate_restore/content" -type f -exec chmod 0640 {} +
sudo chown -R --no-dereference filegate:filegate "$filegate_restore/state"
sudo chmod 0700 "$filegate_restore/state"
sudo chmod 0600 "$filegate_restore/state/state.json" "$filegate_restore/state/blacklist.txt"
sudo install -d -o filegate -g filegate -m 0700 "$filegate_restore/logs"
printf '0.0.0.0/0\n' | sudo -u filegate tee "$filegate_restore/state/recovery-blacklist.txt" > /dev/null
sudo chmod 0600 "$filegate_restore/state/recovery-blacklist.txt"
if sudo test -d "$filegate_restore/tls"; then
  sudo chown -R --no-dereference root:filegate "$filegate_restore/tls"
  sudo find "$filegate_restore/tls" -type d -exec chmod 0750 {} +
  sudo find "$filegate_restore/tls" -type f -exec chmod 0640 {} +
fi
sudoedit "$filegate_restore/config.json"
```

将暂存配置的 `blacklist` **临时改为 `"state/recovery-blacklist.txt"`**。该文件拒绝所有 IPv4 Web 请求，包括本机；Unix socket 管理不受此黑名单限制。原始永久黑名单保留在 `state/blacklist.txt`，不能覆盖或清空。核对恢复映射、外部路径、内容完整性和后续新增封禁；TLS 如恢复到 `tls/`，另设 root:filegate 所有权、目录 `0750`、私钥 `0640`，确认服务可读。外部绝对路径不得在暂存检查时误指向尚未迁移的旧部署。

使用已校验且兼容的二进制执行暂存检查；先读出错原因，不能因 `check` 允许缺少初始状态而省略上面的已有状态检查：

```bash
sudo -u filegate /usr/local/bin/filegate --config "$filegate_restore/config.json" check
if sudo test -e /srv/filegate; then
  filegate_previous=$(sudo mktemp -d /srv/filegate-previous.XXXXXXXX)
  sudo mv -- /srv/filegate "$filegate_previous/deployment"
fi
sudo mv -- "$filegate_restore" /srv/filegate
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json check
sudo systemctl start filegate.service
```

以上假设兼容二进制及经过审核的标准单元已安装；新主机按部署指南安装它们，勿运行 `init`。如需恢复 `systemd.tar.gz`，先在单独私有目录解包、审核并安装单元/drop-in；`systemd-effective.txt` 是排查记录，不能直接当作可安装单元。当前版本启动后常规 IPv4 Web 请求应为 `403`；若曾在打开访问的状态下启动旧状态，立即停止并重新进入隔离流程，不继续发码。

### 撤销旧码与重新开放

```bash
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json status
# 对 codes 中每个尚存的 id 重复以下操作；使用真实 ID，不使用明文访问码。
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json revoke CODE_ID
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json status
```

确认 `codes` 没有任何条目（可能省略该字段）、`sessions` 为 0，且所有撤销均持久化成功。当前没有 revoke-all 命令，不用手改状态文件代替；任一次失败都保持全封禁并排查。恢复的临时封禁按原期限保留，但备份后新增的封禁须从最新可信记录补入永久黑名单；审核原 `state/blacklist.txt`，不能依赖旧备份已经包含它们。

```bash
sudo systemctl stop filegate.service
sudoedit /srv/filegate/config.json
# blacklist 改回已审核的 "state/blacklist.txt"，其他字段保持已验证配置。
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json check
sudo systemctl start filegate.service
sudo -u filegate /usr/local/bin/filegate --config /srv/filegate/config.json status
```

黑名单路径变化需要重启，不能只 reload。**不要执行 `unblock 0.0.0.0/0` 来解除恢复隔离**，它会同时清除匹配的临时封禁；应切回原黑名单文件。再次确认没有旧码、系统时钟正确和映射符合当前授权，再生成新码并完成验收。恢复后的新状态是新的备份基线，不要再次回填旧状态。隔离文件和旧部署副本先保留在私有位置，完成验收/保留周期后由管理员核对清理。

## 5. 状态丢失、损坏或密钥泄露

若无可信且兼容的完整状态，无法从摘要恢复原码，也不能只补一个 `secret` 字段继续使用。先采取系统或网络层隔离、停止服务并保存证据；服务配置指向上一节的全封禁文件，保留并审核永久黑名单与内容映射。不要尝试通过反复启动、放宽权限或清空所有黑名单来“修复”。

若管理员明确选择放弃全部旧授权，可在停服时将整个原 `state.json` 移到内容根目录之外的 root 私有隔离目录，保持运行 `state/` 和两个黑名单文件不变。下次启动由程序生成新密钥和空码集合；这是**有意重置授权**，不是保留状态恢复，会丢失旧码及状态中的临时封禁。启动前须将仍应生效的临时封禁转为审核后的永久记录；持续全封禁，核对新 `status` 后再按第 4 节切回正常黑名单。不要运行 `init` 或手工构造状态 JSON。

密钥泄露还需查明账号/主机和备份是否可信，必要时在干净主机重建、轮换 TLS 私钥和系统凭据。恢复旧密钥不属于轮换。后续随机码仍可能偶然与过去的短数字码相同，不能把重新生成密钥宣称为对未来所有同字串的永久拒绝。

## 6. 验收和演练

- 核对目标二进制版本、实际新进程、启动日志、文件日志权限及轮转配置；确认旧进程/socket 已退出，只有一个状态写入者。
- 使用测试目录的新码完成一次性登录、中文文件读取和目录隔离；旧码/恢复前会话不可访问。不要在操作记录、工单或日志里粘贴真实码、Cookie、密钥和私钥。
- 用本机管理核对永久/临时封禁、状态可保存及重启后撤销仍生效；如需验证 `block`/`unblock` 写入，只用明确的测试地址，不误删实际封禁。
- 检查配置/黑名单/TLS 变更的重启要求和 HTTPS 信任；在实际 systemd 文件系统限制内验收，不能把服务用户在外部执行 `check` 当成写入权限证明。
- 定期在独立目录、回环 IPv4 和虚构凭据下演练：备份前发码，备份后消费/撤销，恢复后观察旧状态可使旧码重新有效，再验证全封禁、逐项撤销和重启后失效。不要同时运行两个实例共享状态，不在生产部署上演练，也不为演练发送真实用户消息。

记录备份时间、版本、来源、完整性校验、恢复点、授权清理结果及未验证项。当前仓库提供操作指南与发布打包，不提供自动备份、加密、跨文件事务、数据迁移或一键批量撤销功能。
