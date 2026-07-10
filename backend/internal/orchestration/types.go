package orchestration

import "time"

type LocalizedText map[string]string

type PackageSpec struct {
	APIVersion     string        `json:"apiVersion,omitempty"`
	Kind           string        `json:"kind,omitempty"`
	Name           string        `json:"name"`
	Version        string        `json:"version,omitempty"`
	PackageVersion string        `json:"packageVersion,omitempty"`
	Title          LocalizedText `json:"title,omitempty"`
	Description    LocalizedText `json:"description,omitempty"`
	Actions        []ActionSpec  `json:"actions,omitempty"`
	Scripts        ScriptSet     `json:"scripts,omitempty"`
}

type ActionSpec struct {
	Name           string                `json:"name"`
	Title          LocalizedText         `json:"title,omitempty"`
	Description    LocalizedText         `json:"description,omitempty"`
	Operation      string                `json:"operation,omitempty"`
	Script         string                `json:"script,omitempty"`
	TargetMode     string                `json:"targetMode,omitempty"`
	Topology       string                `json:"topology,omitempty"`
	MinTargets     int                   `json:"minTargets,omitempty"`
	MaxTargets     int                   `json:"maxTargets,omitempty"`
	ReadOnly       *bool                 `json:"readOnly,omitempty"`
	TimeoutSeconds int                   `json:"timeoutSeconds,omitempty"`
	Stages         []string              `json:"stages,omitempty"`
	Steps          []ActionStepSpec      `json:"steps,omitempty"`
	Phases         []ActionPhaseSpec     `json:"phases,omitempty"`
	Secrets        []GeneratedSecretSpec `json:"secrets,omitempty"`
	Credential     *CredentialSpec       `json:"credential,omitempty"`
	Result         *ActionResultSpec     `json:"result,omitempty"`
}

type ActionPhaseSpec struct {
	Name  string           `json:"name"`
	Title LocalizedText    `json:"title,omitempty"`
	Scope string           `json:"scope"`
	Mode  string           `json:"mode,omitempty"`
	RunOn string           `json:"runOn,omitempty"`
	Steps []ActionStepSpec `json:"steps,omitempty"`
}

type GeneratedSecretSpec struct {
	Name      string `json:"name"`
	Env       string `json:"env"`
	Generator string `json:"generator,omitempty"`
	Bytes     int    `json:"bytes,omitempty"`
}

type CredentialSpec struct {
	Kind       string `json:"kind"`
	Username   string `json:"username,omitempty"`
	Purpose    string `json:"purpose,omitempty"`
	SecretName string `json:"secretName"`
	SecretKey  string `json:"secretKey,omitempty"`
}

type ActionResultSpec struct {
	App     string `json:"app"`
	Version string `json:"version,omitempty"`
	Status  string `json:"status,omitempty"`
	Port    int    `json:"port,omitempty"`
}

type ActionWorkflow struct {
	Phases     []ActionPhaseSpec     `json:"phases,omitempty"`
	Secrets    []GeneratedSecretSpec `json:"secrets,omitempty"`
	Credential *CredentialSpec       `json:"credential,omitempty"`
	Result     *ActionResultSpec     `json:"result,omitempty"`
}

type ActionStepSpec struct {
	Name           string        `json:"name"`
	Title          LocalizedText `json:"title,omitempty"`
	Script         string        `json:"script"`
	ReadOnly       *bool         `json:"readOnly,omitempty"`
	TimeoutSeconds int           `json:"timeoutSeconds,omitempty"`
}

type ScriptSet struct {
	Check ScriptSpec `json:"check,omitempty"`
}

type ScriptSpec struct {
	Path           string        `json:"path"`
	Title          LocalizedText `json:"title,omitempty"`
	Description    LocalizedText `json:"description,omitempty"`
	Scope          string        `json:"scope,omitempty"`
	TimeoutSeconds int           `json:"timeoutSeconds,omitempty"`
}

type PackageView struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Version        string        `json:"version"`
	Title          LocalizedText `json:"title"`
	Description    LocalizedText `json:"description"`
	Status         string        `json:"status"`
	PackageSHA256  string        `json:"packageSha256"`
	SourceFilename string        `json:"sourceFilename,omitempty"`
	UploadedBy     string        `json:"uploadedBy,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
	PublishedAt    time.Time     `json:"publishedAt,omitempty"`
	Actions        []ActionView  `json:"actions"`
}

type ActionView struct {
	ID             string            `json:"id"`
	PackageID      string            `json:"packageId"`
	PackageName    string            `json:"packageName"`
	PackageVersion string            `json:"packageVersion"`
	Name           string            `json:"name"`
	Title          LocalizedText     `json:"title"`
	Description    LocalizedText     `json:"description"`
	Operation      string            `json:"operation"`
	ScriptPath     string            `json:"scriptPath"`
	ScriptSHA256   string            `json:"scriptSha256"`
	TargetMode     string            `json:"targetMode"`
	Topology       string            `json:"topology,omitempty"`
	MinTargets     int               `json:"minTargets"`
	MaxTargets     int               `json:"maxTargets"`
	ReadOnly       bool              `json:"readOnly"`
	TimeoutSeconds int               `json:"timeoutSeconds"`
	Stages         []string          `json:"stages"`
	Steps          []ActionStepView  `json:"steps"`
	Phases         []ActionPhaseView `json:"phases"`
}

type ActionPhaseView struct {
	Name  string           `json:"name"`
	Title LocalizedText    `json:"title"`
	Scope string           `json:"scope"`
	Mode  string           `json:"mode"`
	RunOn string           `json:"runOn,omitempty"`
	Order int              `json:"order"`
	Steps []ActionStepView `json:"steps"`
}

type ActionStepView struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Title          LocalizedText `json:"title"`
	ScriptPath     string        `json:"scriptPath"`
	ScriptSHA256   string        `json:"scriptSha256"`
	ReadOnly       bool          `json:"readOnly"`
	TimeoutSeconds int           `json:"timeoutSeconds"`
	Order          int           `json:"order"`
}

type RunRequest struct {
	ActionID            string   `json:"actionId"`
	ServerIDs           []string `json:"serverIds"`
	CoordinatorServerID string   `json:"coordinatorServerId,omitempty"`
	ClusterName         string   `json:"clusterName,omitempty"`
	Language            string   `json:"language,omitempty"`
}

type DryRunResult struct {
	Action      ActionView   `json:"action"`
	GeneratedAt time.Time    `json:"generatedAt"`
	Targets     []TargetPlan `json:"targets"`
	Steps       []PlanStep   `json:"steps"`
	Issues      []PlanIssue  `json:"issues"`
}

type TargetPlan struct {
	ServerID    string `json:"serverId"`
	Name        string `json:"name"`
	Host        string `json:"host"`
	Status      string `json:"status"`
	Role        string `json:"role,omitempty"`
	Coordinator bool   `json:"coordinator,omitempty"`
	Order       int    `json:"order,omitempty"`
}

type PlanStep struct {
	Target string `json:"target"`
	Name   string `json:"name"`
	Title  string `json:"title"`
	Order  int    `json:"order"`
	Phase  string `json:"phase,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Mode   string `json:"mode,omitempty"`
}

type PlanIssue struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
}
