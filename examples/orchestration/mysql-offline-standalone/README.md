# MySQL 8.0.36 Offline Standalone Action Package

本动作包用于验证 AIFAR 多步骤自定义编排闭环。当前版本为 `1.0.3`，面向 `openEuler 24.03 SP3 x86_64`，使用 MySQL 8.0.36 官方通用二进制包完成离线安装。为了保证包可运行且安装时间可控，本示例不做 MySQL 源码编译。

## 执行步骤

1. `check-connection`：确认 SSH 脚本通道、用户权限和远程工作目录可用。
2. `check-system`：检查操作系统、架构、内存、磁盘、端口、SELinux 工具、RPM 工具和包内 SHA256。
3. `open-firewall`：通过运行中的 firewalld 或 ufw 幂等开放 `3306/tcp`；没有受支持的活动防火墙时记录提示。
4. `install-mysql`：导入 openEuler 官方 RPM 公钥并校验签名，安装本地依赖，解压官方 bundle，配置 SELinux 上下文，初始化或恢复数据目录，创建 systemd 服务并验证 MySQL。

## 安装结果

- 安装目录：目标服务器部署目录下的 `mysql-offline-standalone/`。
- systemd 服务：`aifar-orchestration-mysql.service`。
- 监听地址：`0.0.0.0:3306`。
- 管理用户：`aifar_admin`。
- 凭据文件：`<install-root>/secrets/mysql-admin.env`，权限 `0600`，密码不会进入任务日志。
- 数据目录在重复执行时保留；缺少凭据文件的既有数据目录会拒绝自动重置密码。
- 上一次任务已完成初始化但尚未创建管理账号时，重复执行会通过本地 root socket 继续完成账号加固。

## 限制和风险

- 仅支持单台目标服务器和 x86_64 架构。
- 包内 RPM 来自 openEuler 24.03 SP3，不允许在其他发行版上强制安装。
- 可用内存低于 `768MB` 时阻断安装；低于建议值 `2048MB` 时告警并继续。示例配置使用 `256M` InnoDB buffer pool 和 `100` 个最大连接。
- 支持 SELinux Enforcing。存在 `semanage` 时写入持久化 `mysqld_*` 文件上下文；缺失时使用 `chcon` 修复当前文件并给出非持久化告警，不会关闭 SELinux。
- RPM 公钥来自 openEuler 24.03 LTS SP3 官方仓库，指纹为 `8AA1 6BF9 F2CA 5244 010D CA96 3B47 7C60 B675 600B`。
- 防火墙变更发生在 MySQL 安装之前；后续安装失败时不会自动撤销已开放端口。
- 本动作包未提供卸载和数据删除 Action，避免示例包隐式删除数据库数据。
- 正式环境应将生成的凭据接入 AIFAR 凭据中心，并增加备份、升级、回滚和卸载生命周期。

## 从 1.0.2 失败任务恢复

如果旧任务停在 systemd `status=203/EXEC`，先执行 `systemctl stop aifar-orchestration-mysql` 停止旧 unit 的自动重试，再上传并运行 `1.0.3`。新版本会保留已有数据和凭据文件，修复 SELinux/执行权限后，从账号加固和服务验证阶段继续；不要删除 `data/` 或 `secrets/mysql-admin.env`。

## 包目录

```text
deployspec.json
manifest.json
checksums.sha256
README.md
scripts/
  01-check-connection.sh
  02-check-system.sh
  03-open-firewall.sh
  04-install-mysql.sh
files/
  RPM-GPG-KEY-openEuler
  mysql-aifar-8.0.36-official-bundle.tar
rpms/
  *.rpm
```
