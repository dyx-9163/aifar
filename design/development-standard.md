# AIFAR Development Standard

Status: Standard 1.0
Date: 2026-07-09
Audience: product, development, testing, delivery, operations, and future agents
Scope: AIFAR Deployment source, offline resource packages, installer scripts, runtime agent, tests, packaging, and release verification

本文是 AIFAR Deployment 的统一开发标准。它用于需求评审、方案评审、开发自查、Code Review、测试验收、交付质检和后续维护交接。

本文不替代具体设计文档：

- 自定义部署编排细节见 `design/custom-deployment-orchestration-design.md`。
- AIFAR Runtime 单机资源模型见 `design/single-node-k8s-runtime-resource-design.md`。
- 前端视觉与布局规则见 `design/ant-design-system-portable202606.md`。
- 安装脚本覆盖模板见 `config/installers/README.md`。

## 1. 文档目标

1. 让所有参与者使用同一套工程语言描述 AIFAR。
2. 明确后端、前端、安装脚本、离线资源、runtime agent 的职责边界。
3. 把“状态变更必须可追踪、可审计、可回放诊断”变成硬标准。
4. 给质检提供可检查的验收项，而不是只给开发者看实现细节。
5. 支撑内置应用和自定义部署编排长期并行演进。

## 2. 项目定位

AIFAR Deployment 是一个可离线部署的 Linux 运维与应用部署控制面。

核心能力：

- 统一管理服务器、离线资源、凭据、任务、审计、应用实例和运行时状态。
- 通过 Go 后端控制部署流程，通过 Vue 前端配置和展示，通过 SSH/agent/脚本完成目标机操作。
- 以离线资源包为准入基础，面向 Docker、MySQL、Redis、MinIO、Nacos、AIFAR runtime-v2 以及后续自定义应用。
- 运行形态是 Go 单二进制托管 `/api/v2` API 和 `web/dist` 静态资源。

非目标：

- 不做任意 shell 执行平台。
- 不把业务安装逻辑写进 HTTP handler。
- 不把前端做成绕过后端校验的部署引擎。
- 不让 agent 取代全局任务、RBAC、审计、资源准入和实例模型。

## 3. 角色与质检视角

| 角色 | 关注点 | 必须能检查 |
| --- | --- | --- |
| 产品/项目负责人 | 需求是否能离线交付、是否可回滚、是否可观测 | 功能范围、非目标、风险、验收口径 |
| 后端开发 | API、任务、审计、store、worker、应用模块 | 分层是否正确、状态变更是否可追踪 |
| 前端开发 | 页面、表单、i18n、权限、任务展示 | 是否通过 registry/schema 驱动，是否有中英文文案 |
| 安装脚本开发 | 幂等安装、卸载、检查、诊断、超时 | 脚本是否可重复执行、失败是否输出诊断 |
| 测试/质检 | 单元、构建、只读 smoke、受控 E2E | 命令是否通过，报告是否可复核 |
| 交付/运维 | 配置、资源包、备份、升级、回滚 | 生产参数、离线资源、发布包完整性 |

## 4. 总体架构边界

### 4.1 Vue 配置展示面

Vue 负责：

- 登录、导航、权限展示和语言切换。
- 应用商店展示、安装参数表单、目标服务器选择、提交任务。
- 任务中心、日志、实例状态、运行时视图和告警展示。
- 前端输入预校验和用户体验优化。

Vue 不负责：

- 生成任意 shell 并提交执行。
- 绕过后端做资源准入、拓扑准入、凭据解析或实例写入。
- 在 `AppsView.vue` 中硬编码具体应用业务流程。

### 4.2 Go 控制面

Go 后端负责：

- API、鉴权、RBAC、请求解析、错误响应。
- 应用 registry、资源扫描、参数校验、拓扑校验和部署计划。
- worker 任务、target、step、日志、取消、SSE、审计。
- SQLite store、迁移、敏感字段加密、状态持久化。
- SSH 上传和远程命令执行的受控适配。
- 与 aifar-agent 交互，管理 runtime-v2 控制面记录。

Go 后端不负责：

- 在 handler 中写具体应用安装细节。
- 接收前端传入的自由 shell 并直接执行。
- 把敏感字段写入明文日志、审计或 metadata。

### 4.3 Shell 执行面

Shell 脚本负责目标机上的本地动作：

- 解包、安装 RPM/二进制、渲染配置、创建用户、systemd/docker 操作。
- readiness 等待、端口检查、版本检查、失败诊断。
- 幂等安装、卸载、检查、备份、还原、升级、回滚动作。

Shell 脚本不负责：

- 全局拓扑决策。
- RBAC、审计、任务系统。
- 选择任意服务器或读取平台数据库。
- 输出敏感明文。

### 4.4 aifar-agent 节点执行面

aifar-agent 负责：

- 节点本地 runtime-v2 desired state apply。
- Docker pod/service/endpoint 对齐。
- 本地日志、状态、Nacos 代理注册和代理流量。
- 后续可扩展为节点本地 job runner。

aifar-agent 不负责：

- 全局应用商店 catalog。
- 平台 RBAC、用户、审计。
- 多服务器全局调度。
- 跨主机数据库迁移和集群仲裁。

### 4.5 aifar-server 与 aifar-agent 边界

`aifar-server` 是平台控制面和事实源；`aifar-agent` 是目标节点上的本地执行面。二者边界必须保持为“server 做全局决策，agent 做本地 apply/status/log/proxy”。

| 能力 | aifar-server | aifar-agent | 边界判断 |
| --- | --- | --- | --- |
| 用户、登录、JWT、RBAC | 负责 | 不负责 | 只能在 server。 |
| API `/api/v2`、前端静态资源 | 负责 | 不负责 | 只能在 server。 |
| SQLite store、迁移、控制面事实源 | 负责 | 不负责 | 只能在 server。 |
| 应用商店 catalog、资源扫描、准入 | 负责 | 不负责 | 只能在 server。 |
| 任务、target、step、SSE、审计 | 负责 | 可返回本地 job 状态，但不替代平台任务 | server 是任务事实源。 |
| 多服务器拓扑、coordinator、集群 bootstrap 顺序 | 负责 | 不负责 | server 做全局编排。 |
| SSH bootstrap、上传 agent/资源/脚本 | 负责 | 被安装或被调用 | server 负责把受信内容送到节点。 |
| Runtime desired state 生成 | 负责 | 只接收和 apply | server 生成 spec，agent 不自创业务拓扑。 |
| Docker pod/container reconcile | 触发、记录、展示 | 负责本机 apply/reconcile | agent 是本机执行者。 |
| Service proxy、endpoint cache、Nacos proxy 心跳 | 展示和审计 | 负责本机维护 | agent 负责本地实时性。 |
| 自动扩缩容策略 | 负责策略、任务化和审计 | 可执行本地 scale apply | 策略留在 server，执行落到 agent。 |
| 日志与状态 | 聚合、权限控制、持久化快照 | 提供本地 live status/log tail | server 聚合，agent 采集。 |
| 任意 shell | 不允许接收自由 shell | 不允许提供自由 shell API | 只允许受信脚本/job spec。 |

当前边界总体清晰，但需要逐步收敛三点：

1. Runtime live 状态应优先来自 agent `/status`，server 只做聚合、权限控制和持久化快照；server 直接解析 Docker labels 只能作为兼容或 agent 不可用时的 fallback。
2. 自定义部署的本地执行可以逐步下沉到 agent job runner，但 job 必须由 server 生成、签名或校验，且仍写入 server task/audit。
3. agent API 当前适合绑定 `127.0.0.1` 并由 server 通过 SSH 调用；如果未来改成 server 直接远程访问 agent，必须增加 mTLS 或短期 token，不允许裸露内网 HTTP。

不建议调整的部分：

- 不把 RBAC、catalog、任务系统、审计、SQLite 迁到 agent。
- 不让 agent 自行决定安装哪个应用、选哪些服务器、如何组成集群。
- 不让前端绕过 server 直接调用 agent。

## 5. 仓库目录职责

| 路径 | 职责 | 质检要点 |
| --- | --- | --- |
| `backend/cmd/aifar-server` | 服务端入口 | 启动流程清晰，配置、store、worker、API 初始化顺序正确 |
| `backend/cmd/aifar-agent` | 节点 runtime agent | 本地 API/命令幂等，失败诊断明确 |
| `backend/cmd/aifar-smoke` | 自动化验收命令 | 只读 smoke 不改库，mutating E2E 必须显式授权 |
| `backend/internal/httpapi` | API handler 和路由 | handler 保持薄层，状态变更走 worker 和审计 |
| `backend/internal/store` | SQLite 持久化和迁移 | 新表/字段集中迁移，敏感字段加密 |
| `backend/internal/worker` | 任务执行、日志、SSE | 任务状态、target、step 可追踪 |
| `backend/internal/apps/registry` | 应用模块协议 | 不 import 具体应用 |
| `backend/internal/apps/<app>` | 应用生命周期实现 | 实现 validate/plan/install/check/delete 等能力 |
| `backend/internal/installer` | 脚本渲染、上传、执行工具 | 上传重试、安全 quoting、日志脱敏 |
| `backend/internal/adapter` | SSH/Docker 适配 | 测试使用 fake，不直接连真实环境 |
| `web/src/api` | API client | Bearer token 与 `X-AIFAR-Language` 必须保留 |
| `web/src/apps` | 前端应用模块 | 应用能力通过 registry 发现 |
| `web/src/components` | 共享组件 | 表单/任务/状态组件复用 |
| `web/src/views` | 页面编排 | 页面不写后端业务规则 |
| `resources` | 离线资源 | 目录、manifest、SHA256、part 信息可扫描 |
| `config/installers` | 安装脚本覆盖 | 覆盖脚本兼容内置模板变量 |
| `design` | 设计与开发标准 | 标准、设计、视觉规范分层清晰 |

## 6. 开发流程标准

每个需求按以下流程推进：

1. 明确需求范围、非目标、影响模块和验收标准。
2. 阅读 `memory.md`、`AGENTS.md` 和相关源码/设计文档。
3. 判断是否涉及数据库、任务、审计、凭据、资源包、脚本、前端 i18n。
4. 先保持当前模块边界，只有在重复复杂度明确时才新增抽象。
5. 实现时同步补测试、文案、错误码和文档。
6. 运行对应验证命令。
7. 交付时说明变更、验证结果、未覆盖风险。
8. 结束前向 `memory.md` 追加精简问题和结论。

## 7. 后端开发标准

### 7.1 API 与 handler

所有 API 保持 `/api/v2` 前缀。

handler 只做：

- 鉴权和权限检查。
- 请求解析和基础参数校验。
- 调用 service/module 创建任务或读取数据。
- 记录审计。
- 返回统一响应。

handler 不做：

- 具体安装脚本编排。
- 远程命令细节。
- 散落建表或迁移。
- 明文处理敏感信息。

### 7.2 状态变更

以下动作必须返回 task id 或写入审计，优先二者都做：

- 安装、卸载、检查、升级、回滚、备份、还原。
- 服务器探测。
- 资源扫描。
- 容器 start/stop/restart。
- 凭据新增、更新、禁用、删除。
- 用户、设置、维护类变更。

禁止新增同步阻塞式远程变更 API。

### 7.3 错误与 i18n

后端用户可见错误、任务日志和审计动作必须：

- 使用稳定机器码，例如 `apps.mysql.install`、`permission.denied`。
- 提供 zh/en 文案。
- 保持错误响应结构一致。
- 不包含密码、token、私钥、完整连接串。

### 7.4 Store 与迁移

SQLite schema 只允许在 `backend/internal/store` 集中维护。

新增字段/表时必须：

- 在迁移逻辑中前向兼容。
- 给默认值或兼容旧数据。
- 补 store 级测试。
- 明确是否需要加密、索引、唯一约束。

禁止在 handler、service、installer 中临时建表。

### 7.5 Worker 与任务步骤

任务必须具备可质检的步骤计划。

最小要求：

- task action 使用稳定英文机器码。
- 多目标操作写入 `task_targets`。
- 关键阶段写入 `task_steps`。
- stdout/stderr 经脱敏后进入日志。
- 失败时给出可定位原因。
- 取消时尽力终止后续动作并更新状态。

### 7.6 标准安装步骤

所有应用安装都应映射到同一套标准步骤。具体模块可以拆细，但语义必须落在下面的阶段里，便于前端展示、测试验收、失败诊断和后续自定义部署编排复用。

| 顺序 | 标准步骤 | 代码中常见名称 | Go 职责 | 脚本/远端职责 | 是否必须 |
| --- | --- | --- | --- | --- | --- |
| 1 | `load-context` | `load-server`、`resolve-cluster` | 读取服务器、实例、拓扑、凭据、设置 | 无 | 是 |
| 2 | `validate-request` | `validate-*`、`verify-base` | 校验参数、权限、拓扑、目标数量、冲突实例 | 本地前置检查可选 | 是 |
| 3 | `verify-resource` | `verify-resource`、`select-resources` | 匹配离线资源、校验版本和 SHA256 | 确认目标目录/磁盘可用 | 是 |
| 4 | `prepare-target` | `select-data-disk`、`render-*`、`record-desired-instance` | 生成上下文、脚本、desired 记录、磁盘选择 | 创建目录、用户、临时文件 | 视应用而定 |
| 5 | `upload-artifacts` | `upload`、`upload-bundle`、`upload-artifact` | 上传资源包、脚本、上下文 | 接收文件、校验落盘 | 是 |
| 6 | `install-runtime` | `install-engine`、`install-mysql`、`install-redis`、`install-minio`、`install-nacos`、`deploy-runtime` | 调用受控 installer/adapter | 解包、安装、配置、systemd/docker apply | 是 |
| 7 | `configure-topology` | `configure-sentinel`、`configure-distributed`、`configure-bucket-replication`、`install-router` | 传入角色、节点、端点、集群上下文 | 写配置、加入角色、启动附属组件 | 集群/复合应用必需 |
| 8 | `cluster-bootstrap` | `bootstrap-cluster`、`enable-cluster`、`detect-primary` | 选择 coordinator、控制执行顺序和重试 | 执行 bootstrap/join/rebalance/readiness | 集群必需 |
| 9 | `wait-ready` | `check-runtime`、脚本内部 readiness | 设置超时、收集状态、决定失败语义 | 端口、进程、健康接口、版本检查 | 是 |
| 10 | `record-instance` | `record-instance`、`record-installed-instance`、`record-router-instance`、`record-release` | 写入实例、metadata、release、凭据绑定 | 无 | 是 |
| 11 | `post-check` | `update-server`、`update-instance`、`post-check` | 更新 server/app 状态，触发可观测检查 | 输出最终状态和诊断摘要 | 是 |

标准化要求：

- 前端任务展示应优先使用标准步骤语义；模块内部可保留更具体的 step name。
- `PlanInstall` 返回的步骤必须和实际 `Install` 执行的关键阶段一致。
- 单目标和多目标安装都要写 `task_targets`；每个目标至少有 load/verify/install/record/post-check 语义。
- 集群 bootstrap 只能由 Go worker 编排 coordinator 顺序，脚本不自行选择全局拓扑。
- 脚本内部可以包含更细的 readiness 等待，但失败时必须把诊断透出到任务日志。
- 后续自定义部署 spec 应直接声明这些标准阶段，平台再映射到具体脚本。

### 7.7 当前服务步骤映射

当前内置服务可以统一成如下模型：

| 服务 | 当前主要步骤 | 标准化后步骤 |
| --- | --- | --- |
| Docker | `load-server` -> `install-engine` -> `update-server` -> `record-instance` | `load-context` -> `verify-resource` -> `upload-artifacts` -> `install-runtime` -> `post-check` -> `record-instance` |
| MySQL standalone | `load-server` -> `verify-resource` -> `install-mysql` -> `record-instance` | `load-context` -> `verify-resource` -> `upload-artifacts` -> `install-runtime` -> `wait-ready` -> `record-instance` -> `post-check` |
| MySQL InnoDB Cluster | standalone 步骤 + `bootstrap-cluster`，可带 Router | 标准步骤 + `configure-topology` -> `cluster-bootstrap` -> `post-check` |
| MySQL Router | `load-server` -> `resolve-cluster` -> `verify-resource` -> `install-router` -> `record-instance` | `load-context` -> `validate-request` -> `verify-resource` -> `upload-artifacts` -> `configure-topology` -> `wait-ready` -> `record-instance` |
| Redis standalone | `load-server` -> `verify-resource` -> `install-redis` -> `record-instance` | `load-context` -> `verify-resource` -> `upload-artifacts` -> `install-runtime` -> `wait-ready` -> `record-instance` |
| Redis Sentinel | Redis 安装 + `configure-sentinel` | 标准步骤 + `configure-topology` -> `post-check` |
| Redis Cluster | Redis 安装 + `enable-cluster` -> `bootstrap-cluster` | 标准步骤 + `configure-topology` -> `cluster-bootstrap` -> `post-check` |
| MinIO standalone | `load-server` -> `verify-resource` -> `select-data-disk` -> `install-minio` -> `record-instance` | `load-context` -> `verify-resource` -> `prepare-target` -> `upload-artifacts` -> `install-runtime` -> `wait-ready` -> `record-instance` |
| MinIO distributed/replication | MinIO 安装 + `configure-distributed` 或 `configure-bucket-replication` | 标准步骤 + `configure-topology` -> `post-check` |
| Nacos standalone/cluster | `load-server` -> `verify-resource` -> `install-nacos` -> `record-instance` | `load-context` -> `validate-request` -> `verify-resource` -> `upload-artifacts` -> `install-runtime` -> `wait-ready` -> `record-instance` -> `post-check` |
| AIFAR runtime-v2 | `load-server` -> `verify-resource` -> `record-desired-instance` -> `upload-bundle` -> `deploy-runtime` -> `record-installed-instance` | `load-context` -> `verify-resource` -> `prepare-target` -> `upload-artifacts` -> `install-runtime` -> `wait-ready` -> `record-instance` -> `post-check` |

代码优化方向：

- 短期保持现有 step name，不破坏历史任务和测试。
- 中期新增共享 `installflow` 或扩展 `taskrun`，统一标准阶段、日志模板、target/step 记录和失败语义。
- 长期让内置应用和 scripted 自定义应用都输出同一种标准步骤模型，前端只按标准阶段展示进度，详情里再显示模块私有步骤。

## 8. 前端开发标准

### 8.1 API client

前端请求必须统一通过 `web/src/api/client.ts` 或已有封装。

必须保留：

- Bearer token。
- `X-AIFAR-Language`。
- 401 失效处理。
- 统一错误转换。
- WebSocket token 子协议规则。

### 8.2 路由与权限

新增页面必须：

- 在路由中声明权限。
- 在导航中按权限过滤。
- 后端仍做最终 RBAC 校验。
- 无权限时展示明确但不泄露内部细节的提示。

### 8.3 i18n

所有用户可见文本必须提供 zh/en：

- 页面标题、按钮、表单 label、placeholder。
- 校验错误、空状态、确认弹窗。
- 应用模块私有字段和拓扑说明。

不允许新增只在中文或英文存在的 UI 文案。

### 8.4 应用商店

新增应用前端模块必须：

- 位于 `web/src/apps/<app>/index.ts`。
- 导出 `AppFrontendModule`。
- 由 `import.meta.glob` 自动发现。
- 通过 schema/fields/topology 描述安装参数。
- 使用共享安装弹窗或兼容的通用配置入口。

`AppsView.vue` 不允许手写具体应用的部署分支。

### 8.5 视觉与交互

修改 `web/src` 页面、布局、组件或样式前，必须阅读 `design/ant-design-system-portable202606.md`。

质检要点：

- 页面适合运维控制台，信息密度合理。
- 文本不溢出、不重叠。
- 表单校验清楚。
- 任务、危险操作、删除确认有明确反馈。
- 移动和桌面宽度下核心信息可读。

## 9. 应用模块开发标准

### 9.1 后端模块生命周期

新应用后端模块必须实现 registry 生命周期：

1. `Manifest`
2. `PreflightInstall`
3. `PlanInstall`
4. `ValidateInstall`
5. `Install`

按需实现：

- `DeleteModule`
- `CheckModule`
- `ArtifactUpdate`
- `ArtifactBundleUpdate`
- `ServiceScaleOut`
- `ServiceScale`
- `RuntimeReconcile`
- `RuntimeConfig`
- `RuntimeCleanup`
- `RuntimeAgentUninstall`
- `ClusterStart`

### 9.2 模块准入

应用商店部署必须同时满足：

- 前端模块存在。
- 后端模块 `backendReady=true`。
- 必需离线资源存在且版本可选择。
- 参数、目标服务器、拓扑通过后端校验。

任一不满足，不得执行部署。

### 9.3 实例记录

安装成功必须写入 `app_instances`。

metadata 应包含：

- topology。
- version/resource 信息。
- target/server 信息。
- endpoint/port/status。
- cluster/group 标识。
- 安装选项摘要。

metadata 不得包含：

- 密码。
- token。
- 私钥。
- 完整连接串。
- 未脱敏 secret。

### 9.4 推荐应用文件结构

内置应用和后续自定义应用应尽量使用同一套目录角色。文件数量不要求完全一致，但职责必须清楚；复杂应用可以拆更多文件，不能把多个职责混在 handler 或脚本里。

推荐后端结构：

```text
backend/internal/apps/<app>/
  module.go
  options.go
  service.go
  flow.go
  bundle.go
  installer.go
  script.go
  status.go
  check.go
  uninstall.go
  i18n.go
  templates/
    <topology>/
      install.sh
      check.sh
      uninstall.sh
      bootstrap.sh
  *_test.go
```

后端文件职责和安装使用情况：

| 文件/目录 | 职责 | 安装时是否每个应用都应使用 |
| --- | --- | --- |
| `module.go` | 实现 `registry.Module`，暴露 manifest、preflight、plan、validate、install，并按需暴露 check/delete/update/scale 等接口。 | 是，所有可部署应用必需。 |
| `options.go` | 定义安装参数、拓扑参数、默认值、normalize/validate/helper。 | 是，除极简无参数应用外都应有；建议所有应用保留。 |
| `service.go` | 应用生命周期编排，调用 installer、写 target/step、记录实例、绑定凭据、处理业务状态。 | 是，所有应用安装主流程必需。 |
| `flow.go` 或 `steps.go` | 声明标准安装步骤到模块私有步骤的映射，例如 load/verify/upload/install/bootstrap/record/post-check。 | 建议全部使用；当前部分模块还写在 `service.go`，后续应逐步抽出。 |
| `bundle.go` | 选择离线资源、校验版本、校验 SHA256、解析资源包结构。 | 资源驱动应用必需；纯控制面能力可选。 |
| `installer.go` | 远程安装执行器，负责上传资源、上传脚本、执行命令、接收输出。 | SSH/脚本安装应用必需；纯 agent API 或纯本地控制面能力可选。 |
| `script.go` | 渲染内置 shell 模板，封装模板变量和脚本覆盖规则。 | 使用内置脚本的应用必需；完全由资源包 deployspec 提供脚本时可选。 |
| `status.go` | 定义检查结果、运行态解析、metadata/status 辅助结构。 | 支持 check 或运行态展示的应用必需；建议所有服务类应用使用。 |
| `check.go` | 实现实例检测、运行态采集、状态更新。 | 所有长期运行服务都应实现；一次性工具可选。 |
| `uninstall.go` 或 `delete.go` | 远程卸载、验证移除、删除实例记录。 | 所有可安装服务都应实现，便于 E2E 清理和交付回退。 |
| `i18n.go` | 后端任务日志、错误、步骤标题的 zh/en 文案。 | 是，所有用户可见任务都必需。 |
| `templates/` | 内置安装、卸载、检查、bootstrap 脚本模板，按拓扑拆分。 | 脚本安装应用必需；纯 agent API 能力可选。 |
| `*_test.go` | 模块、参数、脚本渲染、installer、service、失败诊断测试。 | 代码变更必需；新增应用必须覆盖核心安装路径。 |

建议新增共享结构：

```text
backend/internal/installflow/
  stage.go
  plan.go
  runner.go
  target.go
```

| 文件 | 职责 | 是否每个应用安装都使用 |
| --- | --- | --- |
| `stage.go` | 定义标准阶段常量：`load-context`、`validate-request`、`verify-resource`、`prepare-target`、`upload-artifacts`、`install-runtime`、`configure-topology`、`cluster-bootstrap`、`wait-ready`、`record-instance`、`post-check`。 | 重构后应全部使用。 |
| `plan.go` | 把标准阶段和模块私有标题转换成 `registry.InstallStepPlan`。 | 重构后应全部使用。 |
| `runner.go` | 统一 StartStep/FinishStep、日志模板、错误脱敏、target 维度执行。 | 重构后应全部使用。 |
| `target.go` | 包装多目标并发、coordinator 阶段、失败聚合和取消语义。 | 多目标和集群应用必需，单目标也可复用。 |

该共享结构是建议重构方向；当前代码中已有 `backend/internal/taskrun` 和 `backend/internal/apps/deleteflow`，可以在它们基础上演进，避免一次性大改。

推荐前端结构：

```text
web/src/apps/<app>/
  index.ts
  catalog.ts
  i18n.ts
  schema.ts
  types.ts
  components/
```

前端文件职责和安装使用情况：

| 文件/目录 | 职责 | 安装时是否每个应用都应使用 |
| --- | --- | --- |
| `index.ts` | 导出 `AppFrontendModule`，接入自动发现，声明 install dialog props 和提交 payload。 | 是，所有前端应用必需。 |
| `catalog.ts` | 定义图标、分类、能力标签、前端 manifest 摘要。 | 是，所有应用卡片和 catalog 配对必需。 |
| `i18n.ts` | 定义应用私有字段、拓扑、校验、帮助文本的 zh/en。 | 是，所有用户可见应用模块必需。 |
| `schema.ts` | 推荐放动态字段 schema、拓扑字段联动、默认值生成。 | 复杂应用必需；简单应用可暂时放在 `index.ts`/`i18n.ts`。 |
| `types.ts` | 推荐放应用私有参数和 metadata 类型。 | 复杂应用必需；简单应用可选。 |
| `components/` | 应用私有面板、状态块、复杂配置组件。 | 只有共享安装弹窗无法表达时使用；默认不需要。 |

### 9.5 推荐离线资源包结构

内置应用可以继续使用 Go 内置模板；自定义部署应用必须把部署声明和脚本放进资源包。推荐资源包结构如下：

```text
resources/<app>/<version>/
  manifest.json
  deployspec.yaml
  files/
  rpms/
  scripts/
    install.sh
    check.sh
    uninstall.sh
    bootstrap.sh
    backup.sh
    restore.sh
    update.sh
    rollback.sh
  templates/
  checksums.sha256
  README.md
```

资源包文件职责和安装使用情况：

| 文件/目录 | 职责 | 安装时是否每个应用都使用 |
| --- | --- | --- |
| `manifest.json` | 描述资源文件、part、SHA256、资源类型、版本元数据，供资源扫描和准入使用。 | 标准上所有资源包都应使用；历史包缺失时只能降级扫描。 |
| `deployspec.yaml` | 自定义部署编排声明：参数 schema、拓扑、生命周期脚本、步骤、metadata 映射。 | 自定义/scripted 应用必需；内置 Go 应用可选。 |
| `files/` | 存放 tar、zip、二进制、镜像归档等安装资产。 | 按应用需要使用。 |
| `rpms/` | 存放离线 RPM 依赖。 | 需要 RPM 离线安装的应用使用。 |
| `scripts/install.sh` | 节点本地安装脚本。 | 自定义/scripted 应用必需。 |
| `scripts/check.sh` | 节点本地检查脚本。 | 长期运行服务必需。 |
| `scripts/uninstall.sh` | 节点本地卸载脚本。 | 可安装服务必需。 |
| `scripts/bootstrap.sh` | 集群 coordinator 阶段脚本。 | 集群拓扑必需，standalone 可选。 |
| `scripts/backup.sh` | 备份脚本。 | 有数据状态的服务应提供。 |
| `scripts/restore.sh` | 还原脚本。 | 提供备份能力时必须配套。 |
| `scripts/update.sh` | 升级脚本。 | 支持在线/离线升级时使用。 |
| `scripts/rollback.sh` | 回滚脚本。 | 支持升级时应提供。 |
| `templates/` | 应用配置模板，由 Go 渲染上下文后传给脚本。 | 有配置文件的应用使用。 |
| `checksums.sha256` | 资源包完整性校验。 | 建议所有资源包使用。 |
| `README.md` | 说明资源包内容、拓扑限制、脚本前置条件、人工验证方法。 | 运行时不使用，但交付和质检应使用。 |

每个应用安装并不需要“所有文件”都存在；必须存在的是它所声明生命周期需要的文件。标准判断方式是：`module.go`/`deployspec.yaml` 声明了什么能力，资源包和后端模块就必须提供对应实现和测试。

当前管理端动作包上传采用完整设计的先行格式：ZIP 根目录必须包含 `deployspec.json`，其 `apiVersion`/`kind` 为 `aifar.io/v1alpha1`/`ActionPackage`。`actions[].steps[]` 至少声明稳定步骤名、包内脚本相对路径、只读属性和超时；服务端保存逐步 SHA256，并按声明顺序生成 task step。当前执行器只准入 `check` 和 `install`，具体 schema 与限制见 `design/custom-deployment-orchestration-design.md` 的“当前可执行 ActionPackage 子集”。

## 10. 自定义部署编排标准

自定义部署编排采用“资源包描述 + Go 控制面 + 受信脚本 + Vue 动态表单”的标准。

详细设计见 `design/custom-deployment-orchestration-design.md`，本节给出准入硬规则。

### 10.1 资源包

每个自定义应用资源包必须包含部署描述文件，描述：

- app id、名称、版本。
- 支持拓扑。
- 参数 schema。
- 所需文件和 SHA256。
- lifecycle 脚本路径。
- 步骤计划。
- 健康检查方式。
- 实例 metadata 映射。

资源包必须可离线扫描，不依赖公网 registry 或下载。

### 10.2 脚本生命周期

支持的标准 lifecycle：

- `install`
- `check`
- `delete`
- `backup`
- `restore`
- `update`
- `rollback`

脚本必须满足：

- 可重复执行。
- 参数来自 Go 渲染的上下文文件或环境变量。
- 输出可脱敏日志。
- 失败时返回非零退出码。
- 失败时输出诊断摘要。
- 不主动读取平台数据库。

### 10.3 Go 编排

Go 必须负责：

- 解析 deploy spec。
- 校验资源、拓扑、参数、凭据。
- 生成任务计划。
- 控制多服务器执行顺序和并发。
- 上传资源和上下文。
- 调用脚本并收集日志。
- 写入 task、step、target、audit、app_instances。
- 绑定或生成凭据记录。

多服务器集群必须区分：

- per-node 阶段。
- coordinator 阶段。
- cluster bootstrap 阶段。
- post-check 阶段。
- cleanup/rollback 阶段。

### 10.4 Vue 动态配置

Vue 对自定义应用必须：

- 根据后端 catalog/schema 渲染表单。
- 支持单机、多目标、角色分组、拓扑影响字段。
- 只提交结构化参数和 server id。
- 不提交自由 shell。
- 不自行判断最终准入。

### 10.5 备份、升级与回滚

自定义部署编排必须把备份、升级、回滚视为一等 lifecycle。

最低要求：

- 执行前生成 task。
- 执行前记录目标版本和当前版本摘要。
- 升级前能触发备份或声明无需备份的理由。
- 回滚脚本必须知道回滚到哪个 release/backup。
- 成功后更新 `app_instances.metadata` 中的 release 摘要。
- 失败后保留足够日志用于恢复判断。

第一阶段可把备份列表和 release 摘要写入 `app_instances.metadata`；当需要长期查询、分页、审计独立备份对象时，再新增专用表。

## 11. 离线资源与安装脚本标准

### 11.1 离线资源

资源扫描必须支持：

- `resources/<app>/<version>/`。
- manifest 中的 file、part、SHA256。
- RPM 数量统计。
- 拆分 frontend/backend 命名。

新增资源包必须提供：

- 版本号。
- 文件清单。
- 校验摘要。
- 必需/可选资源说明。
- 离线安装脚本或内置模板。

### 11.2 安装脚本

脚本模板必须：

- 使用 POSIX shell 或明确声明所需 shell。
- 对路径和参数做安全 quoting。
- 对 systemd/docker/mysql/redis/minio/nacos 命令失败做诊断。
- 避免输出 secret。
- 对已有安装做幂等处理。
- 对卸载保留或删除数据目录有明确参数。

### 11.3 覆盖模板

`config/installers` 仅用于生产包在不重编译时覆盖内置模板。

覆盖模板必须：

- 与同路径内置模板变量兼容。
- 保留日志脱敏约定。
- 保留退出码语义。
- 经过测试环境验证后再进入生产包。

## 12. 凭据与安全标准

敏感数据包括：

- SSH 密码、私钥。
- 数据库密码。
- Redis 密码。
- MinIO access key/secret key。
- Nacos 用户密码。
- JWT secret、credential secret。
- 任何 token、session、完整连接串。

标准要求：

- 存储前加密或哈希。
- 日志和审计脱敏。
- API 响应不返回 secret 明文。
- 安装成功后手动输入的应用密码应自动生成或绑定凭据记录。
- 删除实例时需要必要的确认和权限。
- 不在文档、memory、测试报告中记录真实 secret。

## 13. 测试与验收标准

### 13.1 本地质量门禁

| 变更类型 | 必跑命令 | 补充要求 |
| --- | --- | --- |
| 后端 service/store/worker/API | `pnpm test` | 涉及 store 需补迁移测试 |
| 前端页面/组件/i18n | `pnpm web:build` | UI 变更需检查中英文和权限态 |
| 前后端联动 | `pnpm build` | 确认 API 类型和打包产物 |
| 发布包 | `pnpm test:local` 或 `pnpm package` + `pnpm release:verify` | 校验 checksums |
| 服务器只读验收 | `pnpm test:server:readonly` | 不得修改目标机和数据库 |
| 真实部署 E2E | `pnpm test:e2e:mutating` | 必须显式授权和服务器白名单 |

### 13.2 测试设计要求

后端测试：

- 使用 fake prober、fake remote、httptest。
- 不连接真实 SSH/Docker。
- 覆盖成功、失败、取消、权限拒绝。
- 覆盖敏感信息脱敏。

前端测试/构建：

- 至少通过 TypeScript/Vite build。
- 新字段具备 zh/en。
- 权限禁用和空状态可显示。

脚本测试：

- 可在干净目标机安装。
- 可重复安装。
- 可检查状态。
- 可卸载。
- 失败日志含诊断。

### 13.3 质检验收单

每个功能交付至少回答：

- 是否影响数据库 schema？
- 是否新增 API？权限是什么？
- 是否新增状态变更？task/action/audit 是什么？
- 是否新增用户可见文案？zh/en 是否完整？
- 是否处理 secret？
- 是否需要离线资源？
- 是否支持失败诊断？
- 是否支持检查、卸载、重试？
- 是否有本地测试和构建记录？
- 是否需要只读 smoke 或 mutating E2E？

## 14. 发布与交付标准

发布包必须只包含运行所需资产：

- `bin/`
- `web/dist/`
- `resources/`
- `config/`
- 启动脚本。
- `VERSION`
- `checksums.txt`

发布包不得包含：

- 源码。
- `node_modules/`。
- `data/`。
- 日志。
- 缓存。
- 开发临时文件。
- 真实 secret。

交付前必须确认：

- `AIFAR_JWT_SECRET` 和 `AIFAR_CREDENTIAL_SECRET` 已替换为稳定高强度值。
- bootstrap/default password 已按环境修改。
- 数据库路径和备份路径不是临时目录。
- 离线资源目录完整。
- checksum 可验证。

## 15. 文档标准

所有 Markdown 应明确：

- 文档用途。
- 目标读者。
- 当前状态。
- 与其他文档的关系。
- 可质检的检查项或验收口径。

文档内容原则：

- README 只做入口和快速路径。
- `design/development-standard.md` 定标准。
- 具体设计文档写方案细节和取舍。
- 模板 README 写覆盖、变量、脚本职责和质检清单。
- `memory.md` 只写精简问题和结论。

## 16. 变更 Checklist

开发自查：

- [ ] 已读 `memory.md` 和相关设计文档。
- [ ] 没有破坏现有模块边界。
- [ ] 状态变更走 worker/task/audit。
- [ ] 后端 handler 保持薄层。
- [ ] Store 迁移集中维护。
- [ ] 敏感信息已加密或脱敏。
- [ ] 用户可见文案有 zh/en。
- [ ] 前端权限和后端 RBAC 一致。
- [ ] 应用商店能力来自 registry/module。
- [ ] 自定义脚本来自受信资源包或内置模板。
- [ ] 新增资源有 manifest/SHA256。
- [ ] 已补测试或说明无法测试原因。
- [ ] 已运行对应质量门禁命令。
- [ ] 文档已同步。

质检验收：

- [ ] 能从 README 找到相关标准和设计文档。
- [ ] 能根据任务中心追踪每个状态变更。
- [ ] 能从审计中看到关键操作。
- [ ] 能在失败日志中定位原因。
- [ ] 能确认 secret 未泄露。
- [ ] 能确认发布包资产完整且 checksum 通过。
- [ ] 能确认备份、升级、回滚语义清楚。
