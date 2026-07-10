# Orchestration Workbench Design

Status: Draft 0.1
Date: 2026-07-09
Audience: product, frontend, backend, runtime-agent, testing, delivery, and QA

本文设计 AIFAR 的“编排工作台”：用户可以先上传脚本、安装文件和动作元数据，形成可版本化、可校验、可审批的“动作包”，再在页面上把这些动作包编排成可保存、可预演、可执行、可审计、可回滚的部署流程。Docker、MySQL、Redis、MinIO、Nacos、AIFAR runtime-v2 不应是平台写死的能力边界，只能作为预置动作包或迁移样例。

核心结论：

- 可以做页面编排。
- 编排单位必须是“用户自定义动作包”或“平台预置动作包”发布出来的受控 action，不是页面临时输入的自由 shell。
- 用户可以上传脚本和安装文件，但上传后必须经过解析、SHA256 校验、路径校验、schema 校验、版本快照和发布审批，运行时只能引用已发布版本。
- `aifar-server` 负责模板、校验、权限、计划、全局调度、任务、审计和状态事实源。
- `aifar-agent` 负责目标节点本地 job 执行、状态、日志、锁和 runtime apply。
- 第一阶段可以不改 agent，继续由 server 通过 SSH 执行；第二阶段再把本地执行下沉到 agent job runner。

## 1. 产品目标

编排工作台解决的问题：

1. 不再把复杂部署流程硬编码成单个“安装”按钮。
2. 允许用户自行定义动作包：上传安装包、脚本、配置模板和 `deployspec.yaml`，由平台生成可编排动作。
3. 每一步都能看到输入、输出、目标服务器、依赖、失败处理和回滚策略。
4. 编排过程仍满足平台安全边界：权限、任务、审计、凭据、资源准入、日志脱敏。
5. 编排模板可以组合自定义动作包和预置动作包，并支持保存、复制、版本化、预演和重复执行。

非目标：

- 不做运行时任意 shell 平台；脚本必须先进入动作包，完成校验、发布和版本快照后才能执行。
- 不让前端绕过后端直接控制服务器。
- 不把 RBAC、catalog、任务、审计下沉到 agent。
- 不在第一阶段做复杂 BPMN/低代码平台。

## 2. 页面形态

推荐页面名：`编排工作台` / `Orchestration`。

推荐路由：

```text
/orchestration
/orchestration/templates/:id
/orchestration/runs/:taskId
```

推荐前端目录：

```text
web/src/orchestration/
  api.ts
  types.ts
  schema.ts
  stages.ts
  validators.ts
  useOrchestration.ts
  components/
    TemplateList.vue
    WorkflowDesigner.vue
    ActionLibrary.vue
    PackageUploadDialog.vue
    PackageDetailDrawer.vue
    PackageReviewPanel.vue
    PhaseList.vue
    StepCard.vue
    StepPropertyPanel.vue
    VariablePanel.vue
    PlanPreview.vue
    RunDrawer.vue
    RollbackPanel.vue
    YamlPreview.vue
  i18n.ts
web/src/views/OrchestrationView.vue
```

第一阶段不建议直接做自由拖拽 DAG 画布。推荐使用更稳的“阶段列表 + 并行组”：

```text
阶段 1: 准备
  - 执行动作包 custom-os-precheck.check
  - 执行动作包 docker-offline.install

阶段 2: 数据层
  并行:
    - 执行动作包 mysql-innodb-cluster.install
    - 执行动作包 redis-sentinel.install
    - 执行动作包 minio-distributed.install

阶段 3: 配置中心
  - 执行动作包 nacos-cluster.install
  - 执行动作包 nacos-config.publish

阶段 4: 应用运行时
  - 执行动作包 aifar-runtime.install
  - 执行动作包 business-services.install
  - 执行动作包 runtime.reconcile

阶段 5: 验收
  - 检查全部实例
  - 生成验收报告
```

页面布局：

| 区域 | 内容 |
| --- | --- |
| 左侧 Action Library | 已发布动作包生成的动作；支持筛选分类、版本、来源、是否预置、是否通过质检。 |
| 中间 Phase List | 阶段列表、步骤顺序、并行组、依赖关系、禁用状态。 |
| 右侧 Property Panel | 当前步骤参数、目标服务器、凭据引用、失败策略、输出变量。 |
| 底部 Plan Preview | dry-run 结果、实际 task steps、资源缺失、权限缺失、风险提示。 |
| 运行抽屉 Run Drawer | 当前运行状态、target、step、日志、重试、取消。 |

动作包管理建议作为同一页面的一级 Tab，避免“上传资源”和“编排模板”割裂：

| Tab | 内容 |
| --- | --- |
| 动作包 | 上传、解析、校验、质检、发布、下线动作包。 |
| 编排模板 | 使用已发布动作包组合部署流程。 |
| 运行记录 | 查看编排任务、目标节点、步骤、日志和回滚入口。 |

## 3. 用户自定义动作包

动作包是编排工作台的一等能力。平台不要求用户先等开发人员写 Go/Vue 模块，而是允许用户上传一个结构化包：

```text
resources/<package>/<version>/
  manifest.json
  deployspec.yaml
  checksums.sha256
  files/
    app.tar.gz
    image.tar
    extra-assets/
  rpms/
    *.rpm
  scripts/
    install.sh
    check.sh
    uninstall.sh
    backup.sh
    restore.sh
    update.sh
    rollback.sh
  templates/
    *.tmpl
  README.md
```

文件职责：

| 文件/目录 | 职责 | 是否每个动作包都必须使用 |
| --- | --- | --- |
| `manifest.json` | 文件清单、版本、hash、资源元数据。 | 必须。 |
| `deployspec.yaml` | 动作包声明、参数 schema、拓扑、脚本入口、输出变量、回滚能力。 | 必须。 |
| `checksums.sha256` | 包内文件完整性校验。 | 强烈建议，发布前应生成。 |
| `files/` | 安装包、镜像包、二进制、静态资源。 | 按动作需要。 |
| `rpms/` | 离线系统依赖。 | 按动作需要。 |
| `scripts/` | 受信脚本入口。 | 至少需要一个可执行动作脚本。 |
| `templates/` | 配置模板，由 server 渲染为上下文文件。 | 按动作需要。 |
| `README.md` | 交付说明、人工验收、风险和回滚说明。 | 质检必读，运行时不依赖。 |

动作包生命周期：

```text
upload -> unpack/quarantine -> parse deployspec -> verify checksum
  -> validate path/schema/scripts -> security review -> publish version
  -> appear in action library -> dry-run -> run -> audit/run snapshot
```

约束：

- 前端可以上传脚本和文件，但不能在运行时直接提交一段 shell 给后端执行。
- 脚本路径必须是包内相对路径，禁止绝对路径和 `..`。
- shell 脚本在发布前由 server 规范化为 UTF-8、无 BOM、LF 换行，并以规范化结果生成执行 hash。
- 动作包发布后不可原地修改；任何修改都生成新版本。
- 编排模板运行时引用动作包版本快照，不能引用“最新版本”这种可变目标。
- 预置 Docker/MySQL/Redis/MinIO/Nacos/AIFAR 能力也应逐步沉淀成同一种包模型，最多在来源字段上标记为 `builtin`。

当前已落地的最小可执行子集使用 ZIP 根目录 `deployspec.json`，格式为 `aifar.io/v1alpha1` `ActionPackage`。一个 Action 可以声明有序 `steps`，每步绑定一份包内脚本、只读标记和独立超时；当前支持 `check` 与 `install`，支持发布、Action Library、dry-run、顺序执行、任务步骤、目标日志和审计。完整 `deployspec.yaml`、跨 Action 模板、变量传递、并行组和回滚仍属于后续模型。

## 4. 编排模型

推荐模型：

```json
{
  "apiVersion": "aifar.io/v1",
  "kind": "OrchestrationTemplate",
  "name": "aifar-full-stack",
  "version": 1,
  "inputs": [
    { "name": "servers", "type": "server-list", "required": true },
    { "name": "dbRootCredentialId", "type": "credential", "kind": "database", "required": true }
  ],
  "phases": [
    {
      "name": "runtime-base",
      "title": "基础运行时",
      "mode": "sequential",
      "steps": [
        {
          "id": "install-container-runtime",
          "action": "packages.docker-offline.install",
          "package": { "name": "docker-offline", "version": "24.0.9-aifar.1" },
          "targets": "{{ inputs.servers }}",
          "with": { "installRoot": "/aifar/apps/docker" },
          "onFailure": "stop"
        }
      ]
    }
  ],
  "rollback": [
    { "from": "install-container-runtime", "action": "packages.docker-offline.uninstall" }
  ]
}
```

核心对象：

| 对象 | 说明 |
| --- | --- |
| Template | 用户保存的编排模板。 |
| Version | 模板版本，运行时必须使用快照。 |
| Input | 模板运行前填写的变量，例如服务器、版本、凭据、端口。 |
| Phase | 阶段，支持 sequential 或 parallel。 |
| Step | 具体动作，引用受控 action。 |
| Package | 用户上传或平台预置的动作包。 |
| Action | 动作包发布后由 server 注册出来的可执行能力。 |
| Script | action 在目标节点执行的受信脚本入口。 |
| Artifact | action 需要上传到目标节点的安装文件或依赖。 |
| Output | 步骤产物，例如 instanceId、endpoint、credentialBinding。 |
| Rollback | 失败或用户触发回滚时执行的补偿动作。 |
| Run | 一次执行，底层映射到 worker task。 |

## 5. 动作包与 Action 注册标准

所有可被页面编排的动作必须由 server 注册。注册来源分两类：

1. 用户上传并发布的动作包。
2. 平台内置的预置动作包，或者兼容旧 app module 暴露出来的动作。

建议新增动作包定义：

```go
type PackageDefinition struct {
    Name        string
    Version     string
    Source      string // user, builtin
    Title       string
    Description string
    Category    string
    ManifestHash string
    SpecHash     string
    Published    bool
    Actions      []ActionDefinition
}
```

建议新增后端接口：

```go
type ActionDefinition struct {
    Name        string
    Title       string
    Description string
    Category    string
    PackageName string
    PackageVersion string
    Operation   string
    InputSchema  ActionSchema
    OutputSchema ActionSchema
    RequiredPermissions []rbac.Permission
    TargetMode registry.TargetMode
    Stages []installflow.Stage
    ScriptPath string
    ArtifactPaths []string
    SupportsDryRun bool
    SupportsRollback bool
}

type ActionProvider interface {
    Actions(ctx context.Context, lang string) []ActionDefinition
    PlanAction(ctx context.Context, req ActionRequest) (ActionPlan, error)
    RunAction(ctx context.Context, req ActionRequest, run ActionRunContext) (ActionResult, error)
}
```

现有 app module 可以逐步迁移为预置动作包；迁移完成前，也可以通过兼容层暴露为 action：

| 现有能力 | 编排 action |
| --- | --- |
| `Install` | `packages.<package>.install` |
| `Check` | `packages.<package>.check` |
| `Delete` | `packages.<package>.uninstall` |
| `ArtifactUpdate` | `packages.aifar-runtime.update-artifact` |
| `ArtifactBundleUpdate` | `packages.aifar-runtime.update-artifact-bundle` |
| `RuntimeReconcile` | `packages.aifar-runtime.reconcile` |
| `RuntimeConfig` | `packages.aifar-runtime.config` |
| `ServiceScaleOut` | `packages.business-services.scale-out` |
| `ClusterStart` | `packages.mysql-innodb-cluster.cluster-start` |
| Nacos config publish | `packages.nacos-config.publish` |
| Nacos config rollback | `packages.nacos-config.rollback` |

## 6. 后端 server 改造

推荐新增目录：

```text
backend/internal/orchestration/
  package.go
  package_store.go
  package_upload.go
  package_validate.go
  package_publish.go
  action.go
  definition.go
  validate.go
  planner.go
  executor.go
  variables.go
  rollback.go
  store.go
  i18n.go
  orchestration_test.go
```

职责：

| 文件 | 职责 |
| --- | --- |
| `package.go` | 动作包领域模型，描述包、版本、来源、发布状态、hash 和 actions。 |
| `package_store.go` | 动作包元数据、版本、发布状态、质检状态持久化。 |
| `package_upload.go` | 上传、解包到隔离区、生成文件清单和 hash。 |
| `package_validate.go` | 校验 `deployspec.yaml`、脚本路径、artifact 路径、schema 和危险字段。 |
| `package_publish.go` | 发布、下线、版本冻结和审计。 |
| `action.go` | action registry，聚合已发布动作包、预置动作包和兼容 app modules。 |
| `definition.go` | 编排模板结构、phase、step、input、output、rollback。 |
| `validate.go` | 校验 action 是否存在、权限、schema、目标、依赖、循环引用。 |
| `planner.go` | dry-run，生成完整执行计划和 task step plan。 |
| `executor.go` | 将 phase/step 转成 worker task 执行。 |
| `variables.go` | 处理输入变量、步骤输出、模板表达式。 |
| `rollback.go` | 失败补偿和手动回滚计划。 |
| `store.go` | 模板保存、版本快照、运行记录查询。 |
| `i18n.go` | 编排页面和任务日志后端文案。 |

推荐新增 API：

```text
GET    /api/v2/orchestration/actions
GET    /api/v2/orchestration/packages
POST   /api/v2/orchestration/packages/upload
GET    /api/v2/orchestration/packages/{id}
POST   /api/v2/orchestration/packages/{id}/validate
POST   /api/v2/orchestration/packages/{id}/publish
POST   /api/v2/orchestration/packages/{id}/unpublish
GET    /api/v2/orchestration/templates
POST   /api/v2/orchestration/templates
GET    /api/v2/orchestration/templates/{id}
PUT    /api/v2/orchestration/templates/{id}
DELETE /api/v2/orchestration/templates/{id}
POST   /api/v2/orchestration/templates/{id}/dry-run
POST   /api/v2/orchestration/templates/{id}/run
GET    /api/v2/orchestration/runs/{taskId}
POST   /api/v2/orchestration/runs/{taskId}/rollback
```

权限：

```text
orchestration.view
orchestration.manage
orchestration.run
orchestration.rollback
orchestration.package.manage
orchestration.package.publish
```

建议新增数据库表：

```sql
orchestration_templates
orchestration_template_versions
orchestration_runs
orchestration_packages
orchestration_package_versions
orchestration_package_files
```

第一阶段可以不新增 `orchestration_run_steps`，继续复用 `tasks`、`task_targets`、`task_steps`、`task_logs`。如果后续需要更复杂的变量输出和节点级回放，再新增 step output 表。

server 必须负责：

- 接收动作包上传并落入隔离区。
- 解析 `manifest.json` 和 `deployspec.yaml`。
- 生成并校验 SHA256 文件清单。
- 校验脚本路径、artifact 路径、参数 schema、输出 schema 和回滚声明。
- 管理动作包版本、质检状态、发布状态和下线状态。
- 保存模板。
- 校验模板。
- 生成执行计划。
- 检查 RBAC。
- 解析凭据引用。
- 选择动作包版本和资源版本。
- 决定多服务器顺序、并发、coordinator。
- 创建 worker task。
- 写 task、target、step、audit。
- 记录模板版本和运行快照。

server 不应负责：

- 执行未发布、未校验、未纳入动作包版本快照的临时 shell。
- 在前端传来的步骤里相信任意命令。
- 把所有节点本地状态都长期轮询成同步阻塞 API。

## 7. agent 改造

第一阶段 agent 可以不改，server 仍通过 SSH 调用现有 installer。

第二阶段建议新增 agent job runner：

```text
GET  /node/facts
POST /jobs
GET  /jobs/{id}
GET  /jobs/{id}/logs
GET  /jobs/{id}/logs/events
POST /jobs/{id}/cancel
```

Job 只接受结构化定义：

```json
{
  "jobId": "task_xxx:srv-1:customdb-install",
  "package": "customdb",
  "packageVersion": "1.0.0-aifar.1",
  "action": "packages.customdb.install",
  "instanceId": "customdb_cluster_xxx:srv-1",
  "workDir": "/aifar/apps/_work/customdb-1.0.0-task_xxx",
  "script": "scripts/install.sh",
  "scriptSha256": "...",
  "artifacts": [
    { "path": "files/customdb.tar.gz", "sha256": "..." }
  ],
  "contextPath": "context.json",
  "timeoutSeconds": 900,
  "lockKey": "customdb:/aifar/apps/customdb"
}
```

agent 负责：

- 校验本地 artifacts hash。
- 校验脚本 hash 和动作包版本。
- 创建工作目录和权限。
- 执行受信脚本。
- 维护本地 job 状态。
- 提供日志 tail/SSE。
- 本地锁，避免同一 installRoot 并发冲突。
- runtime apply/status/proxy/Nacos heartbeat。

agent 不负责：

- 判断用户有没有权限。
- 保存编排模板。
- 选择服务器。
- 决定集群 coordinator。
- 访问平台 SQLite。
- 执行前端/API 传来的自由 shell。
- 接收用户临时上传的未发布脚本并直接运行。

## 8. 页面使用流程

### 8.1 管理动作包

1. 用户进入编排工作台。
2. 进入“动作包”Tab。
3. 上传动作包压缩文件，或上传脚本、安装文件后由页面生成动作包草稿。
4. server 解包到隔离区，解析 `deployspec.yaml` 和 `manifest.json`。
5. 页面展示动作、参数、脚本入口、安装文件、hash、风险和质检结果。
6. 用户修正后重新上传，或由具备权限的人发布版本。
7. 发布后的动作出现在 Action Library。

### 8.2 创建模板

1. 用户进入“编排模板”Tab。
2. 左侧从 Action Library 选择已发布 action。
3. 中间放入阶段。
4. 右侧配置目标、版本、凭据、参数。
5. 点击 dry-run。
6. server 返回完整计划、风险、缺失资源、权限问题。
7. 用户保存模板。

### 8.3 执行模板

1. 用户选择模板版本。
2. 填写运行 inputs。
3. 点击预演。
4. 预演通过后点击执行。
5. server 创建一个 orchestration task。
6. 页面展示 phase、step、target、日志。
7. 失败时按策略 stop/continue/rollback/manual。

### 8.4 失败策略

每个 step 支持：

| 策略 | 说明 |
| --- | --- |
| `stop` | 默认，失败后停止后续步骤。 |
| `continue` | 记录失败，继续后续步骤。 |
| `rollback` | 执行 rollback 计划。 |
| `manual` | 暂停，等待人工处理后继续/跳过/回滚。 |

## 9. 预置动作包示例

以下不是平台必须固定内置的动作清单，而是可以随交付包提供的“预置动作包示例”。用户自定义动作包与这些预置包在编排工作台中应当使用同一套展示、校验、dry-run、执行和审计机制。

| 预置包 | 可声明 Actions |
| --- | --- |
| docker-offline | install、check、uninstall |
| mysql-standalone / mysql-innodb-cluster | install、check、cluster-start、backup、restore、update、rollback、uninstall |
| mysql-router | install、check、uninstall |
| redis-standalone / redis-sentinel / redis-cluster | install、check、backup、restore、update、rollback、uninstall |
| minio-standalone / minio-distributed | install、check、bucket-replication、backup、restore、update、rollback、uninstall |
| nacos-standalone / nacos-cluster / nacos-config | install、check、config-publish、config-rollback、uninstall |
| aifar-runtime / business-services | install、check、update-artifact、update-bundle、runtime-config、reconcile、scale-out、scale-in、offline-service、uninstall |
| user-defined | 由 `deployspec.yaml` 声明，例如 install、check、uninstall、backup、restore、update、rollback |

## 10. 推荐落地阶段

### M1: 动作包上传、校验和只读动作库

- 后端提供动作包 upload、validate、publish、actions 查询。
- 前端提供动作包列表、上传、校验结果、发布状态和 Action Library。
- 支持从已发布动作包生成只读 action catalog。
- 不执行真实任务。

### M2: 单动作包执行闭环

- 支持选择一个已发布动作包 action 进行 dry-run 和执行。
- 使用 SSH 上传动作包文件、渲染 context、执行包内脚本。
- 复用 worker task。
- 写 task、target、step、audit 和运行快照。

### M3: 模板保存和顺序编排

- 新增模板表。
- 支持 sequential phase。
- 支持多个动作包 step 串联执行。
- 支持步骤输出变量给后续步骤使用。

### M4: 并行组、coordinator 和 rollback

- 支持 phase `mode=parallel`。
- 支持 cluster coordinator step。
- 支持变量输出，例如 MySQL endpoint 给 Nacos/AIFAR 使用。
- 支持 stop/continue/manual/rollback。
- 支持 rollback plan。
- 支持运行快照。

### M5: agent job runner

- 新增 `/jobs`。
- server 仍是任务事实源。
- agent 承担节点本地执行、日志和锁。

### M6: 预置服务迁移为动作包

- 将 Docker/MySQL/Redis/MinIO/Nacos/AIFAR 逐步迁移为同模型预置动作包。
- 兼容旧 app module，迁移期间不破坏现有 Apps 商店部署入口。
- 前端不写具体应用分支。

## 11. 质检 Checklist

- [ ] 页面不能输入自由 shell。
- [ ] 每个 step 都能追溯到已发布动作包版本、server 注册 action、脚本路径和脚本 hash。
- [ ] 动作包上传后先进入隔离区，未发布版本不能执行。
- [ ] `deployspec.yaml` 必须校验参数 schema、目标模式、脚本路径、artifact 路径和输出变量。
- [ ] 动作包发布后不可原地修改，模板运行使用版本快照。
- [ ] dry-run 能发现资源缺失、权限缺失、目标数量错误。
- [ ] 运行后有 task、target、step、audit。
- [ ] secret 只用 credential id 引用，不进入日志和模板明文。
- [ ] 并行和 coordinator 阶段有明确顺序。
- [ ] 失败后能知道哪些节点完成、哪些节点未完成。
- [ ] rollback 不自动删除未声明可删除的数据。
- [ ] agent job runner 不暴露公网/内网裸 HTTP 管理面。
- [ ] 前端中英文完整。
