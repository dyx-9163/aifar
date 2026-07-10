package httpapi

import (
	"context"
	"net/http"
	"strings"

	"aifar-deployment/backend/internal/i18n"
	"aifar-deployment/backend/internal/orchestration"
	"aifar-deployment/backend/internal/store"
	"aifar-deployment/backend/internal/taskplan"
	"aifar-deployment/backend/internal/worker"

	"github.com/go-chi/chi/v5"
)

func (a *API) orchestrationService() *orchestration.Service {
	return orchestration.NewService(a.store, orchestration.DefaultPackageRoot(a.cfg.DatabasePath), nil)
}

func (a *API) listOrchestrationPackages(w http.ResponseWriter, r *http.Request) {
	out, err := a.orchestrationService().ListPackages(r.Context())
	respond(w, out, err)
}

func (a *API) listOrchestrationActions(w http.ResponseWriter, r *http.Request) {
	out, err := a.orchestrationService().ListActions(r.Context())
	respond(w, out, err)
}

func (a *API) uploadOrchestrationPackage(w http.ResponseWriter, r *http.Request) {
	lang := languageFromRequest(r)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "ORCHESTRATION_PACKAGE_REQUIRED", i18n.Text(lang, "orchestration.packageRequired"), nil)
		return
	}
	defer file.Close()
	pkg, err := a.orchestrationService().UploadAndPublish(r.Context(), header.Filename, file, currentUser(r).Username)
	if err != nil {
		writeError(w, http.StatusBadRequest, "ORCHESTRATION_PACKAGE_INVALID", err.Error(), nil)
		return
	}
	a.audit(r, "orchestration.package.upload", pkg.ID, "success", pkg.Name+"@"+pkg.Version)
	writeJSON(w, http.StatusCreated, pkg)
}

func (a *API) dryRunOrchestrationAction(w http.ResponseWriter, r *http.Request) {
	lang := languageFromRequest(r)
	var req orchestration.RunRequest
	if !decode(w, r, &req) {
		return
	}
	req.ActionID = chi.URLParam(r, "id")
	if strings.TrimSpace(req.Language) != "" {
		lang = req.Language
	}
	result, err := a.orchestrationService().DryRun(r.Context(), req, lang)
	respond(w, result, err)
}

func (a *API) runOrchestrationAction(w http.ResponseWriter, r *http.Request) {
	lang := languageFromRequest(r)
	var req orchestration.RunRequest
	if !decode(w, r, &req) {
		return
	}
	req.ActionID = chi.URLParam(r, "id")
	if strings.TrimSpace(req.Language) != "" {
		lang = req.Language
	}
	svc := a.orchestrationService()
	plan, err := svc.DryRun(r.Context(), req, lang)
	if err != nil {
		respond(w, nil, err)
		return
	}
	target := strings.Join(cleanStringIDs(req.ServerIDs), ",")
	actor := currentUser(r).Username
	taskType := "orchestration." + plan.Action.Operation
	task, err := a.store.CreateTask(store.Task{Type: taskType, Target: target, Status: "pending", CreatedBy: actor})
	if err != nil {
		respondTask(w, task, err)
		return
	}
	if err := taskplan.StorePlan(a.store, task.ID, orchestrationPlanSteps(plan.Steps)); err != nil {
		_ = a.store.DeleteTask(task.ID)
		writeError(w, http.StatusInternalServerError, "ORCHESTRATION_PLAN_STORE_FAILED", err.Error(), nil)
		return
	}
	task, err = a.tasks.StartExistingWithLanguage(task, lang, func(ctx context.Context, log worker.Logger) error {
		return svc.RunAction(ctx, req, lang, orchestrationWorkerLogger{Logger: log})
	})
	if err == nil {
		a.audit(r, "orchestration.action."+plan.Action.Operation, target, "running", task.ID)
	}
	respondTask(w, task, err)
}

func orchestrationPlanSteps(steps []orchestration.PlanStep) []taskplan.Step {
	out := make([]taskplan.Step, 0, len(steps))
	for _, step := range steps {
		out = append(out, taskplan.Step{
			Target: step.Target,
			Name:   step.Name,
			Title:  step.Title,
			Order:  step.Order,
		})
	}
	return out
}

type orchestrationWorkerLogger struct {
	worker.Logger
}

func (l orchestrationWorkerLogger) TargetLogger(target string) orchestration.Logger {
	return orchestrationWorkerLogger{Logger: l.Logger.Target(target)}
}
