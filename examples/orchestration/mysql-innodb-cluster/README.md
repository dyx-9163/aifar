# MySQL 8.0.36 Three-node InnoDB Cluster Action Package

这是一个可直接上传到 AIFAR 编排工作台的三节点 MySQL 集群动作包。它面向 `openEuler 24.03 SP3 x86_64`，使用 MySQL 8.0.36 官方通用二进制制品，包含 MySQL Server、MySQL Shell 和 MySQL Router，不依赖外网。

## 拓扑

- 必须选择 3 台不同服务器。
- 页面指定 1 台 `coordinator`，该节点承担 MySQL Shell 建群和 MySQL Router 运行；另外 2 台为 `member`。
- 三台 MySQL Server 均参与 Group Replication，目标状态为 3 个 `ONLINE` 成员、1 个 `PRIMARY`。
- Router 在协调节点提供读写端口 `6446` 和只读端口 `6447`，X Protocol 对应 `6448/6449`。

## 阶段

1. `preflight-nodes`：并行检查 SSH 执行上下文、openEuler 版本、架构、内存、磁盘、端口、时钟、SELinux 和离线制品 SHA256。
2. `prepare-nodes`：并行开放端口，安装 RPM 依赖，解压 Server/Shell，创建专用系统用户、配置文件和 systemd 服务。
3. `nodes-ready`：平台屏障。三台节点没有全部成功时，不执行后续阶段。
4. `bootstrap-cluster`：仅协调节点执行 MySQL Shell AdminAPI，依次调用实例检查/配置、`createCluster()` 和 `addInstance(..., recoveryMethod: 'clone')`。
5. `install-router`：仅协调节点引导并启动 MySQL Router。
6. `verify-cluster`：确认三节点 `ONLINE`、唯一 `PRIMARY`、Router systemd 状态及 `6446` 查询链路。

## 平台输入与凭据

- 集群名只允许字母、数字、下划线和短横线，最长 63 个字符。
- 平台生成 24 字节随机值，作为 `aifar_cluster_admin` 的密码。
- 密码由 AIFAR 凭据中心加密保存；执行时只写入权限 `0600` 的任务上下文，不拼入 SSH 命令行，也不输出到任务日志。
- 同一集群名的失败任务重试应复用原集群凭据。不要手工删除凭据后直接覆盖已有数据目录。

## 节点配置

- 安装根目录：目标服务器部署目录下的 `mysql-innodb-cluster/`。
- MySQL systemd：`aifar-mysql-innodb.service`。
- MySQL 运行用户：`aifar-mysql`，系统账号、`nologin`、不可远程登录。
- Router systemd：`aifar-mysql-router.service`，仅协调节点。
- Router 运行用户：`aifar-mysql-router`，系统账号、`nologin`。
- MySQL 数据目录：`mysql-innodb-cluster/data/`，重复执行不会主动删除既有数据。
- 低内存配置：`innodb_buffer_pool_size=256M`、`max_connections=100`；每节点可用内存低于 `768MB` 时阻断。

## 端口

| 端口 | 节点 | 用途 |
| --- | --- | --- |
| `3306/tcp` | 全部 | MySQL Classic Protocol |
| `33060/tcp` | 全部 | MySQL X Protocol |
| `33061/tcp` | 全部 | Group Replication 内部通信 |
| `6446/tcp` | 协调节点 | Router 读写入口 |
| `6447/tcp` | 协调节点 | Router 只读入口 |
| `6448/tcp` | 协调节点 | Router X Protocol 读写入口 |
| `6449/tcp` | 协调节点 | Router X Protocol 只读入口 |

防火墙步骤支持运行中的 firewalld 或 ufw。若只启用了裸 nftables，动作会阻断并要求先由平台或管理员建立持久规则，不会写入不可追踪的临时规则。

## SELinux

- 不关闭 SELinux。
- 存在 `semanage` 时，持久登记 MySQL 二进制、配置、数据、日志、运行目录和 `mysqld_port_t` 端口类型。
- 没有 `semanage` 时使用 `chcon` 修复当前文件标签并记录非持久化告警。
- 失败日志会输出 systemd、MySQL error log 和近期 AVC 诊断，但不会输出数据库密码。

## 使用步骤

1. 在“编排工作台”上传生成的 `mysql-innodb-cluster-1.0.0.zip`。
2. 选择 `install-cluster` Action。
3. 选择恰好三台服务器，填写集群名并指定协调节点。
4. 先执行后端 dry-run，检查角色、阶段、作用域和目标映射。
5. 提交任务，在运行记录中观察节点并行阶段、屏障、协调节点阶段和最终验收。
6. 成功后在数据库实例与凭据中心检查三条 MySQL 节点记录及集群管理员凭据。

## 生产限制

- 三节点 InnoDB Cluster 可以容忍一个 MySQL 成员失效，但本包只在协调节点部署一个 Router；Router 本身仍是单点。生产应用侧应部署至少两个 Router，并由 VIP、L4 负载均衡或客户端多地址消除入口单点。
- 本包没有自动删除数据、强制重建集群、缩容、滚动升级和备份恢复 Action。高风险生命周期应作为独立动作包并走单独审批。
- 三台服务器必须网络互通、时间同步，并为 Group Replication 使用稳定且互相可解析的地址。
- 首次建群使用 Clone 恢复加入节点；加入节点上的既有业务数据不应通过本动作包覆盖。

## 目录

```text
deployspec.json
manifest.json
checksums.sha256
README.md
scripts/
  01-check-connection.sh
  02-check-system.sh
  03-open-firewall.sh
  04-install-node.sh
  05-bootstrap-cluster.sh
  06-install-router.sh
  07-verify-cluster.sh
files/
  RPM-GPG-KEY-openEuler
  mysql-aifar-8.0.36-official-bundle.tar
rpms/
  *.rpm
```
