export type OrchestrationSource = 'builtin' | 'user'
export type PackageStatus = 'published' | 'reviewing'
export type FieldType = 'server-list' | 'credential' | 'path' | 'number' | 'select' | 'switch'
export type PhaseMode = 'sequential' | 'parallel'
export type FailurePolicy = 'stop' | 'continue' | 'rollback' | 'manual'
export type DryRunSeverity = 'info' | 'warning' | 'error'

export type SchemaField = {
  name: string
  labelKey: string
  type: FieldType
  componentKey: string
  required?: boolean
  secret?: boolean
  defaultValue?: string | number | boolean
}

export type ActionDefinition = {
  id: string
  labelKey: string
  operation: string
  targetMode: 'single' | 'multiple' | 'cluster'
  stages: string[]
  rollback?: boolean
}

export type ActionPackage = {
  id: string
  name: string
  version: string
  titleKey: string
  source: OrchestrationSource
  status: PackageStatus
  qualityScore: number
  hash: string
  actions: ActionDefinition[]
  fields: SchemaField[]
  files: Array<{
    path: string
    hash: string
  }>
}

export type TemplateStep = {
  id: string
  labelKey: string
  actionId: string
  packageId: string
  packageVersion: string
  targetsKey: string
  failurePolicy: FailurePolicy
}

export type TemplatePhase = {
  id: string
  titleKey: string
  mode: PhaseMode
  steps: TemplateStep[]
}

export type OrchestrationTemplate = {
  id: string
  titleKey: string
  version: number
  phases: TemplatePhase[]
}

export type DryRunStep = {
  id: string
  phaseKey: string
  stepKey: string
  actionId: string
  packageVersion: string
  stages: string[]
  targetsKey: string
  failurePolicy: FailurePolicy
}

export type DryRunIssue = {
  severity: DryRunSeverity
  messageKey: string
  params?: Record<string, unknown>
}

export type DryRunResult = {
  generatedAt: string
  steps: DryRunStep[]
  issues: DryRunIssue[]
}

export type RunRecord = {
  id: string
  templateKey: string
  version: number
  status: 'success' | 'failed' | 'running'
  actor: string
  targetCount: number
  stepCount: number
  startedAt: string
}

export type LocalizedText = Record<string, string>

export type ApiActionDefinition = {
  id: string
  packageId: string
  packageName: string
  packageVersion: string
  name: string
  title: LocalizedText
  description: LocalizedText
  operation: string
  scriptPath: string
  scriptSha256: string
  targetMode: 'single' | 'multiple' | 'cluster'
  topology?: string
  minTargets: number
  maxTargets: number
  readOnly: boolean
  timeoutSeconds: number
  stages: string[]
  steps: ApiActionStep[]
  phases: ApiActionPhase[]
}

export type ApiActionPhase = {
  name: string
  title: LocalizedText
  scope: 'node' | 'cluster' | 'barrier'
  mode: PhaseMode
  runOn?: 'all' | 'coordinator'
  order: number
  steps: ApiActionStep[]
}

export type ApiActionStep = {
  id: string
  name: string
  title: LocalizedText
  scriptPath: string
  scriptSha256: string
  readOnly: boolean
  timeoutSeconds: number
  order: number
}

export type ApiActionPackage = {
  id: string
  name: string
  version: string
  title: LocalizedText
  description: LocalizedText
  status: string
  packageSha256: string
  sourceFilename?: string
  uploadedBy?: string
  createdAt?: string
  updatedAt?: string
  publishedAt?: string
  actions: ApiActionDefinition[]
}

export type DryRunPlanStep = {
  target: string
  name: string
  title: string
  order: number
  phase?: string
  scope?: 'node' | 'cluster' | 'barrier'
  mode?: PhaseMode
}

export type DryRunTarget = {
  serverId: string
  name: string
  host: string
  status: string
  role?: 'coordinator' | 'member'
  coordinator?: boolean
  order?: number
}

export type ApiDryRunIssue = {
  severity: DryRunSeverity
  message: string
}

export type ApiDryRunResult = {
  action: ApiActionDefinition
  generatedAt: string
  targets: DryRunTarget[]
  steps: DryRunPlanStep[]
  issues: ApiDryRunIssue[]
}
