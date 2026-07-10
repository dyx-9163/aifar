package store

import (
	"path/filepath"
	"testing"
)

func TestListOrchestrationPackagesIncludesActions(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "aifar.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.SaveOrchestrationPackageWithActions(OrchestrationPackage{
		Name:            "host-readonly-check",
		Version:         "1.0.0",
		TitleJSON:       `{"en":"Host Readonly Check"}`,
		DescriptionJSON: `{"en":"Readonly check"}`,
		PackagePath:     "packages/host-readonly-check.zip",
		PackageDir:      "packages/host-readonly-check",
		PackageSHA256:   "sha256",
		SpecJSON:        `{}`,
		SourceFilename:  "deployspec.zip",
		UploadedBy:      "admin",
	}, []OrchestrationAction{{
		Name:           "check",
		Operation:      "check",
		ScriptPath:     "scripts/check.sh",
		ScriptSHA256:   "script-sha256",
		TargetMode:     "multiple",
		ReadOnly:       true,
		TimeoutSeconds: 60,
		StagesJSON:     `[]`,
		Steps: []OrchestrationActionStep{{
			Name:           "check-host",
			TitleJSON:      `{"en":"Check host"}`,
			ScriptPath:     "scripts/check.sh",
			ScriptSHA256:   "script-sha256",
			ReadOnly:       true,
			TimeoutSeconds: 60,
			Order:          1,
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}

	got, err := db.ListOrchestrationPackages()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected one package, got %d", len(got))
	}
	if len(got[0].Actions) != 1 || got[0].Actions[0].Name != "check" {
		t.Fatalf("expected check action, got %+v", got[0].Actions)
	}
	if len(got[0].Actions[0].Steps) != 1 || got[0].Actions[0].Steps[0].Name != "check-host" {
		t.Fatalf("expected check step, got %+v", got[0].Actions[0].Steps)
	}
}
