package orchestration

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"aifar-deployment/backend/internal/i18n"
	"aifar-deployment/backend/internal/installer/installerkit"
	"aifar-deployment/backend/internal/store"
	"aifar-deployment/backend/internal/taskrun"
)

type generatedSecret struct {
	Spec  GeneratedSecretSpec
	Value string
}

type clusterRuntime struct {
	Cluster    store.OrchestrationCluster
	Workflow   ActionWorkflow
	Secrets    map[string]generatedSecret
	Credential store.Credential
	Servers    []store.Server
	WorkDirs   map[string]string
}

func clusterActionPlanSteps(action store.OrchestrationAction, servers []store.Server, coordinatorID, lang string) []PlanStep {
	steps := make([]PlanStep, 0)
	for _, server := range servers {
		steps = append(steps,
			PlanStep{Target: server.ID, Name: "load-server", Title: i18n.Text(lang, "orchestration.stepLoadServer"), Order: 1, Phase: "prepare-runtime", Scope: "node", Mode: "parallel"},
			PlanStep{Target: server.ID, Name: "verify-package", Title: i18n.Text(lang, "orchestration.stepVerifyPackage"), Order: 2, Phase: "prepare-runtime", Scope: "node", Mode: "parallel"},
			PlanStep{Target: server.ID, Name: "upload-package", Title: i18n.Text(lang, "orchestration.stepUploadPackage"), Order: 3, Phase: "prepare-runtime", Scope: "node", Mode: "parallel"},
		)
	}
	workflow := storedActionWorkflow(action)
	stepByName := storedStepMap(action)
	order := 4
	for _, phase := range workflow.Phases {
		phaseTitle := phase.Title.withFallback(phase.Name)[string(i18n.Resolve(lang))]
		switch phase.Scope {
		case "node":
			for _, phaseStep := range phase.Steps {
				step := stepByName[phaseStep.Name]
				title := localizedTitle(step.TitleJSON, lang, step.Name)
				for _, server := range servers {
					steps = append(steps, PlanStep{Target: server.ID, Name: step.Name, Title: title, Order: order, Phase: phase.Name, Scope: phase.Scope, Mode: phase.Mode})
				}
				order++
			}
		case "barrier":
			steps = append(steps, PlanStep{Target: coordinatorID, Name: phase.Name, Title: phaseTitle, Order: order, Phase: phase.Name, Scope: phase.Scope, Mode: phase.Mode})
			order++
		case "cluster":
			for _, phaseStep := range phase.Steps {
				step := stepByName[phaseStep.Name]
				steps = append(steps, PlanStep{Target: coordinatorID, Name: step.Name, Title: localizedTitle(step.TitleJSON, lang, step.Name), Order: order, Phase: phase.Name, Scope: phase.Scope, Mode: phase.Mode})
				order++
			}
		}
	}
	for _, server := range servers {
		steps = append(steps, PlanStep{Target: server.ID, Name: "record-result", Title: i18n.Text(lang, "orchestration.stepRecordResult"), Order: order, Phase: "record-result", Scope: "node", Mode: "sequential"})
	}
	return steps
}

func (s *Service) runClusterAction(ctx context.Context, req RunRequest, item store.OrchestrationActionWithPackage, servers []store.Server, lang string, log RunLogger) (runErr error) {
	workflow := storedActionWorkflow(item.Action)
	if len(workflow.Phases) == 0 {
		return errors.New("cluster action workflow is missing")
	}
	runtime, err := s.newClusterRuntime(req, item, servers, log.TaskID(), workflow)
	if err != nil {
		return err
	}
	clusterSaved := false
	completed := false
	defer func() {
		if runErr == nil || completed || !clusterSaved {
			return
		}
		runtime.Cluster.Status = "failed"
		for index := range runtime.Cluster.Nodes {
			if runtime.Cluster.Nodes[index].Status != "healthy" {
				runtime.Cluster.Nodes[index].Status = "failed"
			}
		}
		_, _ = s.store.SaveOrchestrationCluster(runtime.Cluster)
		if runtime.Credential.ID != "" {
			runtime.Credential.Status = "invalid"
			_, _ = s.store.SaveCredential(runtime.Credential)
		}
	}()

	if runtime.Credential.Name != "" {
		if runtime.Credential.ID != "" {
			runtime.Credential.Secret = nil
		}
		saved, saveErr := s.store.SaveCredential(runtime.Credential)
		if saveErr != nil {
			return saveErr
		}
		runtime.Credential = saved
		runtime.Cluster.CredentialID = saved.ID
	}
	savedCluster, err := s.store.SaveOrchestrationCluster(runtime.Cluster)
	if err != nil {
		return err
	}
	runtime.Cluster = savedCluster
	clusterSaved = true

	stepOrders := clusterPlanOrderMap(clusterActionPlanSteps(item.Action, servers, req.CoordinatorServerID, lang))
	for _, server := range servers {
		log.StartTarget(server.ID)
	}
	serverByID := make(map[string]store.Server, len(servers))
	for _, server := range servers {
		serverByID[server.ID] = server
	}

	targetIDs := make([]string, 0, len(servers))
	for _, server := range servers {
		targetIDs = append(targetIDs, server.ID)
	}
	prepareFailures := taskrun.RunTargets(ctx, targetIDs, len(targetIDs), func(serverID string) error {
		return s.prepareClusterTarget(ctx, item, runtime, serverByID[serverID], lang, log, stepOrders)
	})
	if len(prepareFailures) > 0 {
		err := clusterFailuresError("cluster target preparation failed", prepareFailures)
		finishClusterTargets(log, servers, "failed", err.Error())
		return err
	}

	stepByName := storedStepMap(item.Action)
	coordinator := serverByID[req.CoordinatorServerID]
	for _, phase := range workflow.Phases {
		if err := ctx.Err(); err != nil {
			finishClusterTargets(log, servers, "failed", err.Error())
			return err
		}
		switch phase.Scope {
		case "node":
			concurrency := 1
			if phase.Mode == "parallel" {
				concurrency = len(targetIDs)
			}
			failures := taskrun.RunTargets(ctx, targetIDs, concurrency, func(serverID string) error {
				server := serverByID[serverID]
				for _, phaseStep := range phase.Steps {
					step := stepByName[phaseStep.Name]
					if err := s.runClusterStep(ctx, item, runtime, server, phase.Name, step, lang, log, stepOrders); err != nil {
						return err
					}
				}
				return nil
			})
			if len(failures) > 0 {
				err := clusterFailuresError("cluster node phase "+phase.Name+" failed", failures)
				finishClusterTargets(log, servers, "failed", err.Error())
				return err
			}
		case "barrier":
			title := phase.Title.withFallback(phase.Name)[string(i18n.Resolve(lang))]
			order := stepOrders[clusterPlanKey(coordinator.ID, phase.Name)]
			log.StartStep(coordinator.ID, phase.Name, title, order)
			log.TargetLogger(coordinator.ID).Info(i18n.Text(lang, "orchestration.clusterBarrierPassed"), phase.Name)
			log.FinishStep(coordinator.ID, phase.Name, "success", "")
		case "cluster":
			for _, phaseStep := range phase.Steps {
				step := stepByName[phaseStep.Name]
				if err := s.runClusterStep(ctx, item, runtime, coordinator, phase.Name, step, lang, log, stepOrders); err != nil {
					finishClusterTargets(log, servers, "failed", err.Error())
					return err
				}
			}
		default:
			return fmt.Errorf("unsupported cluster phase scope %q", phase.Scope)
		}
	}

	if err := s.recordClusterResult(runtime, item, lang, log, stepOrders); err != nil {
		finishClusterTargets(log, servers, "failed", err.Error())
		return err
	}
	runtime.Cluster.Status = "healthy"
	for index := range runtime.Cluster.Nodes {
		runtime.Cluster.Nodes[index].Status = "healthy"
	}
	if _, err := s.store.SaveOrchestrationCluster(runtime.Cluster); err != nil {
		finishClusterTargets(log, servers, "failed", err.Error())
		return err
	}
	for _, server := range servers {
		log.FinishTarget(server.ID, "success", "")
	}
	completed = true
	log.Info(i18n.Text(lang, "orchestration.actionCompleted"), item.Action.Operation, len(servers))
	return nil
}

func (s *Service) newClusterRuntime(req RunRequest, item store.OrchestrationActionWithPackage, servers []store.Server, taskID string, workflow ActionWorkflow) (clusterRuntime, error) {
	clusterName := strings.TrimSpace(req.ClusterName)
	if clusterName == "" {
		clusterName = defaultClusterName(item.Package.Name, taskID)
	}
	existing, findErr := s.store.FindOrchestrationCluster(item.Action.ID, clusterName)
	if findErr != nil && !errors.Is(findErr, sql.ErrNoRows) {
		return clusterRuntime{}, findErr
	}
	hasExisting := findErr == nil
	if hasExisting {
		if existing.CoordinatorServerID != req.CoordinatorServerID || !sameClusterTargets(existing.Nodes, servers) {
			return clusterRuntime{}, errors.New("an existing cluster with this name uses a different coordinator or target set")
		}
	}

	secrets := make(map[string]generatedSecret, len(workflow.Secrets))
	for _, spec := range workflow.Secrets {
		value, err := generateHexSecret(spec.Bytes)
		if err != nil {
			return clusterRuntime{}, err
		}
		secrets[spec.Name] = generatedSecret{Spec: spec, Value: value}
	}
	resultPort := 0
	if workflow.Result != nil {
		resultPort = workflow.Result.Port
	}
	cluster := store.OrchestrationCluster{
		ID:                  store.NewID("orchcluster"),
		ActionID:            item.Action.ID,
		PackageName:         item.Package.Name,
		PackageVersion:      item.Package.Version,
		Name:                clusterName,
		Topology:            item.Action.Topology,
		Status:              "preparing",
		CoordinatorServerID: req.CoordinatorServerID,
		TaskID:              taskID,
		MetadataJSON:        mustJSON(map[string]any{"targetCount": len(servers)}),
		Nodes:               make([]store.OrchestrationClusterNode, 0, len(servers)),
	}
	if hasExisting {
		cluster.ID = existing.ID
		cluster.CredentialID = existing.CredentialID
		cluster.CreatedAt = existing.CreatedAt
	}
	existingNodes := map[string]store.OrchestrationClusterNode{}
	for _, node := range existing.Nodes {
		existingNodes[node.ServerID] = node
	}
	for index, server := range servers {
		role := "member"
		if server.ID == req.CoordinatorServerID {
			role = "coordinator"
		}
		endpoint := strings.TrimSpace(server.Host)
		if resultPort > 0 {
			endpoint = fmt.Sprintf("%s:%d", endpoint, resultPort)
		}
		node := store.OrchestrationClusterNode{
			ServerID: server.ID,
			Role:     role,
			Endpoint: endpoint,
			Status:   "preparing",
			Order:    index + 1,
		}
		if previous, ok := existingNodes[server.ID]; ok {
			node.ID = previous.ID
			node.CreatedAt = previous.CreatedAt
		}
		cluster.Nodes = append(cluster.Nodes, node)
	}
	runtime := clusterRuntime{Cluster: cluster, Workflow: workflow, Secrets: secrets, Servers: servers, WorkDirs: map[string]string{}}
	for _, server := range servers {
		runtime.WorkDirs[server.ID] = remoteWorkDir(server.DeployDir, item.Package.Name, item.Package.Version, taskID)
	}
	if workflow.Credential != nil {
		secret := secrets[workflow.Credential.SecretName]
		app := ""
		port := 0
		if workflow.Result != nil {
			app = workflow.Result.App
			port = workflow.Result.Port
		}
		endpoint := coordinatorHost(servers, req.CoordinatorServerID)
		if port > 0 {
			endpoint = fmt.Sprintf("%s:%d", endpoint, port)
		}
		runtime.Credential = store.Credential{
			Name:      clusterName + " " + workflow.Credential.Purpose,
			Kind:      workflow.Credential.Kind,
			Username:  workflow.Credential.Username,
			Endpoint:  endpoint,
			Scope:     "app",
			Status:    "active",
			App:       app,
			Purpose:   workflow.Credential.Purpose,
			Tags:      "orchestration-cluster," + cluster.ID,
			Secret:    map[string]string{workflow.Credential.SecretKey: secret.Value},
			CreatedBy: "task:" + taskID,
		}
		if hasExisting {
			if existing.CredentialID == "" {
				return clusterRuntime{}, errors.New("existing cluster has no managed credential")
			}
			credential, err := s.store.GetCredential(existing.CredentialID, true)
			if err != nil {
				return clusterRuntime{}, err
			}
			value := credential.Secret[workflow.Credential.SecretKey]
			if value == "" {
				return clusterRuntime{}, errors.New("existing cluster credential has no managed secret")
			}
			secret.Value = value
			secrets[workflow.Credential.SecretName] = secret
			credential.Status = "active"
			credential.Secret = nil
			runtime.Credential = credential
		}
	}
	return runtime, nil
}

func (s *Service) prepareClusterTarget(ctx context.Context, item store.OrchestrationActionWithPackage, runtime clusterRuntime, server store.Server, lang string, log RunLogger, orders map[string]int) error {
	targetLog := log.TargetLogger(server.ID)
	fail := func(step string, err error) error {
		log.FinishStep(server.ID, step, "failed", err.Error())
		return err
	}

	log.StartStep(server.ID, "load-server", i18n.Text(lang, "orchestration.stepLoadServer"), orders[clusterPlanKey(server.ID, "load-server")])
	if strings.TrimSpace(server.Password) == "" && strings.TrimSpace(server.PrivateKey) == "" {
		return fail("load-server", errors.New(i18n.Text(lang, "servers.credentialMissing")))
	}
	log.FinishStep(server.ID, "load-server", "success", "")

	log.StartStep(server.ID, "verify-package", i18n.Text(lang, "orchestration.stepVerifyPackage"), orders[clusterPlanKey(server.ID, "verify-package")])
	for _, step := range storedActionSteps(item.Action) {
		scriptPath := filepath.Join(item.Package.PackageDir, filepath.FromSlash(step.ScriptPath))
		gotSHA, err := fileSHA256(scriptPath)
		if err != nil {
			return fail("verify-package", err)
		}
		if !strings.EqualFold(gotSHA, step.ScriptSHA256) {
			return fail("verify-package", fmt.Errorf("script %s sha256 mismatch: expected %s, got %s", step.ScriptPath, step.ScriptSHA256, gotSHA))
		}
	}
	log.FinishStep(server.ID, "verify-package", "success", "")

	workDir := runtime.WorkDirs[server.ID]
	log.StartStep(server.ID, "upload-package", i18n.Text(lang, "orchestration.stepUploadPackage"), orders[clusterPlanKey(server.ID, "upload-package")])
	targetLog.Info(i18n.Text(lang, "orchestration.prepareWorkDir"), workDir)
	if _, err := installerkit.Run(ctx, s.remote, server, "mkdir -p "+installerkit.ShellQuote(workDir), targetLog, i18n.Text(lang, "orchestration.remoteCommandFailed")); err != nil {
		return fail("upload-package", err)
	}
	executablePaths := map[string]bool{}
	for _, step := range storedActionSteps(item.Action) {
		executablePaths[step.ScriptPath] = true
	}
	if err := s.uploadPackageFiles(ctx, item.Package.PackageDir, workDir, executablePaths, server, lang, targetLog); err != nil {
		return fail("upload-package", err)
	}
	if err := s.uploadClusterContext(ctx, runtime, server); err != nil {
		return fail("upload-package", err)
	}
	log.FinishStep(server.ID, "upload-package", "success", "")
	return nil
}

func (s *Service) runClusterStep(ctx context.Context, item store.OrchestrationActionWithPackage, runtime clusterRuntime, server store.Server, phaseName string, step store.OrchestrationActionStep, lang string, log RunLogger, orders map[string]int) error {
	title := localizedTitle(step.TitleJSON, lang, step.Name)
	log.StartStep(server.ID, step.Name, title, orders[clusterPlanKey(server.ID, step.Name)])
	workDir := runtime.WorkDirs[server.ID]
	contextDir := path.Join(workDir, "context")
	remoteScript := path.Join(workDir, step.ScriptPath)
	cmd := "export AIFAR_WORK_DIR=" + installerkit.ShellQuote(workDir) +
		" AIFAR_CONTEXT_DIR=" + installerkit.ShellQuote(contextDir) +
		" AIFAR_PACKAGE_NAME=" + installerkit.ShellQuote(item.Package.Name) +
		" AIFAR_PACKAGE_VERSION=" + installerkit.ShellQuote(item.Package.Version) +
		" AIFAR_ACTION_NAME=" + installerkit.ShellQuote(item.Action.Name) +
		" AIFAR_ACTION_OPERATION=" + installerkit.ShellQuote(item.Action.Operation) +
		" AIFAR_PHASE_NAME=" + installerkit.ShellQuote(phaseName) +
		" AIFAR_STEP_NAME=" + installerkit.ShellQuote(step.Name) +
		" AIFAR_LANGUAGE=" + installerkit.ShellQuote(string(i18n.Resolve(lang))) +
		"; set -a; . " + installerkit.ShellQuote(path.Join(contextDir, "cluster.env")) +
		"; . " + installerkit.ShellQuote(path.Join(contextDir, "secrets.env")) +
		"; set +a; sh " + installerkit.ShellQuote(remoteScript)
	stepCtx := ctx
	cancel := func() {}
	if step.TimeoutSeconds > 0 {
		stepCtx, cancel = context.WithTimeout(ctx, time.Duration(step.TimeoutSeconds)*time.Second)
	}
	_, err := installerkit.Run(stepCtx, s.remote, server, cmd, log.TargetLogger(server.ID), i18n.Text(lang, "orchestration.remoteCommandFailed"))
	cancel()
	if err != nil {
		log.FinishStep(server.ID, step.Name, "failed", err.Error())
		return err
	}
	log.FinishStep(server.ID, step.Name, "success", "")
	return nil
}

func (s *Service) uploadClusterContext(ctx context.Context, runtime clusterRuntime, server store.Server) error {
	tmpDir, err := os.MkdirTemp("", "aifar-orchestration-context-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	clusterJSON, err := json.MarshalIndent(map[string]any{
		"id": runtime.Cluster.ID, "name": runtime.Cluster.Name, "topology": runtime.Cluster.Topology,
		"coordinatorServerId": runtime.Cluster.CoordinatorServerID, "nodes": runtime.Cluster.Nodes,
	}, "", "  ")
	if err != nil {
		return err
	}
	clusterEnv := clusterEnvironment(runtime, server)
	secretEnv := secretEnvironment(runtime.Secrets)
	files := []struct {
		name string
		data []byte
		mode os.FileMode
	}{
		{name: "cluster.json", data: append(clusterJSON, '\n'), mode: 0o644},
		{name: "cluster.env", data: []byte(clusterEnv), mode: 0o600},
		{name: "secrets.env", data: []byte(secretEnv), mode: 0o600},
	}
	for _, file := range files {
		localPath := filepath.Join(tmpDir, file.name)
		if err := os.WriteFile(localPath, file.data, file.mode); err != nil {
			return err
		}
		remotePath := path.Join(runtime.WorkDirs[server.ID], "context", file.name)
		if err := s.remote.UploadFile(ctx, server, localPath, remotePath, file.mode); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) recordClusterResult(runtime clusterRuntime, item store.OrchestrationActionWithPackage, lang string, log RunLogger, orders map[string]int) error {
	result := runtime.Workflow.Result
	existingInstances, err := s.store.ListAppInstances()
	if err != nil {
		return err
	}
	for _, server := range runtime.Servers {
		order := orders[clusterPlanKey(server.ID, "record-result")]
		log.StartStep(server.ID, "record-result", i18n.Text(lang, "orchestration.stepRecordResult"), order)
		if result != nil {
			role := "member"
			if server.ID == runtime.Cluster.CoordinatorServerID {
				role = "coordinator"
			}
			version := result.Version
			if version == "" {
				version = item.Package.Version
			}
			endpoint := server.Host
			if result.Port > 0 {
				endpoint = fmt.Sprintf("%s:%d", server.Host, result.Port)
			}
			metadata := mustJSON(map[string]any{
				"clusterId": runtime.Cluster.ID, "clusterName": runtime.Cluster.Name,
				"role": role, "endpoint": endpoint, "taskId": runtime.Cluster.TaskID,
			})
			instanceRecord := store.AppInstance{
				App: result.App, Version: version, ServerID: server.ID, Status: result.Status,
				Topology: item.Action.Topology, Metadata: metadata,
			}
			if existing, ok := clusterAppInstance(existingInstances, runtime.Cluster.ID, result.App, server.ID); ok {
				instanceRecord.ID = existing.ID
				instanceRecord.CreatedAt = existing.CreatedAt
			}
			instance, err := s.store.SaveAppInstance(instanceRecord)
			if err != nil {
				log.FinishStep(server.ID, "record-result", "failed", err.Error())
				return err
			}
			if runtime.Credential.ID != "" {
				if _, err := s.store.BindCredential(store.CredentialBinding{
					CredentialID: runtime.Credential.ID, AppInstanceID: instance.ID,
					Purpose: runtime.Workflow.Credential.Purpose, ServiceName: result.App,
				}); err != nil {
					log.FinishStep(server.ID, "record-result", "failed", err.Error())
					return err
				}
			}
		}
		log.TargetLogger(server.ID).Info(i18n.Text(lang, "orchestration.actionTargetSucceeded"), item.Action.Operation, server.ID)
		log.FinishStep(server.ID, "record-result", "success", "")
	}
	return nil
}

func clusterAppInstance(instances []store.AppInstance, clusterID, app, serverID string) (store.AppInstance, bool) {
	for _, instance := range instances {
		if instance.App != app || instance.ServerID != serverID {
			continue
		}
		var metadata struct {
			ClusterID string `json:"clusterId"`
		}
		if json.Unmarshal([]byte(instance.Metadata), &metadata) == nil && metadata.ClusterID == clusterID {
			return instance, true
		}
	}
	return store.AppInstance{}, false
}

func storedStepMap(action store.OrchestrationAction) map[string]store.OrchestrationActionStep {
	out := make(map[string]store.OrchestrationActionStep, len(action.Steps))
	for _, step := range storedActionSteps(action) {
		out[step.Name] = step
	}
	return out
}

func clusterPlanOrderMap(steps []PlanStep) map[string]int {
	out := make(map[string]int, len(steps))
	for _, step := range steps {
		out[clusterPlanKey(step.Target, step.Name)] = step.Order
	}
	return out
}

func clusterPlanKey(target, name string) string {
	return target + "\x00" + name
}

func clusterEnvironment(runtime clusterRuntime, server store.Server) string {
	coordinator := coordinatorHost(runtime.Servers, runtime.Cluster.CoordinatorServerID)
	role := "member"
	nodeOrder := 0
	port := 0
	if runtime.Workflow.Result != nil {
		port = runtime.Workflow.Result.Port
	}
	for _, node := range runtime.Cluster.Nodes {
		if node.ServerID == server.ID {
			role = node.Role
			nodeOrder = node.Order
			break
		}
	}
	lines := []string{
		"AIFAR_CLUSTER_ID=" + installerkit.ShellQuote(runtime.Cluster.ID),
		"AIFAR_CLUSTER_NAME=" + installerkit.ShellQuote(runtime.Cluster.Name),
		"AIFAR_CLUSTER_TOPOLOGY=" + installerkit.ShellQuote(runtime.Cluster.Topology),
		"AIFAR_CLUSTER_COORDINATOR_SERVER_ID=" + installerkit.ShellQuote(runtime.Cluster.CoordinatorServerID),
		"AIFAR_CLUSTER_COORDINATOR_HOST=" + installerkit.ShellQuote(coordinator),
		fmt.Sprintf("AIFAR_CLUSTER_NODE_COUNT=%d", len(runtime.Cluster.Nodes)),
		"AIFAR_NODE_SERVER_ID=" + installerkit.ShellQuote(server.ID),
		"AIFAR_NODE_HOST=" + installerkit.ShellQuote(server.Host),
		"AIFAR_NODE_ROLE=" + installerkit.ShellQuote(role),
		fmt.Sprintf("AIFAR_NODE_INDEX=%d", nodeOrder),
		fmt.Sprintf("AIFAR_SERVICE_PORT=%d", port),
	}
	serverByID := make(map[string]store.Server, len(runtime.Servers))
	for _, item := range runtime.Servers {
		serverByID[item.ID] = item
	}
	for _, node := range runtime.Cluster.Nodes {
		prefix := fmt.Sprintf("AIFAR_CLUSTER_NODE_%d_", node.Order)
		lines = append(lines,
			prefix+"SERVER_ID="+installerkit.ShellQuote(node.ServerID),
			prefix+"HOST="+installerkit.ShellQuote(serverByID[node.ServerID].Host),
			prefix+"ROLE="+installerkit.ShellQuote(node.Role),
			prefix+"ENDPOINT="+installerkit.ShellQuote(node.Endpoint),
		)
	}
	return strings.Join(lines, "\n") + "\n"
}

func secretEnvironment(secrets map[string]generatedSecret) string {
	lines := []string{"# generated by AIFAR; do not log this file"}
	for _, secret := range secrets {
		lines = append(lines, secret.Spec.Env+"="+installerkit.ShellQuote(secret.Value))
	}
	return strings.Join(lines, "\n") + "\n"
}

func generateHexSecret(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func defaultClusterName(packageName, taskID string) string {
	name := strings.ReplaceAll(strings.TrimSpace(packageName), "-", "_")
	suffix := strings.TrimPrefix(strings.TrimSpace(taskID), "tsk_")
	if len(suffix) > 8 {
		suffix = suffix[:8]
	}
	if suffix != "" {
		name += "_" + suffix
	}
	if len(name) > 63 {
		name = name[:63]
	}
	return name
}

func coordinatorHost(servers []store.Server, coordinatorID string) string {
	for _, server := range servers {
		if server.ID == coordinatorID {
			return server.Host
		}
	}
	return ""
}

func sameClusterTargets(nodes []store.OrchestrationClusterNode, servers []store.Server) bool {
	if len(nodes) != len(servers) {
		return false
	}
	selected := make(map[string]bool, len(servers))
	for _, server := range servers {
		selected[server.ID] = true
	}
	for _, node := range nodes {
		if !selected[node.ServerID] {
			return false
		}
	}
	return true
}

func clusterFailuresError(prefix string, failures []taskrun.TargetFailure) error {
	parts := make([]string, 0, len(failures))
	for _, failure := range failures {
		parts = append(parts, fmt.Sprintf("%s: %v", failure.Target, failure.Err))
	}
	return fmt.Errorf("%s: %s", prefix, strings.Join(parts, "; "))
}

func finishClusterTargets(log RunLogger, servers []store.Server, status, errText string) {
	for _, server := range servers {
		log.FinishTarget(server.ID, status, errText)
	}
}
