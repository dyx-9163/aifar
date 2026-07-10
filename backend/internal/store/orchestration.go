package store

import (
	"database/sql"
	"strings"
	"time"
)

func (s *Store) SaveOrchestrationPackageWithActions(pkg OrchestrationPackage, actions []OrchestrationAction) (OrchestrationPackageWithActions, error) {
	now := time.Now()
	pkg.Name = strings.TrimSpace(pkg.Name)
	pkg.Version = strings.TrimSpace(pkg.Version)
	if pkg.ID == "" {
		pkg.ID = NewID("orchpkg")
	}
	if pkg.Status == "" {
		pkg.Status = "published"
	}
	if pkg.TitleJSON == "" {
		pkg.TitleJSON = "{}"
	}
	if pkg.DescriptionJSON == "" {
		pkg.DescriptionJSON = "{}"
	}
	if pkg.CreatedAt.IsZero() {
		pkg.CreatedAt = now
	}
	if pkg.PublishedAt.IsZero() && pkg.Status == "published" {
		pkg.PublishedAt = now
	}
	pkg.UpdatedAt = now

	tx, err := s.db.Begin()
	if err != nil {
		return OrchestrationPackageWithActions{}, err
	}
	defer tx.Rollback()

	var existingID string
	var existingCreatedAt time.Time
	err = tx.QueryRow(`select id, created_at from orchestration_packages where name=? and version=?`, pkg.Name, pkg.Version).Scan(&existingID, &existingCreatedAt)
	if err != nil && err != sql.ErrNoRows {
		return OrchestrationPackageWithActions{}, err
	}
	if err == nil {
		pkg.ID = existingID
		pkg.CreatedAt = existingCreatedAt
	}

	_, err = tx.Exec(`insert into orchestration_packages(id,name,version,title_json,description_json,status,package_path,package_dir,package_sha256,spec_json,source_filename,uploaded_by,created_at,updated_at,published_at)
		values(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		on conflict(name,version) do update set
		title_json=excluded.title_json,description_json=excluded.description_json,status=excluded.status,
		package_path=excluded.package_path,package_dir=excluded.package_dir,package_sha256=excluded.package_sha256,
		spec_json=excluded.spec_json,source_filename=excluded.source_filename,uploaded_by=excluded.uploaded_by,
		updated_at=excluded.updated_at,published_at=excluded.published_at`,
		pkg.ID, pkg.Name, pkg.Version, pkg.TitleJSON, pkg.DescriptionJSON, pkg.Status, pkg.PackagePath, pkg.PackageDir, pkg.PackageSHA256, pkg.SpecJSON, pkg.SourceFilename, pkg.UploadedBy, pkg.CreatedAt, pkg.UpdatedAt, nullableTime(pkg.PublishedAt))
	if err != nil {
		return OrchestrationPackageWithActions{}, err
	}
	if _, err := tx.Exec(`delete from orchestration_action_steps where action_id in (select id from orchestration_actions where package_id=?)`, pkg.ID); err != nil {
		return OrchestrationPackageWithActions{}, err
	}
	if _, err := tx.Exec(`delete from orchestration_actions where package_id=?`, pkg.ID); err != nil {
		return OrchestrationPackageWithActions{}, err
	}
	savedActions := make([]OrchestrationAction, 0, len(actions))
	for _, action := range actions {
		action.PackageID = pkg.ID
		action.Name = strings.TrimSpace(action.Name)
		if action.ID == "" {
			action.ID = NewID("orchact")
		}
		if action.TitleJSON == "" {
			action.TitleJSON = "{}"
		}
		if action.DescriptionJSON == "" {
			action.DescriptionJSON = "{}"
		}
		if action.Operation == "" {
			action.Operation = "check"
		}
		if action.TargetMode == "" {
			action.TargetMode = "multiple"
		}
		if action.MinTargets <= 0 {
			action.MinTargets = 1
		}
		if action.WorkflowJSON == "" {
			action.WorkflowJSON = "{}"
		}
		if action.TimeoutSeconds <= 0 {
			action.TimeoutSeconds = 60
		}
		if action.StagesJSON == "" {
			action.StagesJSON = "[]"
		}
		if len(action.Steps) > 0 {
			if action.ScriptPath == "" {
				action.ScriptPath = action.Steps[0].ScriptPath
			}
			if action.ScriptSHA256 == "" {
				action.ScriptSHA256 = action.Steps[0].ScriptSHA256
			}
		}
		if action.CreatedAt.IsZero() {
			action.CreatedAt = now
		}
		action.UpdatedAt = now
		_, err := tx.Exec(`insert into orchestration_actions(id,package_id,name,title_json,description_json,operation,script_path,script_sha256,target_mode,topology,min_targets,max_targets,read_only,timeout_seconds,stages_json,workflow_json,created_at,updated_at)
			values(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			action.ID, action.PackageID, action.Name, action.TitleJSON, action.DescriptionJSON, action.Operation,
			action.ScriptPath, action.ScriptSHA256, action.TargetMode, action.Topology, action.MinTargets,
			action.MaxTargets, boolInt(action.ReadOnly), action.TimeoutSeconds, action.StagesJSON,
			action.WorkflowJSON, action.CreatedAt, action.UpdatedAt)
		if err != nil {
			return OrchestrationPackageWithActions{}, err
		}
		savedSteps := make([]OrchestrationActionStep, 0, len(action.Steps))
		for index, step := range action.Steps {
			step.ActionID = action.ID
			step.Name = strings.TrimSpace(step.Name)
			if step.ID == "" {
				step.ID = NewID("orchstep")
			}
			if step.TitleJSON == "" {
				step.TitleJSON = "{}"
			}
			if step.TimeoutSeconds <= 0 {
				step.TimeoutSeconds = action.TimeoutSeconds
			}
			if step.Order <= 0 {
				step.Order = index + 1
			}
			if step.CreatedAt.IsZero() {
				step.CreatedAt = now
			}
			step.UpdatedAt = now
			_, err := tx.Exec(`insert into orchestration_action_steps(id,action_id,name,title_json,script_path,script_sha256,read_only,timeout_seconds,step_order,created_at,updated_at)
				values(?,?,?,?,?,?,?,?,?,?,?)`,
				step.ID, step.ActionID, step.Name, step.TitleJSON, step.ScriptPath, step.ScriptSHA256, boolInt(step.ReadOnly), step.TimeoutSeconds, step.Order, step.CreatedAt, step.UpdatedAt)
			if err != nil {
				return OrchestrationPackageWithActions{}, err
			}
			savedSteps = append(savedSteps, step)
		}
		action.Steps = savedSteps
		savedActions = append(savedActions, action)
	}
	if err := tx.Commit(); err != nil {
		return OrchestrationPackageWithActions{}, err
	}
	return OrchestrationPackageWithActions{Package: pkg, Actions: savedActions}, nil
}

func (s *Store) ListOrchestrationPackages() ([]OrchestrationPackageWithActions, error) {
	rows, err := s.db.Query(`select id,name,version,title_json,description_json,status,package_path,package_dir,package_sha256,spec_json,coalesce(source_filename,''),coalesce(uploaded_by,''),created_at,updated_at,published_at
		from orchestration_packages order by updated_at desc, name, version`)
	if err != nil {
		return nil, err
	}
	packages := []OrchestrationPackage{}
	for rows.Next() {
		pkg, err := scanOrchestrationPackage(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		packages = append(packages, pkg)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	out := make([]OrchestrationPackageWithActions, 0, len(packages))
	for _, pkg := range packages {
		actions, err := s.listOrchestrationActionsByPackage(pkg.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, OrchestrationPackageWithActions{Package: pkg, Actions: actions})
	}
	return out, nil
}

func (s *Store) ListPublishedOrchestrationActions() ([]OrchestrationActionWithPackage, error) {
	rows, err := s.db.Query(`select
		a.id,a.package_id,a.name,a.title_json,a.description_json,a.operation,a.script_path,a.script_sha256,a.target_mode,a.topology,a.min_targets,a.max_targets,a.read_only,a.timeout_seconds,a.stages_json,a.workflow_json,a.created_at,a.updated_at,
		p.id,p.name,p.version,p.title_json,p.description_json,p.status,p.package_path,p.package_dir,p.package_sha256,p.spec_json,coalesce(p.source_filename,''),coalesce(p.uploaded_by,''),p.created_at,p.updated_at,p.published_at
		from orchestration_actions a
		join orchestration_packages p on p.id=a.package_id
		where p.status='published'
		order by p.updated_at desc, p.name, a.name`)
	if err != nil {
		return nil, err
	}
	out := []OrchestrationActionWithPackage{}
	for rows.Next() {
		item, err := scanOrchestrationActionWithPackage(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range out {
		steps, err := s.listOrchestrationActionSteps(out[index].Action.ID)
		if err != nil {
			return nil, err
		}
		out[index].Action.Steps = steps
	}
	return out, nil
}

func (s *Store) GetPublishedOrchestrationAction(id string) (OrchestrationActionWithPackage, error) {
	row := s.db.QueryRow(`select
		a.id,a.package_id,a.name,a.title_json,a.description_json,a.operation,a.script_path,a.script_sha256,a.target_mode,a.topology,a.min_targets,a.max_targets,a.read_only,a.timeout_seconds,a.stages_json,a.workflow_json,a.created_at,a.updated_at,
		p.id,p.name,p.version,p.title_json,p.description_json,p.status,p.package_path,p.package_dir,p.package_sha256,p.spec_json,coalesce(p.source_filename,''),coalesce(p.uploaded_by,''),p.created_at,p.updated_at,p.published_at
		from orchestration_actions a
		join orchestration_packages p on p.id=a.package_id
		where a.id=? and p.status='published'`, strings.TrimSpace(id))
	item, err := scanOrchestrationActionWithPackage(row)
	if err != nil {
		return item, err
	}
	item.Action.Steps, err = s.listOrchestrationActionSteps(item.Action.ID)
	return item, err
}

func (s *Store) listOrchestrationActionsByPackage(packageID string) ([]OrchestrationAction, error) {
	rows, err := s.db.Query(`select id,package_id,name,title_json,description_json,operation,script_path,script_sha256,target_mode,topology,min_targets,max_targets,read_only,timeout_seconds,stages_json,workflow_json,created_at,updated_at
		from orchestration_actions where package_id=? order by name`, packageID)
	if err != nil {
		return nil, err
	}
	out := []OrchestrationAction{}
	for rows.Next() {
		action, err := scanOrchestrationAction(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, action)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range out {
		steps, err := s.listOrchestrationActionSteps(out[index].ID)
		if err != nil {
			return nil, err
		}
		out[index].Steps = steps
	}
	return out, nil
}

func (s *Store) listOrchestrationActionSteps(actionID string) ([]OrchestrationActionStep, error) {
	rows, err := s.db.Query(`select id,action_id,name,title_json,script_path,script_sha256,read_only,timeout_seconds,step_order,created_at,updated_at
		from orchestration_action_steps where action_id=? order by step_order, name`, actionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OrchestrationActionStep{}
	for rows.Next() {
		step, err := scanOrchestrationActionStep(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, step)
	}
	return out, rows.Err()
}

type orchestrationPackageScanner interface {
	Scan(dest ...any) error
}

func scanOrchestrationPackage(scanner orchestrationPackageScanner) (OrchestrationPackage, error) {
	var pkg OrchestrationPackage
	var publishedAt sql.NullTime
	err := scanner.Scan(&pkg.ID, &pkg.Name, &pkg.Version, &pkg.TitleJSON, &pkg.DescriptionJSON, &pkg.Status, &pkg.PackagePath, &pkg.PackageDir, &pkg.PackageSHA256, &pkg.SpecJSON, &pkg.SourceFilename, &pkg.UploadedBy, &pkg.CreatedAt, &pkg.UpdatedAt, &publishedAt)
	pkg.PublishedAt = nullTime(publishedAt)
	return pkg, err
}

func scanOrchestrationAction(scanner orchestrationPackageScanner) (OrchestrationAction, error) {
	var action OrchestrationAction
	var readOnly int
	err := scanner.Scan(&action.ID, &action.PackageID, &action.Name, &action.TitleJSON, &action.DescriptionJSON,
		&action.Operation, &action.ScriptPath, &action.ScriptSHA256, &action.TargetMode, &action.Topology,
		&action.MinTargets, &action.MaxTargets, &readOnly, &action.TimeoutSeconds, &action.StagesJSON,
		&action.WorkflowJSON, &action.CreatedAt, &action.UpdatedAt)
	action.ReadOnly = readOnly != 0
	return action, err
}

func scanOrchestrationActionStep(scanner orchestrationPackageScanner) (OrchestrationActionStep, error) {
	var step OrchestrationActionStep
	var readOnly int
	err := scanner.Scan(&step.ID, &step.ActionID, &step.Name, &step.TitleJSON, &step.ScriptPath, &step.ScriptSHA256, &readOnly, &step.TimeoutSeconds, &step.Order, &step.CreatedAt, &step.UpdatedAt)
	step.ReadOnly = readOnly != 0
	return step, err
}

func scanOrchestrationActionWithPackage(scanner orchestrationPackageScanner) (OrchestrationActionWithPackage, error) {
	var item OrchestrationActionWithPackage
	var readOnly int
	var publishedAt sql.NullTime
	err := scanner.Scan(
		&item.Action.ID, &item.Action.PackageID, &item.Action.Name, &item.Action.TitleJSON, &item.Action.DescriptionJSON,
		&item.Action.Operation, &item.Action.ScriptPath, &item.Action.ScriptSHA256, &item.Action.TargetMode,
		&item.Action.Topology, &item.Action.MinTargets, &item.Action.MaxTargets, &readOnly,
		&item.Action.TimeoutSeconds, &item.Action.StagesJSON, &item.Action.WorkflowJSON,
		&item.Action.CreatedAt, &item.Action.UpdatedAt,
		&item.Package.ID, &item.Package.Name, &item.Package.Version, &item.Package.TitleJSON, &item.Package.DescriptionJSON, &item.Package.Status, &item.Package.PackagePath, &item.Package.PackageDir, &item.Package.PackageSHA256, &item.Package.SpecJSON, &item.Package.SourceFilename, &item.Package.UploadedBy, &item.Package.CreatedAt, &item.Package.UpdatedAt, &publishedAt,
	)
	item.Action.ReadOnly = readOnly != 0
	item.Package.PublishedAt = nullTime(publishedAt)
	return item, err
}
