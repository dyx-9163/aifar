package orchestration

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"aifar-deployment/backend/internal/adapter"
	"aifar-deployment/backend/internal/store"
)

func TestUploadAndDryRunCheckPackage(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "aifar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server, err := db.SaveServer(store.Server{Name: "node-1", Host: "10.0.0.11", Username: "root"})
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(db, filepath.Join(t.TempDir(), "packages"), nil)
	pkg, err := svc.UploadAndPublish(t.Context(), "readonly-check.zip", zipPackage(t, map[string]string{
		"deployspec.json": `{
			"name":"readonly-check",
			"version":"1.0.0",
			"title":{"zh":"只读检查","en":"Readonly Check"},
			"actions":[{"name":"check","operation":"check","script":"scripts/check.sh","readOnly":true,"targetMode":"multiple"}]
		}`,
		"scripts/":         "",
		"scripts/check.sh": "#!/usr/bin/env sh\necho readonly-ok\n",
	}), "tester")
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Status != "published" || len(pkg.Actions) != 1 || pkg.Actions[0].Operation != "check" {
		t.Fatalf("unexpected published package: %+v", pkg)
	}
	list, err := svc.ListActions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].PackageName != "readonly-check" {
		t.Fatalf("unexpected action list: %+v", list)
	}
	plan, err := svc.DryRun(t.Context(), RunRequest{ActionID: pkg.Actions[0].ID, ServerIDs: []string{server.ID}}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Targets) != 1 || len(plan.Steps) != 5 || plan.Steps[3].Name != "run-check" {
		t.Fatalf("unexpected dry-run plan: %+v", plan)
	}
}

func TestUploadRejectsZipPathTraversal(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "aifar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewService(db, filepath.Join(t.TempDir(), "packages"), nil)
	_, err = svc.UploadAndPublish(t.Context(), "bad.zip", zipPackage(t, map[string]string{
		"deployspec.json":  `{"name":"bad-package","version":"1.0.0","actions":[{"name":"check","operation":"check","script":"scripts/check.sh","readOnly":true}]}`,
		"../escape.sh":     "echo bad\n",
		"scripts/check.sh": "echo ok\n",
	}), "tester")
	if err == nil || !strings.Contains(err.Error(), "safe relative path") {
		t.Fatalf("expected path traversal to be rejected, got %v", err)
	}
}

func TestUploadCanonicalizesWindowsShellScript(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "aifar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := NewService(db, filepath.Join(t.TempDir(), "packages"), nil)
	pkg, err := svc.UploadAndPublish(t.Context(), "windows-check.zip", zipPackage(t, map[string]string{
		"deployspec.json":  `{"name":"windows-check","version":"1.0.0","actions":[{"name":"check","operation":"check","script":"scripts/check.sh","readOnly":true}]}`,
		"scripts/check.sh": "\xef\xbb\xbf#!/bin/sh\r\nset -eu\r\necho ok\r\n",
	}), "tester")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Actions) != 1 {
		t.Fatalf("unexpected actions: %+v", pkg.Actions)
	}
	item, err := db.GetPublishedOrchestrationAction(pkg.Actions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(item.Package.PackageDir, filepath.FromSlash(item.Action.ScriptPath))
	data, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	want := "#!/bin/sh\nset -eu\necho ok\n"
	if string(data) != want {
		t.Fatalf("script was not canonicalized: %q", string(data))
	}
	hash, err := fileSHA256(scriptPath)
	if err != nil {
		t.Fatal(err)
	}
	if hash != pkg.Actions[0].ScriptSHA256 {
		t.Fatalf("script hash mismatch: package=%s file=%s", pkg.Actions[0].ScriptSHA256, hash)
	}
}

func TestRunMultiStepInstallAction(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "aifar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	server, err := db.SaveServer(store.Server{Name: "node-1", Host: "10.0.0.11", Username: "root", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	remote := &recordingRemote{}
	svc := NewService(db, filepath.Join(t.TempDir(), "packages"), remote)
	pkg, err := svc.UploadAndPublish(t.Context(), "mysql-install.zip", zipPackage(t, map[string]string{
		"deployspec.json": `{
			"name":"mysql-offline-install",
			"version":"1.0.0",
			"actions":[{
				"name":"install-mysql",
				"operation":"install",
				"readOnly":false,
				"targetMode":"single",
				"steps":[
					{"name":"check-connection","script":"scripts/01-check-connection.sh","readOnly":true},
					{"name":"check-system","script":"scripts/02-check-system.sh","readOnly":true},
					{"name":"open-firewall","script":"scripts/03-open-firewall.sh","readOnly":false},
					{"name":"install-mysql","script":"scripts/04-install-mysql.sh","readOnly":false,"timeoutSeconds":1800}
				]
			}]
		}`,
		"scripts/01-check-connection.sh": "#!/bin/sh\nset -eu\necho connected\n",
		"scripts/02-check-system.sh":     "#!/bin/sh\nset -eu\necho system-ok\n",
		"scripts/03-open-firewall.sh":    "#!/bin/sh\nset -eu\necho firewall-open\n",
		"scripts/04-install-mysql.sh":    "#!/bin/sh\nset -eu\necho mysql-installed\n",
	}), "tester")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Actions) != 1 || pkg.Actions[0].ReadOnly || len(pkg.Actions[0].Steps) != 4 {
		t.Fatalf("unexpected install action: %+v", pkg.Actions)
	}
	plan, err := svc.DryRun(t.Context(), RunRequest{ActionID: pkg.Actions[0].ID, ServerIDs: []string{server.ID}}, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 8 || plan.Steps[3].Name != "check-connection" || plan.Steps[6].Name != "install-mysql" || plan.Steps[7].Name != "record-result" {
		t.Fatalf("unexpected multi-step plan: %+v", plan.Steps)
	}
	log := &recordingRunLogger{taskID: "tsk-test"}
	if err := svc.RunAction(t.Context(), RunRequest{ActionID: pkg.Actions[0].ID, ServerIDs: []string{server.ID}}, "en", log); err != nil {
		t.Fatal(err)
	}
	if len(remote.commands) != 5 {
		t.Fatalf("expected mkdir plus four step commands, got %d: %+v", len(remote.commands), remote.commands)
	}
	for _, stepName := range []string{"check-connection", "check-system", "open-firewall", "install-mysql"} {
		found := false
		for _, command := range remote.commands {
			if strings.Contains(command, "AIFAR_STEP_NAME=") && strings.Contains(command, stepName) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("step %s was not executed: %+v", stepName, remote.commands)
		}
	}
	for _, upload := range remote.uploads {
		if strings.HasSuffix(upload.remotePath, ".sh") && upload.mode.Perm() != 0o755 {
			t.Fatalf("script upload mode is not executable: %+v", upload)
		}
	}
	if strings.Join(log.startedSteps, ",") != "load-server,verify-package,upload-package,check-connection,check-system,open-firewall,install-mysql,record-result" {
		t.Fatalf("unexpected started steps: %+v", log.startedSteps)
	}
}

func TestRunThreeNodeClusterActionHonorsBarrierAndRecordsResult(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "aifar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	servers := make([]store.Server, 0, 3)
	for index, host := range []string{"10.0.0.11", "10.0.0.12", "10.0.0.13"} {
		server, saveErr := db.SaveServer(store.Server{
			Name:     "mysql-node-" + string(rune('1'+index)),
			Host:     host,
			Username: "root",
			Password: "secret",
		})
		if saveErr != nil {
			t.Fatal(saveErr)
		}
		servers = append(servers, server)
	}

	remote := &recordingRemote{}
	svc := NewService(db, filepath.Join(t.TempDir(), "packages"), remote)
	pkg, err := svc.UploadAndPublish(t.Context(), "mysql-cluster.zip", zipPackage(t, map[string]string{
		"deployspec.json": `{
			"apiVersion":"aifar.io/v1alpha2",
			"kind":"ActionPackage",
			"name":"mysql-innodb-cluster",
			"version":"1.0.0",
			"actions":[{
				"name":"install-cluster",
				"operation":"install",
				"targetMode":"cluster",
				"topology":"mysql-innodb-cluster",
				"minTargets":3,
				"maxTargets":3,
				"readOnly":false,
				"secrets":[{
					"name":"mysql-admin-password",
					"env":"AIFAR_MYSQL_ADMIN_PASSWORD",
					"generator":"hex",
					"bytes":24
				}],
				"credential":{
					"kind":"database",
					"username":"aifar_cluster_admin",
					"purpose":"cluster-admin",
					"secretName":"mysql-admin-password",
					"secretKey":"password"
				},
				"result":{"app":"mysql","version":"8.0.36","status":"installed","port":3306},
				"phases":[
					{"name":"prepare-nodes","scope":"node","mode":"parallel","steps":[
						{"name":"check-node","script":"scripts/check-node.sh","readOnly":true},
						{"name":"install-node","script":"scripts/install-node.sh","readOnly":false}
					]},
					{"name":"nodes-ready","scope":"barrier"},
					{"name":"bootstrap-cluster","scope":"cluster","runOn":"coordinator","steps":[
						{"name":"bootstrap","script":"scripts/bootstrap.sh","readOnly":false}
					]},
					{"name":"verify-cluster","scope":"cluster","runOn":"coordinator","steps":[
						{"name":"verify","script":"scripts/verify.sh","readOnly":true}
					]}
				]
			}]
		}`,
		"scripts/check-node.sh":   "#!/bin/sh\nset -eu\necho check-node\n",
		"scripts/install-node.sh": "#!/bin/sh\nset -eu\necho install-node\n",
		"scripts/bootstrap.sh":    "#!/bin/sh\nset -eu\necho bootstrap\n",
		"scripts/verify.sh":       "#!/bin/sh\nset -eu\necho verify\n",
	}), "tester")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Actions) != 1 || pkg.Actions[0].TargetMode != "cluster" || len(pkg.Actions[0].Phases) != 4 {
		t.Fatalf("unexpected cluster action: %+v", pkg.Actions)
	}

	req := RunRequest{
		ActionID:            pkg.Actions[0].ID,
		ServerIDs:           []string{servers[0].ID, servers[1].ID, servers[2].ID},
		CoordinatorServerID: servers[1].ID,
		ClusterName:         "aifar_mysql_prod",
	}
	plan, err := svc.DryRun(t.Context(), req, "en")
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Targets) != 3 || plan.Targets[1].Role != "coordinator" || !plan.Targets[1].Coordinator {
		t.Fatalf("unexpected cluster target roles: %+v", plan.Targets)
	}
	if !hasPlanStep(plan.Steps, servers[1].ID, "nodes-ready", "barrier") || !hasPlanStep(plan.Steps, servers[1].ID, "bootstrap", "cluster") {
		t.Fatalf("cluster phases are missing from dry-run: %+v", plan.Steps)
	}

	log := &recordingRunLogger{taskID: "tsk-cluster"}
	if err := svc.RunAction(t.Context(), req, "en", log); err != nil {
		t.Fatal(err)
	}

	commands, commandServers := remote.commandSnapshot()
	lastInstall := -1
	bootstrapIndex := -1
	verifyIndex := -1
	for index, command := range commands {
		switch {
		case strings.Contains(command, "AIFAR_STEP_NAME='install-node'"):
			lastInstall = index
		case strings.Contains(command, "AIFAR_STEP_NAME='bootstrap'"):
			bootstrapIndex = index
			if commandServers[index] != servers[1].ID {
				t.Fatalf("bootstrap ran on %s, want coordinator %s", commandServers[index], servers[1].ID)
			}
		case strings.Contains(command, "AIFAR_STEP_NAME='verify'"):
			verifyIndex = index
		}
	}
	if lastInstall < 0 || bootstrapIndex <= lastInstall || verifyIndex <= bootstrapIndex {
		t.Fatalf("cluster barrier/order was not honored: commands=%+v", commands)
	}

	uploads := remote.uploadSnapshot()
	secretUploads := 0
	for _, upload := range uploads {
		if strings.HasSuffix(upload.remotePath, "/context/secrets.env") {
			secretUploads++
			if upload.mode.Perm() != 0o600 || !strings.Contains(upload.content, "AIFAR_MYSQL_ADMIN_PASSWORD=") {
				t.Fatalf("invalid secret context upload: %+v", upload)
			}
		}
	}
	if secretUploads != 3 {
		t.Fatalf("expected one secret context per node, got %d", secretUploads)
	}

	clusters, err := db.ListOrchestrationClusters()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 || clusters[0].Status != "healthy" || clusters[0].CoordinatorServerID != servers[1].ID || len(clusters[0].Nodes) != 3 {
		t.Fatalf("unexpected persisted cluster: %+v", clusters)
	}
	instances, err := db.ListAppInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 3 {
		t.Fatalf("expected three recorded MySQL instances, got %+v", instances)
	}
	credentials, err := db.ListCredentials(store.CredentialQuery{Kind: "database"})
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 1 || !credentials[0].HasSecret {
		allCredentials, _ := db.ListCredentials(store.CredentialQuery{})
		credentialRows, _ := db.CountRows("credentials")
		storedAction, _ := db.GetPublishedOrchestrationAction(pkg.Actions[0].ID)
		t.Fatalf("expected one encrypted cluster credential, got filtered=%+v all=%+v rows=%d workflow=%s", credentials, allCredentials, credentialRows, storedAction.Action.WorkflowJSON)
	}
	firstCredential, err := db.GetCredential(credentials[0].ID, true)
	if err != nil {
		t.Fatal(err)
	}
	firstPassword := firstCredential.Secret["password"]
	if firstPassword == "" {
		t.Fatal("stored cluster credential has no password")
	}

	retryLog := &recordingRunLogger{taskID: "tsk-cluster-retry"}
	if err := svc.RunAction(t.Context(), req, "en", retryLog); err != nil {
		t.Fatal(err)
	}
	clusters, err = db.ListOrchestrationClusters()
	if err != nil {
		t.Fatal(err)
	}
	credentials, err = db.ListCredentials(store.CredentialQuery{Kind: "database"})
	if err != nil {
		t.Fatal(err)
	}
	instances, err = db.ListAppInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(clusters) != 1 || clusters[0].TaskID != "tsk-cluster-retry" {
		t.Fatalf("cluster retry must reuse the control-plane record: %+v", clusters)
	}
	if len(credentials) != 1 || credentials[0].CurrentVersion != 1 {
		t.Fatalf("cluster retry must not rotate or duplicate the credential: %+v", credentials)
	}
	retriedCredential, err := db.GetCredential(credentials[0].ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if retriedCredential.Secret["password"] != firstPassword {
		t.Fatal("cluster retry changed the managed password")
	}
	if len(instances) != 3 {
		t.Fatalf("cluster retry must reuse the three app instances, got %+v", instances)
	}
}

func TestClusterActionRequiresExactTargetsAndSelectedCoordinator(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "aifar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	servers := make([]store.Server, 0, 3)
	for index, host := range []string{"10.0.0.21", "10.0.0.22", "10.0.0.23"} {
		server, saveErr := db.SaveServer(store.Server{Name: host, Host: host, Username: "root", Password: "secret", SortOrder: index})
		if saveErr != nil {
			t.Fatal(saveErr)
		}
		servers = append(servers, server)
	}
	svc := NewService(db, filepath.Join(t.TempDir(), "packages"), &recordingRemote{})
	pkg, err := svc.UploadAndPublish(t.Context(), "cluster.zip", zipPackage(t, map[string]string{
		"deployspec.json":      `{"name":"three-node-cluster","version":"1.0.0","actions":[{"name":"install","operation":"install","targetMode":"cluster","topology":"test-cluster","minTargets":3,"maxTargets":3,"readOnly":false,"phases":[{"name":"prepare","scope":"node","steps":[{"name":"install-node","script":"scripts/install.sh","readOnly":false}]},{"name":"bootstrap","scope":"cluster","runOn":"coordinator","steps":[{"name":"bootstrap-cluster","script":"scripts/bootstrap.sh","readOnly":false}]}]}]}`,
		"scripts/install.sh":   "#!/bin/sh\nexit 0\n",
		"scripts/bootstrap.sh": "#!/bin/sh\nexit 0\n",
	}), "tester")
	if err != nil {
		t.Fatal(err)
	}
	actionID := pkg.Actions[0].ID
	_, err = svc.DryRun(t.Context(), RunRequest{ActionID: actionID, ServerIDs: []string{servers[0].ID, servers[1].ID}, CoordinatorServerID: servers[0].ID}, "en")
	if err == nil || !strings.Contains(err.Error(), "exactly 3") {
		t.Fatalf("expected exact-target validation, got %v", err)
	}
	_, err = svc.DryRun(t.Context(), RunRequest{ActionID: actionID, ServerIDs: []string{servers[0].ID, servers[1].ID, servers[2].ID}, CoordinatorServerID: "srv-missing"}, "en")
	if err == nil || !strings.Contains(err.Error(), "coordinator") {
		t.Fatalf("expected coordinator validation, got %v", err)
	}
}

func hasPlanStep(steps []PlanStep, target, name, scope string) bool {
	for _, step := range steps {
		if step.Target == target && step.Name == name && step.Scope == scope {
			return true
		}
	}
	return false
}

type recordedUpload struct {
	serverID   string
	remotePath string
	mode       os.FileMode
	content    string
}

type recordingRemote struct {
	mu             sync.Mutex
	commands       []string
	commandServers []string
	uploads        []recordedUpload
}

func (r *recordingRemote) Run(_ context.Context, server store.Server, command string) (adapter.CommandResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, command)
	r.commandServers = append(r.commandServers, server.ID)
	return adapter.CommandResult{ExitCode: 0}, nil
}

func (r *recordingRemote) UploadFile(_ context.Context, server store.Server, localPath, remotePath string, mode os.FileMode) error {
	content, _ := os.ReadFile(localPath)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.uploads = append(r.uploads, recordedUpload{serverID: server.ID, remotePath: remotePath, mode: mode, content: string(content)})
	return nil
}

func (r *recordingRemote) commandSnapshot() ([]string, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.commands...), append([]string(nil), r.commandServers...)
}

func (r *recordingRemote) uploadSnapshot() []recordedUpload {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedUpload(nil), r.uploads...)
}

type recordingRunLogger struct {
	mu           sync.Mutex
	taskID       string
	startedSteps []string
}

func (*recordingRunLogger) Info(string, ...any)                  {}
func (*recordingRunLogger) Error(string, ...any)                 {}
func (l *recordingRunLogger) TaskID() string                     { return l.taskID }
func (l *recordingRunLogger) TargetLogger(string) Logger         { return l }
func (*recordingRunLogger) PlanTarget(string)                    {}
func (*recordingRunLogger) StartTarget(string)                   {}
func (*recordingRunLogger) FinishTarget(string, string, string)  {}
func (*recordingRunLogger) PlanStep(string, string, string, int) {}
func (l *recordingRunLogger) StartStep(_ string, name, _ string, _ int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.startedSteps = append(l.startedSteps, name)
}
func (*recordingRunLogger) FinishStep(string, string, string, string) {}

func zipPackage(t *testing.T, entries map[string]string) *bytes.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes())
}
