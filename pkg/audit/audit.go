package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

type Audit struct {
	ID        string `yaml:"id"`
	Title     string `yaml:"title"`
	Date      string `yaml:"date"`
	Assurance string `yaml:"assurance"`
	Scope     string `yaml:"scope"`
	Method    string `yaml:"method"`
}

type IssueRef struct {
	Repo   string `yaml:"repo"`
	Number int    `yaml:"number"`
}

type Evidence struct {
	Type        string `yaml:"type"`
	Ref         string `yaml:"ref"`
	Description string `yaml:"description"`
	CollectedAt string `yaml:"collected_at,omitempty"`
}

// ProfileOverride holds per-profile status and evidence overrides for a finding.
type ProfileOverride struct {
	Status       string     `yaml:"status,omitempty"`
	Severity     string     `yaml:"severity,omitempty"`
	Evidence     []Evidence `yaml:"evidence,omitempty"`
	ResolvedDate string     `yaml:"resolved_date,omitempty"`
}

type Finding struct {
	ID            string                     `yaml:"id"`
	Title         string                     `yaml:"title"`
	Severity      string                     `yaml:"severity"`
	Status        string                     `yaml:"status"`
	Owner         string                     `yaml:"owner"`
	Controls      []string                   `yaml:"controls"`
	Description   string                     `yaml:"description"`
	EUDIReqs      []string                   `yaml:"eudi_reqs,omitempty"`
	AnnexA        []string                   `yaml:"annex_a,omitempty"`
	GDPRItems     []string                   `yaml:"gdpr_items,omitempty"`
	ASVSSections  []string                   `yaml:"asvs_sections,omitempty"`
	FrameworkRefs map[string][]string        `yaml:"framework_refs,omitempty"` // framework ID -> requirement IDs (generic)
	TrackingIssue *IssueRef                  `yaml:"tracking_issue,omitempty"`
	Issues        []IssueRef                 `yaml:"issues,omitempty"`
	PullRequests  []IssueRef                 `yaml:"pull_requests,omitempty"`
	Evidence      []Evidence                 `yaml:"evidence,omitempty"`
	ResolvedDate  string                     `yaml:"resolved_date,omitempty"`
	Profiles      map[string]ProfileOverride `yaml:"profiles,omitempty"`
}

type AuditFile struct {
	Audit    Audit     `yaml:"audit"`
	Findings []Finding `yaml:"findings"`
}

type AuditSet struct {
	Files             []LoadedFile
	FindingsByID      map[string]*FindingRef
	FindingsByControl map[string][]*FindingRef
}

// Kind says whether a findings file records a real audit or an internal
// self-assessment. Only the former may ever be shown publicly.
type Kind string

const (
	// KindAudit is a real audit cycle (certification or surveillance audit).
	KindAudit Kind = "audit"
	// KindAssessment is an internal self-assessment, never public.
	KindAssessment Kind = "assessment"
)

type LoadedFile struct {
	Path string
	Kind Kind
	Data AuditFile
}

type FindingRef struct {
	File    *LoadedFile
	Index   int
	Finding *Finding
}

// Load reads real audits from auditsDir.
func Load(auditsDir string) (*AuditSet, error) {
	return LoadDirs(auditsDir, "")
}

// LoadDirs reads real audits from auditsDir and internal self-assessments
// from assessmentsDir (skipped when empty or absent), tagging each file with
// its Kind. Finding IDs must be unique across both.
//
// In either directory a findings file is a "*.yaml" directly in the
// directory, or the "audit.yaml" of a per-cycle subdirectory
// ("<dir>/<cycle>/audit.yaml"). Other files in a cycle directory, such as
// "soa.yaml" and "references/", are not findings files and are ignored here.
func LoadDirs(auditsDir, assessmentsDir string) (*AuditSet, error) {
	set := &AuditSet{
		FindingsByID:      make(map[string]*FindingRef),
		FindingsByControl: make(map[string][]*FindingRef),
	}
	if err := set.loadDir(auditsDir, KindAudit, true); err != nil {
		return nil, err
	}
	if assessmentsDir != "" {
		if err := set.loadDir(assessmentsDir, KindAssessment, false); err != nil {
			return nil, err
		}
	}
	return set, nil
}

func (set *AuditSet) loadDir(dir string, kind Kind, required bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !required && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading %s dir: %w", kind, err)
	}

	for _, entry := range entries {
		var path string
		switch {
		case entry.IsDir():
			path = filepath.Join(dir, entry.Name(), "audit.yaml")
			if _, err := os.Stat(path); err != nil {
				continue
			}
		case filepath.Ext(entry.Name()) == ".yaml":
			path = filepath.Join(dir, entry.Name())
		default:
			continue
		}
		if err := set.loadFile(path, kind); err != nil {
			return err
		}
	}
	return nil
}

func (set *AuditSet) loadFile(path string, kind Kind) error {
	name, _ := filepath.Rel(filepath.Dir(filepath.Dir(path)), path)
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", name, err)
	}

	var af AuditFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		return fmt.Errorf("parsing %s: %w", name, err)
	}

	set.Files = append(set.Files, LoadedFile{Path: path, Kind: kind, Data: af})
	file := &set.Files[len(set.Files)-1]
	for i := range file.Data.Findings {
		f := &file.Data.Findings[i]
		ref := &FindingRef{File: file, Index: i, Finding: f}

		if _, dup := set.FindingsByID[f.ID]; dup {
			return fmt.Errorf("duplicate finding ID: %s in %s", f.ID, name)
		}
		set.FindingsByID[f.ID] = ref

		for _, ctrlID := range f.Controls {
			set.FindingsByControl[ctrlID] = append(set.FindingsByControl[ctrlID], ref)
		}
	}
	return nil
}

// AuditsOnly returns a view of the set restricted to real audits, with every
// internal assessment removed. Anything that renders for a public audience
// must go through it, so assessments cannot leak by omission.
func (set *AuditSet) AuditsOnly() *AuditSet {
	out := &AuditSet{
		FindingsByID:      make(map[string]*FindingRef),
		FindingsByControl: make(map[string][]*FindingRef),
	}
	for i := range set.Files {
		if set.Files[i].Kind == KindAudit {
			out.Files = append(out.Files, set.Files[i])
		}
	}
	for id, ref := range set.FindingsByID {
		if ref.File.Kind == KindAudit {
			out.FindingsByID[id] = ref
		}
	}
	for ctrl, refs := range set.FindingsByControl {
		for _, ref := range refs {
			if ref.File.Kind == KindAudit {
				out.FindingsByControl[ctrl] = append(out.FindingsByControl[ctrl], ref)
			}
		}
	}
	return out
}

func (lf *LoadedFile) Save() error {
	data, err := yaml.Marshal(&lf.Data)
	if err != nil {
		return fmt.Errorf("marshaling %s: %w", filepath.Base(lf.Path), err)
	}
	return os.WriteFile(lf.Path, data, 0644)
}

func (f *Finding) AddEvidence(ev Evidence) {
	if ev.CollectedAt == "" {
		ev.CollectedAt = time.Now().UTC().Format("2006-01-02")
	}
	f.Evidence = append(f.Evidence, ev)
}

// MatchesReq reports whether the finding references the given requirement
// in the specified framework. Checks both the generic FrameworkRefs map
// and the legacy per-framework fields for backward compatibility.
func (f *Finding) MatchesReq(fwID, reqID string) bool {
	if refs, ok := f.FrameworkRefs[fwID]; ok {
		for _, r := range refs {
			if r == reqID {
				return true
			}
		}
	}
	var legacy []string
	switch fwID {
	case "eudi":
		legacy = f.EUDIReqs
	case "iso27001":
		legacy = f.AnnexA
	case "gdpr":
		legacy = f.GDPRItems
	case "owasp-asvs":
		legacy = f.ASVSSections
	}
	for _, r := range legacy {
		if r == reqID {
			return true
		}
	}
	return false
}

func (f *Finding) IsResolved() bool {
	return f.Status == StatusResolved
}

func (f *Finding) HasEvidence() bool {
	return len(f.Evidence) > 0
}

// StatusForProfile returns the effective status for the given profile.
// An empty profile name returns the default (top-level) status.
func (f *Finding) StatusForProfile(profile string) string {
	if profile == "" {
		return f.Status
	}
	if po, ok := f.Profiles[profile]; ok && po.Status != "" {
		return po.Status
	}
	return f.Status
}

// SeverityForProfile returns the effective severity for the given profile.
func (f *Finding) SeverityForProfile(profile string) string {
	if profile == "" {
		return f.Severity
	}
	if po, ok := f.Profiles[profile]; ok && po.Severity != "" {
		return po.Severity
	}
	return f.Severity
}

// EvidenceForProfile returns the combined evidence for the given profile:
// the base evidence plus any profile-specific evidence.
func (f *Finding) EvidenceForProfile(profile string) []Evidence {
	base := f.Evidence
	if profile == "" {
		return base
	}
	po, ok := f.Profiles[profile]
	if !ok || len(po.Evidence) == 0 {
		return base
	}
	combined := make([]Evidence, 0, len(base)+len(po.Evidence))
	combined = append(combined, base...)
	combined = append(combined, po.Evidence...)
	return combined
}

// IsResolvedForProfile reports whether the finding is resolved in the given profile.
func (f *Finding) IsResolvedForProfile(profile string) bool {
	return f.StatusForProfile(profile) == StatusResolved
}

// IsTerminalForProfile reports whether the finding is in a terminal state for the profile.
func (f *Finding) IsTerminalForProfile(profile string) bool {
	s := f.StatusForProfile(profile)
	return s == StatusResolved || s == StatusAccepted
}
