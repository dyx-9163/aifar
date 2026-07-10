package orchestration

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"aifar-deployment/backend/internal/adapter"
	"aifar-deployment/backend/internal/i18n"
	"aifar-deployment/backend/internal/installer/installerkit"
	"aifar-deployment/backend/internal/store"
)

const (
	statusPublished       = "published"
	checkOperation        = "check"
	installOperation      = "install"
	specFileName          = "deployspec.json"
	maxPackageBytes       = 256 << 20
	maxExtractedBytes     = 512 << 20
	maxPackageFileCount   = 512
	defaultTimeoutSeconds = 60
)

var packageNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)
var clusterNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,62}$`)
var environmentNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{2,127}$`)

var reservedActionStepNames = map[string]bool{
	"load-server":    true,
	"verify-package": true,
	"upload-package": true,
	"record-result":  true,
}

type Remote interface {
	Run(ctx context.Context, server store.Server, command string) (adapter.CommandResult, error)
	UploadFile(ctx context.Context, server store.Server, localPath, remotePath string, mode os.FileMode) error
}

type Logger interface {
	Info(format string, args ...any)
	Error(format string, args ...any)
}

type RunLogger interface {
	Logger
	TaskID() string
	TargetLogger(target string) Logger
	PlanTarget(target string)
	StartTarget(target string)
	FinishTarget(target, status, errText string)
	PlanStep(target, name, title string, order int)
	StartStep(target, name, title string, order int)
	FinishStep(target, name, status, errText string)
}

type Service struct {
	store       *store.Store
	packageRoot string
	remote      Remote
}

func DefaultPackageRoot(databasePath string) string {
	root := filepath.Dir(strings.TrimSpace(databasePath))
	if root == "." || root == "" {
		root = "data"
	}
	return filepath.Join(root, "orchestration", "packages")
}

func NewService(s *store.Store, packageRoot string, remote Remote) *Service {
	if remote == nil {
		remote = adapter.SSHRemote{}
	}
	return &Service{store: s, packageRoot: packageRoot, remote: remote}
}

func (s *Service) UploadAndPublish(ctx context.Context, filename string, r io.Reader, actor string) (PackageView, error) {
	if r == nil {
		return PackageView{}, errors.New("package file is required")
	}
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(filename)), ".zip") {
		return PackageView{}, errors.New("only .zip action packages are supported")
	}
	if err := os.MkdirAll(s.packageRoot, 0o755); err != nil {
		return PackageView{}, err
	}
	tmpDir, err := os.MkdirTemp(s.packageRoot, "_upload-*")
	if err != nil {
		return PackageView{}, err
	}
	defer os.RemoveAll(tmpDir)

	tmpZip := filepath.Join(tmpDir, "bundle.zip")
	packageSHA, size, err := copyWithSHA256(tmpZip, r, maxPackageBytes)
	if err != nil {
		return PackageView{}, err
	}
	if size == 0 {
		return PackageView{}, errors.New("package file is empty")
	}

	extractDir := filepath.Join(tmpDir, "extract")
	files, specBytes, err := extractZip(tmpZip, extractDir)
	if err != nil {
		return PackageView{}, err
	}
	if len(specBytes) == 0 {
		return PackageView{}, fmt.Errorf("%s is required", specFileName)
	}
	spec, specJSON, err := parseSpec(specBytes)
	if err != nil {
		return PackageView{}, err
	}
	actions, err := actionsFromSpec(spec, files)
	if err != nil {
		return PackageView{}, err
	}
	if err := canonicalizeActionScripts(extractDir, actions, files); err != nil {
		return PackageView{}, err
	}

	finalDir := filepath.Join(s.packageRoot, installerkit.Sanitize(spec.Name), installerkit.Sanitize(specVersion(spec)), shortHash(packageSHA))
	if err := replaceDir(extractDir, finalDir); err != nil {
		return PackageView{}, err
	}
	finalZip := filepath.Join(finalDir, "bundle.zip")
	if err := copyFile(tmpZip, finalZip, 0o644); err != nil {
		return PackageView{}, err
	}

	pkg := store.OrchestrationPackage{
		Name:            spec.Name,
		Version:         specVersion(spec),
		TitleJSON:       mustJSON(spec.Title.withFallback(spec.Name)),
		DescriptionJSON: mustJSON(spec.Description.withFallback("")),
		Status:          statusPublished,
		PackagePath:     finalZip,
		PackageDir:      finalDir,
		PackageSHA256:   packageSHA,
		SpecJSON:        specJSON,
		SourceFilename:  filepath.Base(filename),
		UploadedBy:      strings.TrimSpace(actor),
	}
	storeActions := make([]store.OrchestrationAction, 0, len(actions))
	for _, action := range actions {
		storeActions = append(storeActions, store.OrchestrationAction{
			Name:            action.Name,
			TitleJSON:       mustJSON(action.Title.withFallback(action.Name)),
			DescriptionJSON: mustJSON(action.Description.withFallback("")),
			Operation:       action.Operation,
			ScriptPath:      action.Script,
			ScriptSHA256:    files[action.Script],
			TargetMode:      action.TargetMode,
			Topology:        action.Topology,
			MinTargets:      action.MinTargets,
			MaxTargets:      action.MaxTargets,
			ReadOnly:        action.ReadOnly != nil && *action.ReadOnly,
			TimeoutSeconds:  action.TimeoutSeconds,
			StagesJSON:      mustJSON(actionStageNames(action)),
			WorkflowJSON:    mustJSON(ActionWorkflow{Phases: action.Phases, Secrets: action.Secrets, Credential: action.Credential, Result: action.Result}),
			Steps:           storeActionSteps(action, files),
		})
	}
	saved, err := s.store.SaveOrchestrationPackageWithActions(pkg, storeActions)
	if err != nil {
		return PackageView{}, err
	}
	return packageView(saved), nil
}

func (s *Service) ListPackages(ctx context.Context) ([]PackageView, error) {
	items, err := s.store.ListOrchestrationPackages()
	if err != nil {
		return nil, err
	}
	out := make([]PackageView, 0, len(items))
	for _, item := range items {
		out = append(out, packageView(item))
	}
	return out, nil
}

func (s *Service) ListActions(ctx context.Context) ([]ActionView, error) {
	items, err := s.store.ListPublishedOrchestrationActions()
	if err != nil {
		return nil, err
	}
	out := make([]ActionView, 0, len(items))
	for _, item := range items {
		out = append(out, actionView(item.Action, item.Package))
	}
	return out, nil
}

func (s *Service) DryRun(ctx context.Context, req RunRequest, lang string) (DryRunResult, error) {
	item, servers, err := s.resolveActionAndTargets(req, false)
	if err != nil {
		return DryRunResult{}, err
	}
	action := actionView(item.Action, item.Package)
	result := DryRunResult{
		Action:      action,
		GeneratedAt: time.Now(),
		Targets:     targetPlans(servers, req.CoordinatorServerID, item.Action.TargetMode),
		Steps:       actionPlanSteps(item.Action, servers, req.CoordinatorServerID, lang),
		Issues: []PlanIssue{{
			Severity: "info",
			Message:  i18n.Text(lang, "orchestration.dryRunInfo"),
		}},
	}
	return result, nil
}

func (s *Service) RunAction(ctx context.Context, req RunRequest, lang string, log RunLogger) error {
	item, servers, err := s.resolveActionAndTargets(req, true)
	if err != nil {
		return err
	}
	action := item.Action
	pkg := item.Package
	if !supportedActionOperation(action.Operation) {
		return fmt.Errorf("unsupported orchestration operation %q", action.Operation)
	}
	for _, step := range actionPlanSteps(action, servers, req.CoordinatorServerID, lang) {
		if step.Target != "" {
			log.PlanTarget(step.Target)
		}
		log.PlanStep(step.Target, step.Name, step.Title, step.Order)
	}
	log.Info(i18n.Text(lang, "orchestration.actionRequested"), pkg.Name, pkg.Version, action.Name, action.Operation)
	if action.TargetMode == "cluster" {
		return s.runClusterAction(ctx, req, item, servers, lang, log)
	}
	var failures []string
	for _, server := range servers {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.runActionOnServer(ctx, pkg, action, server, lang, log); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", server.ID, err))
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "; "))
	}
	log.Info(i18n.Text(lang, "orchestration.actionCompleted"), action.Operation, len(servers))
	return nil
}

func (s *Service) RunCheck(ctx context.Context, req RunRequest, lang string, log RunLogger) error {
	return s.RunAction(ctx, req, lang, log)
}

func (s *Service) runActionOnServer(ctx context.Context, pkg store.OrchestrationPackage, action store.OrchestrationAction, server store.Server, lang string, log RunLogger) error {
	targetLog := log.TargetLogger(server.ID)
	log.StartTarget(server.ID)
	failTarget := func(step, errText string) error {
		if step != "" {
			log.FinishStep(server.ID, step, "failed", errText)
		}
		log.FinishTarget(server.ID, "failed", errText)
		return errors.New(errText)
	}

	log.StartStep(server.ID, "load-server", i18n.Text(lang, "orchestration.stepLoadServer"), 1)
	if strings.TrimSpace(server.Password) == "" && strings.TrimSpace(server.PrivateKey) == "" {
		return failTarget("load-server", i18n.Text(lang, "servers.credentialMissing"))
	}
	log.FinishStep(server.ID, "load-server", "success", "")

	log.StartStep(server.ID, "verify-package", i18n.Text(lang, "orchestration.stepVerifyPackage"), 2)
	actionSteps := storedActionSteps(action)
	for _, step := range actionSteps {
		scriptPath := filepath.Join(pkg.PackageDir, filepath.FromSlash(step.ScriptPath))
		gotSHA, err := fileSHA256(scriptPath)
		if err != nil {
			return failTarget("verify-package", err.Error())
		}
		if !strings.EqualFold(gotSHA, step.ScriptSHA256) {
			return failTarget("verify-package", fmt.Sprintf("script %s sha256 mismatch: expected %s, got %s", step.ScriptPath, step.ScriptSHA256, gotSHA))
		}
	}
	log.FinishStep(server.ID, "verify-package", "success", "")

	workDir := remoteWorkDir(server.DeployDir, pkg.Name, pkg.Version, log.TaskID())
	log.StartStep(server.ID, "upload-package", i18n.Text(lang, "orchestration.stepUploadPackage"), 3)
	targetLog.Info(i18n.Text(lang, "orchestration.prepareWorkDir"), workDir)
	if _, err := installerkit.Run(ctx, s.remote, server, "mkdir -p "+installerkit.ShellQuote(workDir), targetLog, i18n.Text(lang, "orchestration.remoteCommandFailed")); err != nil {
		return failTarget("upload-package", err.Error())
	}
	executablePaths := map[string]bool{}
	for _, step := range actionSteps {
		executablePaths[step.ScriptPath] = true
	}
	if err := s.uploadPackageFiles(ctx, pkg.PackageDir, workDir, executablePaths, server, lang, targetLog); err != nil {
		return failTarget("upload-package", err.Error())
	}
	log.FinishStep(server.ID, "upload-package", "success", "")

	for index, step := range actionSteps {
		order := index + 4
		title := localizedTitle(step.TitleJSON, lang, step.Name)
		log.StartStep(server.ID, step.Name, title, order)
		remoteScript := path.Join(workDir, step.ScriptPath)
		cmd := "AIFAR_WORK_DIR=" + installerkit.ShellQuote(workDir) +
			" AIFAR_PACKAGE_NAME=" + installerkit.ShellQuote(pkg.Name) +
			" AIFAR_PACKAGE_VERSION=" + installerkit.ShellQuote(pkg.Version) +
			" AIFAR_ACTION_NAME=" + installerkit.ShellQuote(action.Name) +
			" AIFAR_ACTION_OPERATION=" + installerkit.ShellQuote(action.Operation) +
			" AIFAR_STEP_NAME=" + installerkit.ShellQuote(step.Name) +
			" AIFAR_LANGUAGE=" + installerkit.ShellQuote(string(i18n.Resolve(lang))) +
			" sh " + installerkit.ShellQuote(remoteScript)
		stepCtx := ctx
		cancel := func() {}
		if step.TimeoutSeconds > 0 {
			stepCtx, cancel = context.WithTimeout(ctx, time.Duration(step.TimeoutSeconds)*time.Second)
		}
		_, runErr := installerkit.Run(stepCtx, s.remote, server, cmd, targetLog, i18n.Text(lang, "orchestration.remoteCommandFailed"))
		cancel()
		if runErr != nil {
			return failTarget(step.Name, runErr.Error())
		}
		log.FinishStep(server.ID, step.Name, "success", "")
	}

	recordOrder := len(actionSteps) + 4
	log.StartStep(server.ID, "record-result", i18n.Text(lang, "orchestration.stepRecordResult"), recordOrder)
	targetLog.Info(i18n.Text(lang, "orchestration.actionTargetSucceeded"), action.Operation, server.ID)
	log.FinishStep(server.ID, "record-result", "success", "")
	log.FinishTarget(server.ID, "success", "")
	return nil
}

func (s *Service) uploadPackageFiles(ctx context.Context, packageDir, workDir string, executablePaths map[string]bool, server store.Server, lang string, log Logger) error {
	return filepath.WalkDir(packageDir, func(localPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(packageDir, localPath)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "bundle.zip" {
			return nil
		}
		mode := os.FileMode(0o644)
		if executablePaths[rel] {
			mode = 0o755
		}
		remotePath := path.Join(workDir, rel)
		log.Info(i18n.Text(lang, "orchestration.uploadFile"), rel)
		return s.remote.UploadFile(ctx, server, localPath, remotePath, mode)
	})
}

func (s *Service) resolveActionAndTargets(req RunRequest, includeSecret bool) (store.OrchestrationActionWithPackage, []store.Server, error) {
	actionID := strings.TrimSpace(req.ActionID)
	if actionID == "" {
		return store.OrchestrationActionWithPackage{}, nil, errors.New("action id is required")
	}
	serverIDs := cleanIDs(req.ServerIDs)
	if len(serverIDs) == 0 {
		return store.OrchestrationActionWithPackage{}, nil, errors.New("target server is required")
	}
	item, err := s.store.GetPublishedOrchestrationAction(actionID)
	if err != nil {
		return item, nil, err
	}
	if !supportedActionOperation(item.Action.Operation) {
		return item, nil, fmt.Errorf("unsupported orchestration operation %q", item.Action.Operation)
	}
	if item.Action.TargetMode == "single" && len(serverIDs) > 1 {
		return item, nil, errors.New("this action supports only one target server")
	}
	if item.Action.TargetMode == "cluster" {
		if item.Action.MinTargets > 0 && item.Action.MinTargets == item.Action.MaxTargets && len(serverIDs) != item.Action.MinTargets {
			return item, nil, fmt.Errorf("this cluster action requires exactly %d target servers", item.Action.MinTargets)
		}
		if item.Action.MinTargets > 0 && len(serverIDs) < item.Action.MinTargets {
			return item, nil, fmt.Errorf("this cluster action requires at least %d target servers", item.Action.MinTargets)
		}
		if item.Action.MaxTargets > 0 && len(serverIDs) > item.Action.MaxTargets {
			return item, nil, fmt.Errorf("this cluster action supports at most %d target servers", item.Action.MaxTargets)
		}
		coordinatorID := strings.TrimSpace(req.CoordinatorServerID)
		if coordinatorID == "" {
			return item, nil, errors.New("cluster coordinator server is required")
		}
		selected := false
		for _, serverID := range serverIDs {
			if serverID == coordinatorID {
				selected = true
				break
			}
		}
		if !selected {
			return item, nil, errors.New("cluster coordinator must be one of the selected target servers")
		}
		if name := strings.TrimSpace(req.ClusterName); name != "" && !clusterNamePattern.MatchString(name) {
			return item, nil, fmt.Errorf("invalid cluster name %q", name)
		}
	}
	servers := make([]store.Server, 0, len(serverIDs))
	hosts := map[string]bool{}
	for _, id := range serverIDs {
		server, err := s.store.GetServer(id, includeSecret)
		if err != nil {
			return item, nil, err
		}
		if item.Action.TargetMode == "cluster" {
			host := strings.ToLower(strings.TrimSpace(server.Host))
			if host == "" {
				return item, nil, fmt.Errorf("cluster target %s has no host", server.ID)
			}
			if hosts[host] {
				return item, nil, fmt.Errorf("cluster target host %s is duplicated", server.Host)
			}
			hosts[host] = true
		}
		servers = append(servers, server)
	}
	return item, servers, nil
}

func actionPlanSteps(action store.OrchestrationAction, servers []store.Server, coordinatorID, lang string) []PlanStep {
	if action.TargetMode == "cluster" {
		return clusterActionPlanSteps(action, servers, coordinatorID, lang)
	}
	steps := []PlanStep{}
	defs := []PlanStep{
		{Name: "load-server", Title: i18n.Text(lang, "orchestration.stepLoadServer"), Order: 1},
		{Name: "verify-package", Title: i18n.Text(lang, "orchestration.stepVerifyPackage"), Order: 2},
		{Name: "upload-package", Title: i18n.Text(lang, "orchestration.stepUploadPackage"), Order: 3},
	}
	for index, step := range storedActionSteps(action) {
		defs = append(defs, PlanStep{Name: step.Name, Title: localizedTitle(step.TitleJSON, lang, step.Name), Order: index + 4})
	}
	defs = append(defs, PlanStep{Name: "record-result", Title: i18n.Text(lang, "orchestration.stepRecordResult"), Order: len(defs) + 1})
	for _, server := range servers {
		for _, def := range defs {
			def.Target = server.ID
			steps = append(steps, def)
		}
	}
	return steps
}

func targetPlans(servers []store.Server, coordinatorID, targetMode string) []TargetPlan {
	out := make([]TargetPlan, 0, len(servers))
	for index, server := range servers {
		role := ""
		coordinator := false
		if targetMode == "cluster" {
			coordinator = server.ID == coordinatorID
			role = "member"
			if coordinator {
				role = "coordinator"
			}
		}
		out = append(out, TargetPlan{
			ServerID:    server.ID,
			Name:        server.Name,
			Host:        server.Host,
			Status:      server.Status,
			Role:        role,
			Coordinator: coordinator,
			Order:       index + 1,
		})
	}
	return out
}

func packageView(item store.OrchestrationPackageWithActions) PackageView {
	actions := make([]ActionView, 0, len(item.Actions))
	for _, action := range item.Actions {
		actions = append(actions, actionView(action, item.Package))
	}
	return PackageView{
		ID:             item.Package.ID,
		Name:           item.Package.Name,
		Version:        item.Package.Version,
		Title:          parseLocalized(item.Package.TitleJSON),
		Description:    parseLocalized(item.Package.DescriptionJSON),
		Status:         item.Package.Status,
		PackageSHA256:  item.Package.PackageSHA256,
		SourceFilename: item.Package.SourceFilename,
		UploadedBy:     item.Package.UploadedBy,
		CreatedAt:      item.Package.CreatedAt,
		UpdatedAt:      item.Package.UpdatedAt,
		PublishedAt:    item.Package.PublishedAt,
		Actions:        actions,
	}
}

func actionView(action store.OrchestrationAction, pkg store.OrchestrationPackage) ActionView {
	return ActionView{
		ID:             action.ID,
		PackageID:      pkg.ID,
		PackageName:    pkg.Name,
		PackageVersion: pkg.Version,
		Name:           action.Name,
		Title:          parseLocalized(action.TitleJSON),
		Description:    parseLocalized(action.DescriptionJSON),
		Operation:      action.Operation,
		ScriptPath:     action.ScriptPath,
		ScriptSHA256:   action.ScriptSHA256,
		TargetMode:     action.TargetMode,
		Topology:       action.Topology,
		MinTargets:     action.MinTargets,
		MaxTargets:     action.MaxTargets,
		ReadOnly:       action.ReadOnly,
		TimeoutSeconds: action.TimeoutSeconds,
		Stages:         parseStringSlice(action.StagesJSON),
		Steps:          actionStepViews(action),
		Phases:         actionPhaseViews(action),
	}
}

func parseSpec(data []byte) (PackageSpec, string, error) {
	var spec PackageSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return spec, "", fmt.Errorf("parse deployspec.json: %w", err)
	}
	spec.Name = strings.TrimSpace(spec.Name)
	spec.Version = strings.TrimSpace(spec.Version)
	spec.PackageVersion = strings.TrimSpace(spec.PackageVersion)
	if spec.Version == "" {
		spec.Version = spec.PackageVersion
	}
	if !packageNamePattern.MatchString(spec.Name) {
		return spec, "", fmt.Errorf("invalid package name %q", spec.Name)
	}
	if spec.Version == "" {
		return spec, "", errors.New("package version is required")
	}
	canonical, err := json.Marshal(spec)
	if err != nil {
		return spec, "", err
	}
	return spec, string(canonical), nil
}

func actionsFromSpec(spec PackageSpec, files map[string]string) ([]ActionSpec, error) {
	actions := spec.Actions
	if len(actions) == 0 && strings.TrimSpace(spec.Scripts.Check.Path) != "" {
		readOnly := true
		actions = []ActionSpec{{
			Name:           checkOperation,
			Operation:      checkOperation,
			Title:          spec.Scripts.Check.Title,
			Description:    spec.Scripts.Check.Description,
			Script:         spec.Scripts.Check.Path,
			TargetMode:     "multiple",
			ReadOnly:       &readOnly,
			TimeoutSeconds: spec.Scripts.Check.TimeoutSeconds,
		}}
	}
	if len(actions) == 0 {
		return nil, errors.New("at least one action is required")
	}
	out := make([]ActionSpec, 0, len(actions))
	seen := map[string]bool{}
	for _, action := range actions {
		action.Name = strings.TrimSpace(action.Name)
		action.Operation = strings.TrimSpace(action.Operation)
		if action.Operation == "" {
			action.Operation = action.Name
		}
		if action.Name == "" {
			action.Name = action.Operation
		}
		if !packageNamePattern.MatchString(action.Name) {
			return nil, fmt.Errorf("invalid action name %q", action.Name)
		}
		if !supportedActionOperation(action.Operation) {
			return nil, fmt.Errorf("unsupported action operation %q", action.Operation)
		}
		expectedReadOnly := action.Operation == checkOperation
		if action.ReadOnly == nil {
			action.ReadOnly = boolPtr(expectedReadOnly)
		} else if *action.ReadOnly != expectedReadOnly {
			return nil, fmt.Errorf("action %s operation %s must declare readOnly=%t", action.Name, action.Operation, expectedReadOnly)
		}
		action.TargetMode = strings.TrimSpace(action.TargetMode)
		if action.TargetMode == "" {
			action.TargetMode = "multiple"
		}
		if action.TargetMode != "single" && action.TargetMode != "multiple" && action.TargetMode != "cluster" {
			return nil, fmt.Errorf("invalid targetMode %q", action.TargetMode)
		}
		if seen[action.Name] {
			return nil, fmt.Errorf("duplicate action %s", action.Name)
		}
		seen[action.Name] = true
		if action.TimeoutSeconds <= 0 {
			action.TimeoutSeconds = defaultTimeoutSeconds
		}
		if action.TargetMode == "cluster" {
			action.Topology = strings.TrimSpace(action.Topology)
			if action.Topology == "" {
				return nil, fmt.Errorf("cluster action %s requires topology", action.Name)
			}
			if action.MinTargets <= 0 {
				action.MinTargets = 3
			}
			if action.MaxTargets <= 0 {
				action.MaxTargets = action.MinTargets
			}
			if action.MaxTargets < action.MinTargets {
				return nil, fmt.Errorf("cluster action %s maxTargets must be greater than or equal to minTargets", action.Name)
			}
			if len(action.Phases) == 0 {
				return nil, fmt.Errorf("cluster action %s requires phases", action.Name)
			}
			if len(action.Steps) > 0 || strings.TrimSpace(action.Script) != "" {
				return nil, fmt.Errorf("cluster action %s must declare scripts inside phases", action.Name)
			}
		} else {
			action.MinTargets = 1
			if action.TargetMode == "single" {
				action.MaxTargets = 1
			} else {
				action.MaxTargets = 0
			}
			if len(action.Phases) > 0 {
				return nil, fmt.Errorf("phases require targetMode cluster in action %s", action.Name)
			}
			if len(action.Steps) == 0 {
				stepName := action.Name
				if action.Operation == checkOperation {
					stepName = "run-check"
				}
				action.Steps = []ActionStepSpec{{
					Name:           stepName,
					Title:          action.Title,
					Script:         action.Script,
					ReadOnly:       boolPtr(*action.ReadOnly),
					TimeoutSeconds: action.TimeoutSeconds,
				}}
			}
		}
		stepNames := map[string]bool{}
		hasMutatingStep := false
		if action.TargetMode == "cluster" {
			phaseNames := map[string]bool{}
			flattened := make([]ActionStepSpec, 0)
			for phaseIndex, phase := range action.Phases {
				phase.Name = strings.TrimSpace(phase.Name)
				if !packageNamePattern.MatchString(phase.Name) || phaseNames[phase.Name] {
					return nil, fmt.Errorf("invalid or duplicate phase name %q in action %s", phase.Name, action.Name)
				}
				phaseNames[phase.Name] = true
				phase.Scope = strings.ToLower(strings.TrimSpace(phase.Scope))
				switch phase.Scope {
				case "node":
					if phase.Mode == "" {
						phase.Mode = "parallel"
					}
					phase.RunOn = "all"
				case "cluster":
					if phase.Mode == "" {
						phase.Mode = "sequential"
					}
					if phase.RunOn == "" {
						phase.RunOn = "coordinator"
					}
					if phase.RunOn != "coordinator" {
						return nil, fmt.Errorf("cluster phase %s must run on coordinator", phase.Name)
					}
				case "barrier":
					phase.Mode = "sequential"
					phase.RunOn = "coordinator"
					if len(phase.Steps) > 0 {
						return nil, fmt.Errorf("barrier phase %s cannot contain steps", phase.Name)
					}
				default:
					return nil, fmt.Errorf("invalid phase scope %q in action %s", phase.Scope, action.Name)
				}
				phase.Mode = strings.ToLower(strings.TrimSpace(phase.Mode))
				if phase.Mode != "sequential" && phase.Mode != "parallel" {
					return nil, fmt.Errorf("invalid phase mode %q in phase %s", phase.Mode, phase.Name)
				}
				if phase.Scope != "barrier" && len(phase.Steps) == 0 {
					return nil, fmt.Errorf("phase %s requires at least one step", phase.Name)
				}
				for stepIndex, step := range phase.Steps {
					normalized, mutating, err := normalizeActionStep(action, step, files, stepNames)
					if err != nil {
						return nil, err
					}
					phase.Steps[stepIndex] = normalized
					flattened = append(flattened, normalized)
					hasMutatingStep = hasMutatingStep || mutating
				}
				action.Phases[phaseIndex] = phase
			}
			action.Steps = flattened
		} else {
			for index, step := range action.Steps {
				normalized, mutating, err := normalizeActionStep(action, step, files, stepNames)
				if err != nil {
					return nil, err
				}
				action.Steps[index] = normalized
				hasMutatingStep = hasMutatingStep || mutating
			}
		}
		if !*action.ReadOnly && !hasMutatingStep {
			return nil, fmt.Errorf("mutating action %s must contain at least one mutating step", action.Name)
		}
		if err := normalizeActionSecrets(&action); err != nil {
			return nil, err
		}
		action.Script = action.Steps[0].Script
		action.Stages = actionStageNames(action)
		out = append(out, action)
	}
	return out, nil
}

func normalizeActionStep(action ActionSpec, step ActionStepSpec, files map[string]string, stepNames map[string]bool) (ActionStepSpec, bool, error) {
	step.Name = strings.TrimSpace(step.Name)
	if !packageNamePattern.MatchString(step.Name) || reservedActionStepNames[step.Name] {
		return step, false, fmt.Errorf("invalid or reserved step name %q", step.Name)
	}
	if stepNames[step.Name] {
		return step, false, fmt.Errorf("duplicate step %s in action %s", step.Name, action.Name)
	}
	stepNames[step.Name] = true
	script, err := safeRelPath(step.Script)
	if err != nil {
		return step, false, fmt.Errorf("invalid script path for step %s: %w", step.Name, err)
	}
	step.Script = script
	if _, ok := files[step.Script]; !ok {
		return step, false, fmt.Errorf("script %s is missing from package", step.Script)
	}
	if step.ReadOnly == nil {
		step.ReadOnly = boolPtr(*action.ReadOnly)
	}
	if *action.ReadOnly && !*step.ReadOnly {
		return step, false, fmt.Errorf("read-only action %s cannot contain mutating step %s", action.Name, step.Name)
	}
	if step.TimeoutSeconds <= 0 {
		step.TimeoutSeconds = action.TimeoutSeconds
	}
	return step, !*step.ReadOnly, nil
}

func normalizeActionSecrets(action *ActionSpec) error {
	if action.TargetMode != "cluster" && (len(action.Secrets) > 0 || action.Credential != nil || action.Result != nil) {
		return fmt.Errorf("secrets, credential and result require targetMode cluster in action %s", action.Name)
	}
	secretNames := map[string]bool{}
	environmentNames := map[string]bool{}
	for index, secret := range action.Secrets {
		secret.Name = strings.TrimSpace(secret.Name)
		secret.Env = strings.TrimSpace(secret.Env)
		secret.Generator = strings.ToLower(strings.TrimSpace(secret.Generator))
		if secret.Generator == "" {
			secret.Generator = "hex"
		}
		if !packageNamePattern.MatchString(secret.Name) || secretNames[secret.Name] {
			return fmt.Errorf("invalid or duplicate generated secret name %q", secret.Name)
		}
		if !environmentNamePattern.MatchString(secret.Env) || environmentNames[secret.Env] {
			return fmt.Errorf("invalid or duplicate generated secret environment %q", secret.Env)
		}
		if secret.Generator != "hex" {
			return fmt.Errorf("unsupported generated secret generator %q", secret.Generator)
		}
		if secret.Bytes == 0 {
			secret.Bytes = 24
		}
		if secret.Bytes < 16 || secret.Bytes > 64 {
			return fmt.Errorf("generated secret %s bytes must be between 16 and 64", secret.Name)
		}
		secretNames[secret.Name] = true
		environmentNames[secret.Env] = true
		action.Secrets[index] = secret
	}
	if action.Credential != nil {
		action.Credential.Kind = strings.ToLower(strings.TrimSpace(action.Credential.Kind))
		action.Credential.Username = strings.TrimSpace(action.Credential.Username)
		action.Credential.Purpose = strings.TrimSpace(action.Credential.Purpose)
		action.Credential.SecretName = strings.TrimSpace(action.Credential.SecretName)
		action.Credential.SecretKey = strings.TrimSpace(action.Credential.SecretKey)
		if action.Credential.Kind == "" || action.Credential.SecretName == "" {
			return fmt.Errorf("credential kind and secretName are required in action %s", action.Name)
		}
		if !secretNames[action.Credential.SecretName] {
			return fmt.Errorf("credential references unknown generated secret %s", action.Credential.SecretName)
		}
		if action.Credential.SecretKey == "" {
			action.Credential.SecretKey = "password"
		}
	}
	if action.Result != nil {
		action.Result.App = strings.TrimSpace(action.Result.App)
		action.Result.Version = strings.TrimSpace(action.Result.Version)
		action.Result.Status = strings.TrimSpace(action.Result.Status)
		if !packageNamePattern.MatchString(action.Result.App) {
			return fmt.Errorf("invalid result app %q in action %s", action.Result.App, action.Name)
		}
		if action.Result.Status == "" {
			action.Result.Status = "installed"
		}
		if action.Result.Port < 0 || action.Result.Port > 65535 {
			return fmt.Errorf("invalid result port %d in action %s", action.Result.Port, action.Name)
		}
	}
	return nil
}

func canonicalizeActionScripts(root string, actions []ActionSpec, files map[string]string) error {
	seen := map[string]bool{}
	for _, action := range actions {
		for _, step := range action.Steps {
			if seen[step.Script] {
				continue
			}
			seen[step.Script] = true

			scriptPath := filepath.Join(root, filepath.FromSlash(step.Script))
			data, err := os.ReadFile(scriptPath)
			if err != nil {
				return fmt.Errorf("read script %s: %w", step.Script, err)
			}
			normalized, err := canonicalizeShellScript(data)
			if err != nil {
				return fmt.Errorf("invalid script %s: %w", step.Script, err)
			}
			if !bytes.Equal(data, normalized) {
				if err := os.WriteFile(scriptPath, normalized, 0o644); err != nil {
					return fmt.Errorf("normalize script %s: %w", step.Script, err)
				}
			}
			hash := sha256.Sum256(normalized)
			files[step.Script] = hex.EncodeToString(hash[:])
		}
	}
	return nil
}

func canonicalizeShellScript(data []byte) ([]byte, error) {
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, errors.New("contains a NUL byte")
	}
	if !utf8.Valid(data) {
		return nil, errors.New("must be UTF-8 encoded")
	}
	normalized := data
	utf8BOM := []byte{0xef, 0xbb, 0xbf}
	if bytes.HasPrefix(normalized, utf8BOM) {
		normalized = normalized[len(utf8BOM):]
	}
	normalized = bytes.ReplaceAll(normalized, []byte("\r\n"), []byte("\n"))
	if bytes.IndexByte(normalized, '\r') >= 0 {
		return nil, errors.New("contains an unsupported carriage return")
	}
	if len(normalized) == 0 {
		return nil, errors.New("is empty")
	}
	return normalized, nil
}

func extractZip(zipPath, destDir string) (map[string]string, []byte, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, nil, err
	}
	defer reader.Close()
	if len(reader.File) > maxPackageFileCount {
		return nil, nil, fmt.Errorf("package has too many files: %d", len(reader.File))
	}
	files := map[string]string{}
	var spec []byte
	var total uint64
	for _, f := range reader.File {
		isDir := f.FileInfo().IsDir()
		rel, err := safeZipEntryPath(f.Name, isDir)
		if err != nil {
			return nil, nil, err
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return nil, nil, fmt.Errorf("symbolic links are not allowed: %s", rel)
		}
		if isDir {
			continue
		}
		if _, exists := files[rel]; exists {
			return nil, nil, fmt.Errorf("duplicate package path %s", rel)
		}
		total += f.UncompressedSize64
		if total > maxExtractedBytes {
			return nil, nil, errors.New("package extracted content is too large")
		}
		hash, data, err := extractZipFile(f, filepath.Join(destDir, filepath.FromSlash(rel)))
		if err != nil {
			return nil, nil, err
		}
		files[rel] = hash
		if rel == specFileName {
			spec = data
		}
	}
	return files, spec, nil
}

func extractZipFile(f *zip.File, dest string) (string, []byte, error) {
	rc, err := f.Open()
	if err != nil {
		return "", nil, err
	}
	defer rc.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", nil, err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", nil, err
	}
	defer out.Close()
	h := sha256.New()
	var buf strings.Builder
	writer := io.MultiWriter(out, h, limitedSpecWriter(path.Base(filepath.ToSlash(f.Name)), &buf))
	if _, err := io.Copy(writer, rc); err != nil {
		return "", nil, err
	}
	return hex.EncodeToString(h.Sum(nil)), []byte(buf.String()), nil
}

func limitedSpecWriter(name string, buf *strings.Builder) io.Writer {
	if name != specFileName {
		return discardWriter{}
	}
	return maxStringWriter{buf: buf, limit: 2 << 20}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

type maxStringWriter struct {
	buf   *strings.Builder
	limit int
}

func (w maxStringWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.limit {
		return 0, errors.New("deployspec.json is too large")
	}
	return w.buf.Write(p)
}

func copyWithSHA256(dst string, r io.Reader, limit int64) (string, int64, error) {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", 0, err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, limit+1))
	if err != nil {
		return "", 0, err
	}
	if n > limit {
		return "", n, fmt.Errorf("package exceeds %d bytes", limit)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func replaceDir(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyDir(src, dst); err != nil {
		return err
	}
	return os.RemoveAll(src)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target, 0o644)
	})
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func safeRelPath(value string) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if value == "" || strings.HasPrefix(value, "/") || strings.Contains(value, "\x00") || strings.Contains(value, ":") {
		return "", fmt.Errorf("%q is not a safe relative path", value)
	}
	parts := strings.Split(value, "/")
	cleanParts := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("%q is not a safe relative path", value)
		}
		cleanParts = append(cleanParts, part)
	}
	return path.Join(cleanParts...), nil
}

func safeZipEntryPath(value string, isDir bool) (string, error) {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	if isDir {
		value = strings.TrimRight(value, "/")
	}
	return safeRelPath(value)
}

func remoteWorkDir(deployDir, name, version, taskID string) string {
	return path.Join(
		installerkit.RemoteDeployDir(deployDir),
		"_work",
		"orchestration-"+installerkit.Sanitize(name)+"-"+installerkit.Sanitize(version)+"-"+installerkit.Sanitize(taskID),
	)
}

func cleanIDs(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func supportedActionOperation(operation string) bool {
	switch strings.TrimSpace(operation) {
	case checkOperation, installOperation:
		return true
	default:
		return false
	}
}

func actionStageNames(action ActionSpec) []string {
	if len(action.Phases) > 0 {
		out := make([]string, 0, len(action.Phases))
		for _, phase := range action.Phases {
			if name := strings.TrimSpace(phase.Name); name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	out := make([]string, 0, len(action.Steps))
	for _, step := range action.Steps {
		if name := strings.TrimSpace(step.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func storeActionSteps(action ActionSpec, files map[string]string) []store.OrchestrationActionStep {
	out := make([]store.OrchestrationActionStep, 0, len(action.Steps))
	for index, step := range action.Steps {
		readOnly := step.ReadOnly != nil && *step.ReadOnly
		out = append(out, store.OrchestrationActionStep{
			Name:           step.Name,
			TitleJSON:      mustJSON(step.Title.withFallback(step.Name)),
			ScriptPath:     step.Script,
			ScriptSHA256:   files[step.Script],
			ReadOnly:       readOnly,
			TimeoutSeconds: step.TimeoutSeconds,
			Order:          index + 1,
		})
	}
	return out
}

func storedActionSteps(action store.OrchestrationAction) []store.OrchestrationActionStep {
	if len(action.Steps) > 0 {
		return action.Steps
	}
	name := action.Name
	if action.Operation == checkOperation {
		name = "run-check"
	}
	return []store.OrchestrationActionStep{{
		Name:           name,
		TitleJSON:      action.TitleJSON,
		ScriptPath:     action.ScriptPath,
		ScriptSHA256:   action.ScriptSHA256,
		ReadOnly:       action.ReadOnly,
		TimeoutSeconds: action.TimeoutSeconds,
		Order:          1,
	}}
}

func actionStepViews(action store.OrchestrationAction) []ActionStepView {
	steps := storedActionSteps(action)
	out := make([]ActionStepView, 0, len(steps))
	for _, step := range steps {
		out = append(out, ActionStepView{
			ID:             step.ID,
			Name:           step.Name,
			Title:          parseLocalized(step.TitleJSON),
			ScriptPath:     step.ScriptPath,
			ScriptSHA256:   step.ScriptSHA256,
			ReadOnly:       step.ReadOnly,
			TimeoutSeconds: step.TimeoutSeconds,
			Order:          step.Order,
		})
	}
	return out
}

func actionPhaseViews(action store.OrchestrationAction) []ActionPhaseView {
	workflow := storedActionWorkflow(action)
	stepByName := map[string]ActionStepView{}
	for _, step := range actionStepViews(action) {
		stepByName[step.Name] = step
	}
	if len(workflow.Phases) == 0 {
		return []ActionPhaseView{{
			Name:  "execute",
			Title: parseLocalized(action.TitleJSON).withFallback(action.Name),
			Scope: "node",
			Mode:  "sequential",
			RunOn: "all",
			Order: 1,
			Steps: actionStepViews(action),
		}}
	}
	out := make([]ActionPhaseView, 0, len(workflow.Phases))
	for index, phase := range workflow.Phases {
		steps := make([]ActionStepView, 0, len(phase.Steps))
		for _, phaseStep := range phase.Steps {
			if step, ok := stepByName[phaseStep.Name]; ok {
				steps = append(steps, step)
			}
		}
		out = append(out, ActionPhaseView{
			Name:  phase.Name,
			Title: phase.Title.withFallback(phase.Name),
			Scope: phase.Scope,
			Mode:  phase.Mode,
			RunOn: phase.RunOn,
			Order: index + 1,
			Steps: steps,
		})
	}
	return out
}

func storedActionWorkflow(action store.OrchestrationAction) ActionWorkflow {
	var workflow ActionWorkflow
	if err := json.Unmarshal([]byte(action.WorkflowJSON), &workflow); err != nil {
		return ActionWorkflow{}
	}
	return workflow
}

func localizedTitle(raw, lang, fallback string) string {
	labels := parseLocalized(raw).withFallback(fallback)
	return labels[string(i18n.Resolve(lang))]
}

func parseLocalized(raw string) LocalizedText {
	var out LocalizedText
	if err := json.Unmarshal([]byte(raw), &out); err != nil || out == nil {
		return LocalizedText{}
	}
	return out
}

func parseStringSlice(raw string) []string {
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return out
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func specVersion(spec PackageSpec) string {
	if strings.TrimSpace(spec.Version) != "" {
		return strings.TrimSpace(spec.Version)
	}
	return strings.TrimSpace(spec.PackageVersion)
}

func boolPtr(value bool) *bool {
	return &value
}

func shortHash(value string) string {
	if len(value) <= 12 {
		return value
	}
	return value[:12]
}

func (t LocalizedText) withFallback(fallback string) LocalizedText {
	out := LocalizedText{}
	for key, value := range t {
		value = strings.TrimSpace(value)
		if value != "" {
			out[key] = value
		}
	}
	if strings.TrimSpace(out["zh"]) == "" {
		out["zh"] = fallback
	}
	if strings.TrimSpace(out["en"]) == "" {
		out["en"] = fallback
	}
	return out
}
