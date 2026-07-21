// Package projectanalyzer turns Compose configuration and live Docker state
// into an evidence-based protection blueprint. It never collects secret values.
package projectanalyzer

import "time"

const SchemaVersion = 1

type Confidence string

const (
	ConfidenceConfirmed Confidence = "confirmed"
	ConfidenceProbable  Confidence = "probable"
	ConfidencePossible  Confidence = "possible"
)

type Evidence struct {
	Source  string `json:"source"`
	Subject string `json:"subject"`
	Detail  string `json:"detail"`
}

type Finding struct {
	ID             string     `json:"id"`
	Kind           string     `json:"kind"`
	Technology     string     `json:"technology"`
	Service        string     `json:"service,omitempty"`
	Confidence     Confidence `json:"confidence"`
	Evidence       []Evidence `json:"evidence"`
	Recommendation string     `json:"recommendation"`
	Consistency    string     `json:"consistency"`
	Warnings       []string   `json:"warnings,omitempty"`
	// DataMount is where this database keeps its files, when that could be
	// established. It is what lets a dump-aware backup connect a database to
	// the storage behind it instead of treating the two as unrelated findings.
	DataMount *DataMount `json:"dataMount,omitempty"`
}

// DataMount identifies the storage holding a database's files.
type DataMount struct {
	Type   string `json:"type"`
	Source string `json:"source"`
	Target string `json:"target"`
}

type Step struct {
	Order       int    `json:"order"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

type Blueprint struct {
	SchemaVersion int        `json:"schemaVersion"`
	ProjectID     string     `json:"projectId"`
	Fingerprint   string     `json:"fingerprint"`
	AnalyzedAt    time.Time  `json:"analyzedAt"`
	ConfirmedAt   *time.Time `json:"confirmedAt,omitempty"`
	Drifted       bool       `json:"drifted"`
	Findings      []Finding  `json:"findings"`
	Steps         []Step     `json:"steps"`
	Warnings      []string   `json:"warnings"`
}

type ProjectDetector interface {
	ID() string
	Detect(Input) []Finding
}

type ServiceEvidence struct {
	Name             string
	Image            string
	EnvironmentNames []string
	Mounts           []MountEvidence
}

type MountEvidence struct {
	Type, Source, Target string
}

type Input struct {
	ProjectID string
	Services  []ServiceEvidence
	Secrets   []string
	Configs   []string
	EnvFiles  []string
	SQLite    []string
	Warnings  []string
}
