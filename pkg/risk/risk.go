package risk

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/sirosfoundation/go-grc/pkg/audit"
)

// Risk status constants.
const (
	StatusAccepted    = "accepted"
	StatusTransferred = "transferred"
	StatusMonitoring  = "monitoring"
	StatusDraft       = "draft" // proposed entry awaiting a treatment decision
)

// Treatment action status constants.
const (
	TreatmentOpen       = "open"
	TreatmentInProgress = "in_progress"
	TreatmentDone       = "done"
)

// ValidTreatmentStatuses lists valid treatment action statuses.
var ValidTreatmentStatuses = map[string]bool{
	TreatmentOpen:       true,
	TreatmentInProgress: true,
	TreatmentDone:       true,
}

// TreatmentAction records what is done to treat a risk, who does it, and its
// status (and completion date once done).
type TreatmentAction struct {
	Action        string `yaml:"action" json:"action"`                                     // what is being done (required)
	Responsible   string `yaml:"responsible" json:"responsible"`                           // person or role doing it (required)
	Status        string `yaml:"status" json:"status"`                                     // open | in_progress | done (required)
	DueDate       string `yaml:"due_date,omitempty" json:"due_date,omitempty"`             // YYYY-MM-DD target date (optional)
	CompletedDate string `yaml:"completed_date,omitempty" json:"completed_date,omitempty"` // YYYY-MM-DD; required when done, forbidden otherwise
}

// RegisterHeader holds metadata for a risk register file.
type RegisterHeader struct {
	ID         string `yaml:"id"`
	Title      string `yaml:"title"`
	Owner      string `yaml:"owner"` // team owning the register: platform | operator | foundation
	LastReview string `yaml:"last_review"`
	NextReview string `yaml:"next_review"`
}

// Decision records the formal risk acceptance decision.
type Decision struct {
	Date           string `yaml:"date" json:"date"`
	Rationale      string `yaml:"rationale" json:"rationale"`
	Reviewer       string `yaml:"reviewer" json:"reviewer"`
	ReviewInterval string `yaml:"review_interval" json:"review_interval"` // quarterly | annually | etc.

	// OwnerAcceptedDate (YYYY-MM-DD) is when the risk owner accepted the
	// residual risk and approved the treatment plan. Required for accepted
	// risks; distinct from Date (the decision) and Reviewer (the CISO).
	OwnerAcceptedDate string `yaml:"owner_accepted_date,omitempty" json:"owner_accepted_date,omitempty"`
}

// Risk represents a single risk register entry (accepted, transferred,
// monitoring, or a draft proposal awaiting a treatment decision).
type Risk struct {
	ID                   string           `yaml:"id"`
	Finding              string           `yaml:"finding"`            // finding ID
	Profiles             []string         `yaml:"profiles,omitempty"` // empty = all profiles
	Title                string           `yaml:"title"`
	Owner                string           `yaml:"owner"`                         // person or role accountable for the risk (required)
	Consequence          string           `yaml:"consequence,omitempty"`         // low | medium | high | critical
	Likelihood           string           `yaml:"likelihood,omitempty"`          // unlikely | possible | likely, before compensating controls
	ResidualLikelihood   string           `yaml:"residual_likelihood,omitempty"` // same scale, after compensating controls
	Severity             string           `yaml:"severity"`                      // original severity
	ResidualSeverity     string           `yaml:"residual_severity"`             // after compensating controls
	Status               string           `yaml:"status"`                        // accepted | transferred | monitoring | draft
	Description          string           `yaml:"description"`
	CompensatingControls []string         `yaml:"compensating_controls"`
	ResidualRisk         string           `yaml:"residual_risk"`
	Decision             Decision         `yaml:"decision"`
	TreatmentAction      *TreatmentAction `yaml:"treatment_action,omitempty"` // required unless status is draft
	Tracking             *audit.IssueRef  `yaml:"tracking,omitempty"`
}

// RegisterFile is the top-level structure of a risk register YAML file.
type RegisterFile struct {
	Register RegisterHeader `yaml:"risk_register"`
	Risks    []Risk         `yaml:"risks"`
}

// RiskSet holds all loaded risk register data.
type RiskSet struct {
	Files          []LoadedFile
	RisksByID      map[string]*RiskRef
	RisksByFinding map[string][]*RiskRef // finding ID -> risks
}

// LoadedFile is a parsed risk register file.
type LoadedFile struct {
	Path string
	Data RegisterFile
}

// RiskRef points to a risk within a loaded file.
type RiskRef struct {
	File *LoadedFile
	Risk *Risk
}

// Load reads all risk register YAML files from the given directory.
func Load(riskDir string, files []string) (*RiskSet, error) {
	set := &RiskSet{
		RisksByID:      make(map[string]*RiskRef),
		RisksByFinding: make(map[string][]*RiskRef),
	}

	if riskDir == "" {
		return set, nil
	}

	for _, name := range files {
		path := filepath.Join(riskDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue // file not yet created
			}
			return nil, fmt.Errorf("reading %s: %w", name, err)
		}

		var rf RegisterFile
		if err := yaml.Unmarshal(data, &rf); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}

		lf := LoadedFile{Path: path, Data: rf}
		set.Files = append(set.Files, lf)

		file := &set.Files[len(set.Files)-1]
		for i := range file.Data.Risks {
			r := &file.Data.Risks[i]
			ref := &RiskRef{File: file, Risk: r}

			if _, dup := set.RisksByID[r.ID]; dup {
				return nil, fmt.Errorf("duplicate risk ID: %s in %s", r.ID, name)
			}
			set.RisksByID[r.ID] = ref
			set.RisksByFinding[r.Finding] = append(set.RisksByFinding[r.Finding], ref)
		}
	}

	return set, nil
}

// AppliesToProfile reports whether the risk applies to the given profile.
// A risk with no profiles applies to all profiles.
func (r *Risk) AppliesToProfile(profile string) bool {
	if len(r.Profiles) == 0 || profile == "" {
		return true
	}
	for _, p := range r.Profiles {
		if p == profile {
			return true
		}
	}
	return false
}

// IsOverdue reports whether the risk's next review date has passed.
func IsOverdueRegister(reg RegisterHeader) bool {
	if reg.NextReview == "" {
		return false
	}
	t, err := time.Parse("2006-01-02", reg.NextReview)
	if err != nil {
		return false
	}
	return time.Now().After(t)
}

// ValidStatuses lists valid risk statuses.
var ValidStatuses = map[string]bool{
	StatusAccepted:    true,
	StatusTransferred: true,
	StatusMonitoring:  true,
	StatusDraft:       true,
}

// levelTable maps consequence and likelihood to the resulting risk level
// (the "severity" recorded in the register).
var levelTable = map[string]map[string]string{
	"critical": {"unlikely": "high", "possible": "critical", "likely": "critical"},
	"high":     {"unlikely": "medium", "possible": "high", "likely": "critical"},
	"medium":   {"unlikely": "low", "possible": "medium", "likely": "high"},
	"low":      {"unlikely": "low", "possible": "low", "likely": "medium"},
}

// Level returns the risk level for a consequence and likelihood, and whether
// both values are valid.
func Level(consequence, likelihood string) (string, bool) {
	level, ok := levelTable[consequence][likelihood]
	return level, ok
}

// AssessmentProblems checks the likelihood assessment against the risk
// methodology's derivation: when any of consequence, likelihood or
// residual_likelihood is recorded, all must be valid and severity and
// residual_severity must equal the level derived from them. It returns nothing
// for a risk that records no likelihood assessment at all.
func (r *Risk) AssessmentProblems() []string {
	if r.Consequence == "" && r.Likelihood == "" && r.ResidualLikelihood == "" {
		return nil
	}
	var problems []string
	level, ok := Level(r.Consequence, r.Likelihood)
	if !ok {
		problems = append(problems, fmt.Sprintf("invalid consequence %q / likelihood %q", r.Consequence, r.Likelihood))
	} else if level != r.Severity {
		problems = append(problems, fmt.Sprintf("severity %q does not match %s consequence x %s likelihood (= %s)", r.Severity, r.Consequence, r.Likelihood, level))
	}
	rlevel, ok := Level(r.Consequence, r.ResidualLikelihood)
	if !ok {
		problems = append(problems, fmt.Sprintf("invalid consequence %q / residual_likelihood %q", r.Consequence, r.ResidualLikelihood))
	} else if rlevel != r.ResidualSeverity {
		problems = append(problems, fmt.Sprintf("residual_severity %q does not match %s consequence x %s residual likelihood (= %s)", r.ResidualSeverity, r.Consequence, r.ResidualLikelihood, rlevel))
	}
	return problems
}

// DecisionProblems checks the decision record: an accepted risk must record
// when its owner accepted the residual risk, and any recorded date must be a
// valid YYYY-MM-DD date.
func (r *Risk) DecisionProblems() []string {
	var problems []string
	d := r.Decision.OwnerAcceptedDate
	switch {
	case d == "" && r.Status == StatusAccepted:
		problems = append(problems, "accepted risk is missing decision.owner_accepted_date")
	case d != "":
		if _, err := time.Parse("2006-01-02", d); err != nil {
			problems = append(problems, fmt.Sprintf("decision.owner_accepted_date %q is not a YYYY-MM-DD date", d))
		}
	}
	return problems
}

// TreatmentProblems checks the treatment action: every risk that is not a
// draft must record what is done, who does it and its status; a draft may omit
// the block but is validated when present. Dates must be YYYY-MM-DD, a done
// action must record completed_date, and any other status must not.
func (r *Risk) TreatmentProblems() []string {
	t := r.TreatmentAction
	if t == nil {
		if r.Status == StatusDraft {
			return nil
		}
		return []string{"missing treatment_action"}
	}
	var problems []string
	if strings.TrimSpace(t.Action) == "" {
		problems = append(problems, "treatment_action.action is missing")
	}
	if strings.TrimSpace(t.Responsible) == "" {
		problems = append(problems, "treatment_action.responsible is missing")
	}
	if !ValidTreatmentStatuses[t.Status] {
		problems = append(problems, fmt.Sprintf("treatment_action.status %q is invalid (want open, in_progress or done)", t.Status))
	}
	for name, d := range map[string]string{"due_date": t.DueDate, "completed_date": t.CompletedDate} {
		if d == "" {
			continue
		}
		if _, err := time.Parse("2006-01-02", d); err != nil {
			problems = append(problems, fmt.Sprintf("treatment_action.%s %q is not a YYYY-MM-DD date", name, d))
		}
	}
	switch {
	case t.Status == TreatmentDone && t.CompletedDate == "":
		problems = append(problems, "treatment_action.status is done but completed_date is missing")
	case t.Status != TreatmentDone && t.CompletedDate != "":
		problems = append(problems, "treatment_action.completed_date is set but status is not done")
	}
	sort.Strings(problems)
	return problems
}
