package risk

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"gopkg.in/yaml.v3"
)

// Risk status constants.
const (
	StatusAccepted    = "accepted"
	StatusTransferred = "transferred"
	StatusMonitoring  = "monitoring"
)

// RegisterHeader holds metadata for a risk register file.
type RegisterHeader struct {
	ID         string `yaml:"id"`
	Title      string `yaml:"title"`
	Owner      string `yaml:"owner"` // platform | operator
	LastReview string `yaml:"last_review"`
	NextReview string `yaml:"next_review"`
}

// Decision records the formal risk acceptance decision.
type Decision struct {
	Date           string `yaml:"date"`
	Rationale      string `yaml:"rationale"`
	Reviewer       string `yaml:"reviewer"`
	ReviewInterval string `yaml:"review_interval"` // quarterly | annually | etc.
}

// Risk represents a single accepted/transferred risk entry.
type Risk struct {
	ID                   string          `yaml:"id"`
	Finding              string          `yaml:"finding"`            // finding ID
	Profiles             []string        `yaml:"profiles,omitempty"` // empty = all profiles
	Title                string          `yaml:"title"`
	Severity             string          `yaml:"severity"`          // original severity
	ResidualSeverity     string          `yaml:"residual_severity"` // after compensating controls
	Status               string          `yaml:"status"`            // accepted | transferred | monitoring
	Description          string          `yaml:"description"`
	CompensatingControls []string        `yaml:"compensating_controls"`
	ResidualRisk         string          `yaml:"residual_risk"`
	Decision             Decision        `yaml:"decision"`
	Tracking             *audit.IssueRef `yaml:"tracking,omitempty"`
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
}
