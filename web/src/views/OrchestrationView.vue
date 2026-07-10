<template>
  <PageShell class="orchestration-page" :title="t('orchestration.title')" :subtitle="t('orchestration.subtitle')">
    <template #actions>
      <el-button :loading="loading" @click="loadAll">{{ t('common.refresh') }}</el-button>
      <el-upload :show-file-list="false" accept=".zip" :http-request="uploadPackage">
        <el-button type="primary" :loading="uploading">{{ t('orchestration.uploadPackage') }}</el-button>
      </el-upload>
    </template>

    <div class="metric-grid">
      <div class="metric-card">
        <span class="label">{{ t('orchestration.metric.packages') }}</span>
        <strong class="value">{{ packages.length }}</strong>
      </div>
      <div class="metric-card">
        <span class="label">{{ t('orchestration.metric.actions') }}</span>
        <strong class="value">{{ actions.length }}</strong>
      </div>
      <div class="metric-card">
        <span class="label">{{ t('orchestration.selectedServers') }}</span>
        <strong class="value">{{ selectedServerIds.length }}</strong>
      </div>
      <div class="metric-card">
        <span class="label">{{ t('orchestration.metric.dryRunIssues') }}</span>
        <strong class="value">{{ dryRunResult?.issues.length ?? 0 }}</strong>
      </div>
    </div>

    <el-alert class="orchestration-alert" type="info" show-icon :closable="false" :title="t('orchestration.realLoop')" />

    <el-tabs v-model="activeTab" class="aifar-panel orchestration-tabs">
      <el-tab-pane :label="t('orchestration.tabs.packages')" name="packages" />
      <el-tab-pane :label="t('orchestration.planPreview')" name="plan" />
      <el-tab-pane :label="t('orchestration.tabs.runs')" name="runs" />
    </el-tabs>

    <div v-if="activeTab === 'packages'" class="orchestration-grid">
      <section class="workspace-card package-list">
        <div class="section-title">{{ t('orchestration.actionLibrary') }}</div>
        <el-empty v-if="!packages.length" :description="t('orchestration.noPackages')" :image-size="90" />
        <button
          v-for="pkg in packages"
          :key="pkg.id"
          class="package-row"
          :class="{ active: pkg.id === selectedPackageId }"
          type="button"
          @click="selectPackage(pkg)"
        >
          <span class="package-title">{{ localized(pkg.title, pkg.name) }}</span>
          <span class="package-meta">{{ pkg.name }}@{{ pkg.version }}</span>
          <span class="package-tags">
            <StatusTag :status="pkg.status === 'published' ? 'success' : 'pending'" :label="packageStatusLabel(pkg.status)" />
            <el-tag size="small" effect="plain">{{ pkg.actions.length }} {{ t('common.action') }}</el-tag>
          </span>
        </button>
      </section>

      <section class="workspace-card package-detail">
        <div v-if="selectedPackage" class="detail-content">
          <div class="detail-head">
            <div>
              <h2>{{ localized(selectedPackage.title, selectedPackage.name) }}</h2>
              <p>{{ selectedPackage.packageSha256 }}</p>
            </div>
            <StatusTag :status="selectedPackage.status === 'published' ? 'success' : 'pending'" :label="packageStatusLabel(selectedPackage.status)" />
          </div>

          <el-form label-position="top" class="run-form">
            <el-form-item :label="t('orchestration.selectAction')">
              <el-select v-model="selectedActionId" class="full-width" :placeholder="t('orchestration.selectAction')" filterable>
                <el-option
                  v-for="action in selectedPackage.actions"
                  :key="action.id"
                  :label="`${localized(action.title, action.name)} / ${action.operation}`"
                  :value="action.id"
                />
              </el-select>
            </el-form-item>
            <el-form-item :label="t('orchestration.selectServers')">
              <el-select
                v-model="selectedServerIds"
                class="full-width"
                multiple
                filterable
                collapse-tags
                collapse-tags-tooltip
				:multiple-limit="selectedAction?.maxTargets || 0"
                :placeholder="t('orchestration.selectServers')"
              >
                <el-option v-for="server in servers" :key="server.id" :label="serverLabel(server)" :value="server.id" />
              </el-select>
            </el-form-item>
			<el-form-item v-if="isClusterAction" :label="t('orchestration.clusterName')">
			  <el-input v-model="clusterName" class="full-width" :placeholder="t('orchestration.clusterNamePlaceholder')" />
			</el-form-item>
			<el-form-item v-if="isClusterAction" :label="t('orchestration.coordinator')">
			  <el-select v-model="selectedCoordinatorId" class="full-width" :placeholder="t('orchestration.selectCoordinator')" filterable>
				<el-option
				  v-for="server in selectedServers"
				  :key="server.id"
				  :label="serverLabel(server)"
				  :value="server.id"
				/>
			  </el-select>
			</el-form-item>
          </el-form>

		  <div v-if="isClusterAction" class="detail-block topology-block">
			<div class="block-heading">
			  <h3>{{ t('orchestration.clusterTopology') }}</h3>
			  <span>{{ targetRequirementLabel }}</span>
			</div>
			<el-table :data="selectedTopologyRows" size="small" empty-text="-">
			  <el-table-column prop="order" :label="t('orchestration.stepOrder')" width="72" />
			  <el-table-column prop="name" :label="t('table.server')" min-width="150" />
			  <el-table-column prop="host" :label="t('servers.host')" min-width="150" />
			  <el-table-column :label="t('orchestration.role')" width="150">
				<template #default="{ row }">
				  <el-tag size="small" :type="row.coordinator ? 'success' : 'info'" effect="plain">
					{{ row.coordinator ? t('orchestration.role.coordinator') : t('orchestration.role.member') }}
				  </el-tag>
				</template>
			  </el-table-column>
			</el-table>
		  </div>

		  <div v-if="selectedAction" class="action-summary" :class="{ 'is-cluster': isClusterAction }">
            <div>
              <span>{{ t('common.operation') }}</span>
              <strong>{{ selectedAction.operation }}</strong>
            </div>
            <div>
              <span>{{ t('orchestration.script') }}</span>
              <strong>{{ selectedAction.scriptPath }}</strong>
            </div>
            <div>
              <span>{{ t('orchestration.readOnly') }}</span>
              <strong>{{ selectedAction.readOnly ? 'true' : 'false' }}</strong>
            </div>
			<div v-if="isClusterAction">
			  <span>{{ t('orchestration.topology') }}</span>
			  <strong>{{ selectedAction.topology || '-' }}</strong>
			</div>
          </div>

		  <div v-if="isClusterAction && phaseStepRows.length" class="detail-block">
			<h3>{{ t('orchestration.phases') }}</h3>
			<el-table :data="phaseStepRows" size="small">
			  <el-table-column prop="phaseOrder" :label="t('orchestration.stepOrder')" width="72" />
			  <el-table-column prop="phaseTitle" :label="t('orchestration.phase')" min-width="150" />
			  <el-table-column :label="t('orchestration.scope')" width="120">
				<template #default="{ row }">{{ t(`orchestration.scope.${row.scope}`) }}</template>
			  </el-table-column>
			  <el-table-column :label="t('orchestration.stepMode')" width="110">
				<template #default="{ row }">{{ t(`orchestration.mode.${row.mode}`) }}</template>
			  </el-table-column>
			  <el-table-column prop="stepTitle" :label="t('common.action')" min-width="170" />
			  <el-table-column prop="scriptPath" :label="t('orchestration.script')" min-width="210" show-overflow-tooltip />
			</el-table>
		  </div>

		  <div v-else-if="selectedAction?.steps.length" class="detail-block">
            <h3>{{ t('orchestration.actionSteps') }}</h3>
            <el-table :data="selectedAction.steps" size="small">
              <el-table-column prop="order" :label="t('orchestration.stepOrder')" width="72" />
              <el-table-column :label="t('common.action')" min-width="170">
                <template #default="{ row }">{{ localized(row.title, row.name) }}</template>
              </el-table-column>
              <el-table-column :label="t('orchestration.stepMode')" width="110">
                <template #default="{ row }">
                  <el-tag size="small" :type="row.readOnly ? 'info' : 'warning'" effect="plain">
                    {{ row.readOnly ? t('orchestration.readOnly') : t('orchestration.mutating') }}
                  </el-tag>
                </template>
              </el-table-column>
              <el-table-column :label="t('orchestration.timeout')" width="100">
                <template #default="{ row }">{{ row.timeoutSeconds }}s</template>
              </el-table-column>
              <el-table-column prop="scriptPath" :label="t('orchestration.script')" min-width="220" show-overflow-tooltip />
            </el-table>
          </div>

          <div class="button-row">
            <el-button :loading="dryRunning" :disabled="!canSubmit" @click="runDryRun">{{ t('orchestration.backendDryRun') }}</el-button>
            <el-button type="primary" :loading="running" :disabled="!canSubmit" @click="runAction">
              {{ selectedAction?.readOnly ? t('orchestration.runCheck') : t('orchestration.runAction') }}
            </el-button>
          </div>

          <div class="detail-block">
            <h3>{{ t('orchestration.actions') }}</h3>
            <el-table :data="selectedPackage.actions" size="small">
              <el-table-column :label="t('common.action')" min-width="160">
                <template #default="{ row }">{{ localized(row.title, row.name) }}</template>
              </el-table-column>
              <el-table-column prop="operation" :label="t('common.operation')" width="110" />
              <el-table-column prop="targetMode" :label="t('common.target')" width="120" />
              <el-table-column prop="scriptPath" :label="t('orchestration.script')" min-width="180" show-overflow-tooltip />
            </el-table>
          </div>
        </div>
        <el-empty v-else :description="t('orchestration.noPackages')" :image-size="100" />
      </section>
    </div>

    <section v-else-if="activeTab === 'plan'" class="workspace-card plan-panel">
      <div class="section-title">{{ t('orchestration.planPreview') }}</div>
      <div v-if="dryRunResult" class="plan-content">
        <div class="plan-head">
          <span>{{ t('orchestration.generatedAt') }}</span>
          <strong>{{ new Date(dryRunResult.generatedAt).toLocaleString() }}</strong>
        </div>
        <el-alert
          v-for="issue in dryRunResult.issues"
          :key="issue.message"
          class="plan-issue"
          :type="issue.severity === 'error' ? 'error' : issue.severity === 'warning' ? 'warning' : 'info'"
          show-icon
          :closable="false"
          :title="issue.message"
        />
        <el-table :data="dryRunResult.targets" size="small">
		  <el-table-column prop="order" :label="t('orchestration.stepOrder')" width="72" />
          <el-table-column prop="name" :label="t('table.server')" min-width="150" />
          <el-table-column prop="host" :label="t('servers.host')" min-width="160" />
		  <el-table-column v-if="dryRunResult.action.targetMode === 'cluster'" :label="t('orchestration.role')" width="150">
			<template #default="{ row }">{{ row.coordinator ? t('orchestration.role.coordinator') : t('orchestration.role.member') }}</template>
		  </el-table-column>
          <el-table-column :label="t('common.status')" width="130">
            <template #default="{ row }"><StatusTag :status="row.status" /></template>
          </el-table-column>
        </el-table>
        <el-table :data="dryRunResult.steps" size="small">
          <el-table-column prop="order" :label="t('orchestration.steps')" width="90" />
          <el-table-column prop="target" :label="t('common.target')" min-width="150" show-overflow-tooltip />
		  <el-table-column prop="phase" :label="t('orchestration.phase')" min-width="140" show-overflow-tooltip />
		  <el-table-column :label="t('orchestration.scope')" width="110">
			<template #default="{ row }">{{ row.scope ? t(`orchestration.scope.${row.scope}`) : '-' }}</template>
		  </el-table-column>
          <el-table-column prop="title" :label="t('common.action')" min-width="180" />
          <el-table-column prop="name" :label="t('common.module')" min-width="160" />
        </el-table>
      </div>
      <div v-else class="empty-state">
        <div>
          <strong>{{ t('orchestration.noDryRunTitle') }}</strong>
          <p>{{ t('orchestration.noDryRunDesc') }}</p>
        </div>
      </div>
    </section>

    <section v-else class="workspace-card runs-panel">
      <TaskLogPane type-prefix="orchestration." :can-manage="false" :disabled-reason="t('common.permissionDenied')" />
    </section>
  </PageShell>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { ElMessage, ElMessageBox, type UploadRequestOptions } from 'element-plus'
import { apiGet, apiPost, apiPostForm, asArray } from '../api/client'
import PageShell from '../components/PageShell.vue'
import StatusTag from '../components/StatusTag.vue'
import TaskLogPane from '../components/TaskLogPane.vue'
import { useI18n } from '../i18n'
import { useTaskProgressStore } from '../stores/taskProgress'
import type { ServerRecord } from '../servers/types'
import type { ApiActionDefinition, ApiActionPackage, ApiDryRunResult, LocalizedText } from '../orchestration/types'

const { t, locale } = useI18n()
const taskProgress = useTaskProgressStore()

const activeTab = ref('packages')
const loading = ref(false)
const uploading = ref(false)
const dryRunning = ref(false)
const running = ref(false)
const packages = ref<ApiActionPackage[]>([])
const actions = ref<ApiActionDefinition[]>([])
const servers = ref<ServerRecord[]>([])
const selectedPackageId = ref('')
const selectedActionId = ref('')
const selectedServerIds = ref<string[]>([])
const selectedCoordinatorId = ref('')
const clusterName = ref('')
const dryRunResult = ref<ApiDryRunResult | null>(null)

const selectedPackage = computed(() => packages.value.find((pkg) => pkg.id === selectedPackageId.value) ?? packages.value[0] ?? null)
const selectedAction = computed(() => {
  const scoped = selectedPackage.value?.actions.find((action) => action.id === selectedActionId.value)
  return scoped ?? actions.value.find((action) => action.id === selectedActionId.value) ?? selectedPackage.value?.actions[0] ?? null
})
const isClusterAction = computed(() => selectedAction.value?.targetMode === 'cluster')
const selectedServers = computed(() => selectedServerIds.value
  .map((id) => servers.value.find((server) => server.id === id))
  .filter((server): server is ServerRecord => Boolean(server)))
const selectedTopologyRows = computed(() => selectedServers.value.map((server, index) => ({
  ...server,
  order: index + 1,
  coordinator: server.id === selectedCoordinatorId.value
})))
const phaseStepRows = computed(() => (selectedAction.value?.phases ?? []).flatMap((phase) => {
  if (!phase.steps.length) {
    return [{
      phaseOrder: phase.order,
      phaseTitle: localized(phase.title, phase.name),
      scope: phase.scope,
      mode: phase.mode,
      stepTitle: t('orchestration.barrier'),
      scriptPath: '-'
    }]
  }
  return phase.steps.map((step) => ({
    phaseOrder: phase.order,
    phaseTitle: localized(phase.title, phase.name),
    scope: phase.scope,
    mode: phase.mode,
    stepTitle: localized(step.title, step.name),
    scriptPath: step.scriptPath
  }))
}))
const targetRequirementLabel = computed(() => {
  const action = selectedAction.value
  if (!action) return ''
  if (action.minTargets === action.maxTargets && action.minTargets > 0) {
    return t('orchestration.exactTargets', { count: action.minTargets })
  }
  return t('orchestration.targetRange', { min: action.minTargets, max: action.maxTargets || '∞' })
})
const clusterNameValid = computed(() => /^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$/.test(clusterName.value.trim()))
const canSubmit = computed(() => {
  const action = selectedAction.value
  if (!action?.id || !selectedServerIds.value.length) return false
  if (!isClusterAction.value) return action.targetMode !== 'single' || selectedServerIds.value.length === 1
  const count = selectedServerIds.value.length
  return count >= action.minTargets && (!action.maxTargets || count <= action.maxTargets)
    && selectedServerIds.value.includes(selectedCoordinatorId.value)
    && clusterNameValid.value
})

watch(selectedAction, (action) => {
  dryRunResult.value = null
  if (action?.targetMode !== 'cluster') {
    selectedCoordinatorId.value = ''
    clusterName.value = ''
    if (action?.targetMode === 'single' && selectedServerIds.value.length > 1) {
      selectedServerIds.value = selectedServerIds.value.slice(0, 1)
    }
    return
  }
  if (action.maxTargets > 0 && selectedServerIds.value.length > action.maxTargets) {
    selectedServerIds.value = selectedServerIds.value.slice(0, action.maxTargets)
  }
  if (!selectedServerIds.value.includes(selectedCoordinatorId.value)) {
    selectedCoordinatorId.value = selectedServerIds.value[0] ?? ''
  }
  if (!clusterName.value) {
    clusterName.value = `${action.packageName.replace(/-/g, '_')}_cluster`.slice(0, 63)
  }
})

watch(selectedServerIds, (ids) => {
  dryRunResult.value = null
  if (isClusterAction.value && !ids.includes(selectedCoordinatorId.value)) {
    selectedCoordinatorId.value = ids[0] ?? ''
  }
}, { deep: true })

onMounted(loadAll)

async function loadAll() {
  loading.value = true
  try {
    const [packageList, actionList, serverList] = await Promise.all([
      apiGet<ApiActionPackage[] | null>('/orchestration/packages').catch(() => []),
      apiGet<ApiActionDefinition[] | null>('/orchestration/actions').catch(() => []),
      apiGet<ServerRecord[] | null>('/servers').catch(() => [])
    ])
    packages.value = asArray(packageList)
    actions.value = asArray(actionList)
    servers.value = asArray(serverList)
    if (!selectedPackageId.value && packages.value.length) {
      selectPackage(packages.value[0])
    } else if (selectedPackage.value && !selectedActionId.value) {
      selectedActionId.value = selectedPackage.value.actions[0]?.id ?? ''
    }
  } finally {
    loading.value = false
  }
}

async function uploadPackage(options: UploadRequestOptions) {
  uploading.value = true
  const form = new FormData()
  form.append('file', options.file)
  try {
    const pkg = await apiPostForm<ApiActionPackage>('/orchestration/packages/upload', form)
    upsertPackage(pkg)
    selectPackage(pkg)
    ElMessage.success(t('orchestration.uploadSuccess'))
    options.onSuccess?.(pkg)
    void loadAll()
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err)
    ElMessage.error(message)
    options.onError?.(err as any)
  } finally {
    uploading.value = false
  }
}

function upsertPackage(pkg: ApiActionPackage) {
  packages.value = [pkg, ...packages.value.filter((item) => item.id !== pkg.id)]
  const packageActionIds = new Set(pkg.actions.map((action) => action.id))
  actions.value = [...pkg.actions, ...actions.value.filter((action) => !packageActionIds.has(action.id))]
}

function selectPackage(pkg: ApiActionPackage) {
  selectedPackageId.value = pkg.id
  selectedActionId.value = pkg.actions[0]?.id ?? ''
  dryRunResult.value = null
}

function runRequestPayload() {
  const payload: Record<string, unknown> = {
    serverIds: selectedServerIds.value,
    language: locale.value
  }
  if (isClusterAction.value) {
    payload.coordinatorServerId = selectedCoordinatorId.value
    payload.clusterName = clusterName.value.trim()
  }
  return payload
}

async function runDryRun() {
  if (!canSubmit.value || !selectedAction.value) {
    ElMessage.warning(t('orchestration.selectionRequired'))
    return
  }
  dryRunning.value = true
  try {
    dryRunResult.value = await apiPost<ApiDryRunResult>(`/orchestration/actions/${encodeURIComponent(selectedAction.value.id)}/dry-run`, runRequestPayload())
    activeTab.value = 'plan'
    ElMessage.success(t('orchestration.dryRunReady'))
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  } finally {
    dryRunning.value = false
  }
}

async function runAction() {
  if (!canSubmit.value || !selectedAction.value) {
    ElMessage.warning(t('orchestration.selectionRequired'))
    return
  }
  if (!selectedAction.value.readOnly) {
    try {
      await ElMessageBox.confirm(
        t('orchestration.mutatingConfirm', { action: localized(selectedAction.value.title, selectedAction.value.name) }),
        t('orchestration.mutatingConfirmTitle'),
        {
          type: 'warning',
          confirmButtonText: t('orchestration.confirmRun'),
          cancelButtonText: t('common.cancel')
        }
      )
    } catch {
      return
    }
  }
  running.value = true
  try {
    const result = await apiPost<{ taskId: string }>(`/orchestration/actions/${encodeURIComponent(selectedAction.value.id)}/run`, runRequestPayload())
    taskProgress.track(result.taskId, localized(selectedAction.value.title, selectedAction.value.name))
    activeTab.value = 'runs'
    ElMessage.success(t('orchestration.runAccepted'))
  } catch (err) {
    ElMessage.error(err instanceof Error ? err.message : String(err))
  } finally {
    running.value = false
  }
}

function localized(value: LocalizedText | undefined, fallback = '') {
  if (!value) {
    return fallback
  }
  const key = locale.value
  return value[key] || value.zh || value.en || fallback
}

function packageStatusLabel(status: string) {
  return status === 'published' ? t('orchestration.status.published') : t('orchestration.status.reviewing')
}

function serverLabel(server: ServerRecord) {
  const name = server.name || server.id
  return server.host ? `${name} (${server.host})` : name
}
</script>

<style scoped>
.orchestration-page {
  min-width: 0;
}

.orchestration-alert,
.orchestration-tabs {
  flex-shrink: 0;
}

.orchestration-tabs {
  padding-bottom: 0;
}

.orchestration-tabs :deep(.el-tabs__header) {
  margin: 0;
}

.orchestration-tabs :deep(.el-tabs__nav-wrap::after) {
  height: 1px;
  background: var(--aifar-border-soft);
}

.orchestration-grid {
  display: grid;
  grid-template-columns: minmax(280px, 360px) minmax(0, 1fr);
  gap: 12px;
  min-width: 0;
}

.package-list,
.package-detail,
.plan-panel,
.runs-panel {
  min-width: 0;
}

.package-list {
  display: grid;
  align-content: start;
  gap: 8px;
  padding-bottom: 10px;
}

.package-row {
  width: calc(100% - 20px);
  margin: 0 10px;
  border: 1px solid var(--aifar-border);
  border-radius: var(--aifar-radius);
  background: #fff;
  padding: 10px;
  text-align: left;
  display: grid;
  gap: 6px;
  cursor: pointer;
  transition: border-color .16s ease, background .16s ease;
}

.package-row:hover,
.package-row.active {
  border-color: #91caff;
  background: #f7fbff;
}

.package-title {
  color: var(--aifar-ink);
  font-size: 14px;
  line-height: 20px;
  font-weight: 850;
}

.package-meta {
  color: var(--aifar-text-secondary);
  font-size: 12px;
  word-break: break-all;
}

.package-tags,
.button-row {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
  align-items: center;
}

.package-detail,
.plan-panel {
  padding: 12px;
}

.detail-content,
.plan-content {
  display: grid;
  gap: 12px;
  min-width: 0;
}

.detail-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--aifar-border-soft);
}

.detail-head h2 {
  margin: 0;
  color: var(--aifar-ink);
  font-size: 17px;
  line-height: 24px;
  font-weight: 850;
}

.detail-head p {
  margin: 4px 0 0;
  color: var(--aifar-text-secondary);
  font-size: 12px;
  word-break: break-all;
}

.run-form {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(min(100%, 260px), 1fr));
  gap: 12px;
}

.run-form :deep(.el-form-item) {
  margin-bottom: 0;
}

.full-width {
  width: 100%;
}

.action-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  border: 1px solid var(--aifar-border-soft);
  border-radius: var(--aifar-radius);
  overflow: hidden;
  background: #fbfdff;
}

.action-summary.is-cluster {
  grid-template-columns: repeat(4, minmax(0, 1fr));
}

.action-summary > div {
  display: grid;
  gap: 4px;
  min-width: 0;
  padding: 9px 10px;
}

.action-summary > div + div {
  border-left: 1px solid var(--aifar-border-soft);
}

.action-summary span {
  color: var(--aifar-text-tertiary);
  font-size: 11px;
}

.action-summary strong {
  color: var(--aifar-ink);
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.detail-block {
  display: grid;
  gap: 8px;
  min-width: 0;
}

.detail-block h3 {
  margin: 0;
  color: var(--aifar-ink);
  font-size: 14px;
  line-height: 20px;
  font-weight: 850;
}

.block-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.block-heading span {
  color: var(--aifar-text-secondary);
  font-size: 12px;
}

.topology-block {
  padding: 10px 0;
  border-top: 1px solid var(--aifar-border-soft);
  border-bottom: 1px solid var(--aifar-border-soft);
}

.plan-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--aifar-text-secondary);
  font-size: 12px;
}

.plan-head strong {
  color: var(--aifar-ink);
}

.plan-issue {
  margin: 0;
}

.runs-panel {
  display: grid;
  min-height: 640px;
  padding: 10px;
}

@media (max-width: 1100px) {
  .orchestration-grid {
    grid-template-columns: 1fr;
  }

  .action-summary {
    grid-template-columns: 1fr;
  }

  .action-summary.is-cluster {
    grid-template-columns: 1fr;
  }

  .action-summary > div + div {
    border-left: 0;
    border-top: 1px solid var(--aifar-border-soft);
  }
}
</style>
