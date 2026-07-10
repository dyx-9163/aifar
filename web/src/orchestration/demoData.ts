import type { ActionPackage, OrchestrationTemplate, RunRecord } from './types'

export const actionPackages: ActionPackage[] = [
  {
    id: 'custom-os-precheck',
    name: 'custom-os-precheck',
    version: '1.0.0-aifar.1',
    titleKey: 'orchestration.demo.package.osPrecheck',
    source: 'user',
    status: 'published',
    qualityScore: 96,
    hash: 'sha256:8c2d4f1a9b7e',
    actions: [
      {
        id: 'packages.custom-os-precheck.check',
        labelKey: 'orchestration.demo.action.precheck',
        operation: 'check',
        targetMode: 'multiple',
        stages: ['validate-request', 'prepare-target', 'run-check', 'record-instance']
      }
    ],
    fields: [
      { name: 'servers', labelKey: 'orchestration.field.servers', type: 'server-list', componentKey: 'orchestration.component.serverSelector', required: true },
      { name: 'sshCredentialId', labelKey: 'orchestration.field.credential', type: 'credential', componentKey: 'orchestration.component.credentialSelector', required: true, secret: true }
    ],
    files: [
      { path: 'deployspec.yaml', hash: '3f9a1c2d' },
      { path: 'scripts/check.sh', hash: '91ab74d2' }
    ]
  },
  {
    id: 'docker-offline',
    name: 'docker-offline',
    version: '24.0.9-aifar.1',
    titleKey: 'orchestration.demo.package.docker',
    source: 'builtin',
    status: 'published',
    qualityScore: 100,
    hash: 'sha256:2012d9a68841',
    actions: [
      {
        id: 'packages.docker-offline.install',
        labelKey: 'orchestration.demo.action.dockerInstall',
        operation: 'install',
        targetMode: 'multiple',
        stages: ['load-context', 'verify-resource', 'upload-artifacts', 'install-runtime', 'post-check', 'record-instance']
      },
      {
        id: 'packages.docker-offline.uninstall',
        labelKey: 'orchestration.demo.action.dockerUninstall',
        operation: 'uninstall',
        targetMode: 'multiple',
        stages: ['load-context', 'validate-request', 'run-uninstall', 'post-check']
      }
    ],
    fields: [
      { name: 'servers', labelKey: 'orchestration.field.servers', type: 'server-list', componentKey: 'orchestration.component.serverSelector', required: true },
      { name: 'installRoot', labelKey: 'orchestration.field.installRoot', type: 'path', componentKey: 'orchestration.component.pathInput', required: true, defaultValue: '/aifar/apps/docker' }
    ],
    files: [
      { path: 'manifest.json', hash: '9fdc8a10' },
      { path: 'files/docker-24.0.9.tgz', hash: 'd1a78491' },
      { path: 'scripts/install.sh', hash: '469b207c' }
    ]
  },
  {
    id: 'mysql-innodb-cluster',
    name: 'mysql-innodb-cluster',
    version: '8.0.36-aifar.1',
    titleKey: 'orchestration.demo.package.mysql',
    source: 'builtin',
    status: 'published',
    qualityScore: 98,
    hash: 'sha256:ff18c06a8120',
    actions: [
      {
        id: 'packages.mysql-innodb-cluster.install',
        labelKey: 'orchestration.demo.action.mysqlInstall',
        operation: 'install',
        targetMode: 'multiple',
        rollback: true,
        stages: ['load-context', 'verify-resource', 'upload-artifacts', 'install-runtime', 'configure-topology', 'cluster-bootstrap', 'post-check', 'record-instance']
      },
      {
        id: 'packages.mysql-innodb-cluster.backup',
        labelKey: 'orchestration.demo.action.mysqlBackup',
        operation: 'backup',
        targetMode: 'multiple',
        stages: ['validate-cluster', 'select-backup-coordinator', 'run-backup', 'record-backup']
      }
    ],
    fields: [
      { name: 'servers', labelKey: 'orchestration.field.servers', type: 'server-list', componentKey: 'orchestration.component.serverSelector', required: true },
      { name: 'adminCredentialId', labelKey: 'orchestration.field.adminCredential', type: 'credential', componentKey: 'orchestration.component.credentialSelector', required: true, secret: true },
      { name: 'port', labelKey: 'orchestration.field.port', type: 'number', componentKey: 'orchestration.component.numberInput', required: true, defaultValue: 3306 },
      { name: 'backupBeforeRun', labelKey: 'orchestration.field.backupBeforeRun', type: 'switch', componentKey: 'orchestration.component.switch', defaultValue: true }
    ],
    files: [
      { path: 'manifest.json', hash: '2ae5410e' },
      { path: 'files/mysql-8.0.36.tar.gz', hash: '88f3b9e2' },
      { path: 'scripts/install.sh', hash: '584ac891' },
      { path: 'scripts/bootstrap.sh', hash: '6ae3d410' }
    ]
  },
  {
    id: 'nacos-config',
    name: 'nacos-config',
    version: '2.4.3-aifar.1',
    titleKey: 'orchestration.demo.package.nacosConfig',
    source: 'builtin',
    status: 'reviewing',
    qualityScore: 82,
    hash: 'sha256:0dbb95cc21a4',
    actions: [
      {
        id: 'packages.nacos-config.publish',
        labelKey: 'orchestration.demo.action.nacosPublish',
        operation: 'publish',
        targetMode: 'single',
        stages: ['validate-request', 'render-config', 'publish-config', 'record-instance']
      }
    ],
    fields: [
      { name: 'nacosInstance', labelKey: 'orchestration.field.nacosInstance', type: 'select', componentKey: 'orchestration.component.select', required: true },
      { name: 'credentialId', labelKey: 'orchestration.field.credential', type: 'credential', componentKey: 'orchestration.component.credentialSelector', required: true, secret: true }
    ],
    files: [
      { path: 'deployspec.yaml', hash: '728d00b1' },
      { path: 'templates/application.yaml.tmpl', hash: '80be5512' }
    ]
  }
]

export const orchestrationTemplates: OrchestrationTemplate[] = [
  {
    id: 'aifar-full-stack-lite',
    titleKey: 'orchestration.demo.template.fullStack',
    version: 1,
    phases: [
      {
        id: 'prepare',
        titleKey: 'orchestration.demo.phase.prepare',
        mode: 'sequential',
        steps: [
          {
            id: 'os-precheck',
            labelKey: 'orchestration.demo.step.osPrecheck',
            actionId: 'packages.custom-os-precheck.check',
            packageId: 'custom-os-precheck',
            packageVersion: '1.0.0-aifar.1',
            targetsKey: 'orchestration.demo.target.allServers',
            failurePolicy: 'stop'
          },
          {
            id: 'docker-install',
            labelKey: 'orchestration.demo.step.dockerInstall',
            actionId: 'packages.docker-offline.install',
            packageId: 'docker-offline',
            packageVersion: '24.0.9-aifar.1',
            targetsKey: 'orchestration.demo.target.allServers',
            failurePolicy: 'stop'
          }
        ]
      },
      {
        id: 'data',
        titleKey: 'orchestration.demo.phase.data',
        mode: 'parallel',
        steps: [
          {
            id: 'mysql-cluster',
            labelKey: 'orchestration.demo.step.mysqlCluster',
            actionId: 'packages.mysql-innodb-cluster.install',
            packageId: 'mysql-innodb-cluster',
            packageVersion: '8.0.36-aifar.1',
            targetsKey: 'orchestration.demo.target.dbServers',
            failurePolicy: 'rollback'
          }
        ]
      }
    ]
  }
]

export const runRecords: RunRecord[] = [
  {
    id: 'task_orch_20260709_001',
    templateKey: 'orchestration.demo.template.fullStack',
    version: 1,
    status: 'success',
    actor: 'admin',
    targetCount: 3,
    stepCount: 3,
    startedAt: '2026-07-09 10:12:30'
  },
  {
    id: 'task_orch_20260709_002',
    templateKey: 'orchestration.demo.template.fullStack',
    version: 1,
    status: 'running',
    actor: 'operator',
    targetCount: 3,
    stepCount: 2,
    startedAt: '2026-07-09 11:05:18'
  }
]
