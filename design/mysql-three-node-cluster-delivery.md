# MySQL 三节点集群交付配置

## 目标

使用现有 `mysql-innodb-cluster` Action Package，在三台 openEuler 24.03 SP3 x86_64 服务器上离线安装 MySQL 8.0.36，并通过 MySQL Shell AdminAPI 创建三节点 InnoDB Cluster。

这不是“安装三个单体 MySQL 后人工改配置”的半自动流程，而是一条由平台控制阶段、屏障和角色的集群工作流：节点准备并行执行，全部成功后才允许协调节点创建集群，最后执行统一验收。

## 节点规划

| 角色 | 数量 | 职责 |
| --- | ---: | --- |
| coordinator | 1 | 执行 MySQL Shell AdminAPI、创建集群、运行 Router、输出验收结果 |
| member | 2 | 运行 MySQL Group Replication，接受协调节点加入 |

三台服务器必须使用不同的稳定主机名或 IP，彼此可解析并互通。协调节点只是编排入口，不是数据库主从中的“主库”；InnoDB Cluster 的写主节点由 Group Replication 管理。

## 平台执行阶段

1. `preflight-nodes`：平台并行连接三台服务器，检查系统版本、架构、内存、磁盘、时间同步、端口和离线文件校验。
2. `prepare-nodes`：平台并行上传包，开放 3306/33060/33061，安装离线依赖，创建 `aifar-mysql` nologin 用户，配置 systemd、SELinux 标签和 MySQL 实例。
3. `nodes-ready`：平台屏障。任何节点失败都禁止后续建群。
4. `bootstrap-cluster`：只在 coordinator 执行 AdminAPI，配置实例、创建集群并用 clone recovery 加入另外两个节点。
5. `install-router`：只在 coordinator 部署 Router。生产环境建议将该阶段拆成第二个 Router Action，在第二台服务器再部署一个 Router，并由 VIP、L4 负载均衡或客户端多地址消除入口单点。
6. `verify-cluster`：检查三个成员均为 `ONLINE`、只有一个 `PRIMARY`，并验证 Router 读写端口。

## 端口

- 所有节点：`3306/tcp`、`33060/tcp`、`33061/tcp`
- Router 节点：`6446/tcp`、`6447/tcp`、`6448/tcp`、`6449/tcp`

防火墙动作必须幂等，并只写入 firewalld/ufw 的持久规则；nftables 无管理器时应失败并交给平台管理员处理，不能写入无法追踪的临时规则。

## 服务与权限

- MySQL systemd：`aifar-mysql-innodb.service`
- MySQL 用户：`aifar-mysql`，系统账号、`nologin`、禁止远程登录
- Router systemd：`aifar-mysql-router.service`
- Router 用户：`aifar-mysql-router`，系统账号、`nologin`
- 凭据：由平台生成并保存到凭据中心，脚本只从受限任务环境读取，不写入普通日志

SELinux 不应通过关闭策略解决。动作包应优先使用 `semanage` 持久化文件和端口类型；缺少 `semanage` 时可以使用 `chcon`，但必须在任务日志中明确标记为非持久标签。

## 平台代码优化要求

平台负责通用控制面能力：目标数量和角色校验、目标主机去重、协调节点必须属于目标集合、阶段排序、并行度、屏障、超时、凭据生命周期、日志脱敏、失败状态和重试语义。

动作包负责业务实现：MySQL 配置内容、离线文件安装、Group Replication 参数、Router 参数和业务验收命令。动作脚本不能自行决定集群目标，也不能自行生成或持久化平台凭据。

当前代码已经具备上述主要边界，重点回归项如下：

- `service.go` 在 dry-run 和 run 前统一校验三节点数量、协调节点归属、主机重复和集群名格式。
- `cluster.go` 以节点阶段并行执行，以 barrier 阻断未完成节点，cluster 阶段只在 coordinator 执行。
- 已存在集群重试时复用凭据中心中的原密码，避免重新生成密码导致已有节点无法连接。
- 失败时将集群和凭据标记为失败/无效，不自动删除数据目录，避免误删已有数据。
- 所有脚本必须使用 Unix LF 行尾，并以 `#!/usr/bin/env sh` 和 `set -eu` 开头；上传校验必须拒绝目录穿越路径。

## 页面操作

编排工作台选择 `mysql-innodb-cluster` 的 `install-cluster` Action，选择恰好三台服务器，指定 coordinator，填写集群名，先执行 dry-run，再确认执行。运行记录中应能看到三台节点的并行步骤、`nodes-ready` 屏障、协调节点步骤和最终验收结果。

## 生产限制

当前包适合首次部署或同一集群的幂等修复，不包含强制重建、缩容、滚动升级、备份恢复和数据清理。Router 仅部署一个实例时，数据库虽然具备三节点容错，客户端入口仍然存在单点；生产交付必须增加第二个 Router Action 或接入外部负载均衡。
