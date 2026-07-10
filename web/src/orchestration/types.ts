export type PhaseMode = 'sequential' | 'parallel'
export type DryRunSeverity = 'info' | 'warning' | 'error'

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
