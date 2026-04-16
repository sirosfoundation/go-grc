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

type Finding struct {
	ID            string     `yaml:"id"`
	Title         string     `yaml:"title"`
	Severity      string     `yaml:"severity"`
	Status        string     `yaml:"status"`
	Owner         string     `yaml:"owner"`
	Controls      []string   `yaml:"controls"`
	Description   string     `yaml:"description"`
	EUDIReqs      []string   `yaml:"eudi_reqs,omitempty"`
	AnnexA        []string   `yaml:"annex_a,omitempty"`
	GDPRItems     []string   `yaml:"gdpr_items,omitempty"`
	TrackingIssue *IssueRef  `yaml:"tracking_issue,omitempty"`
	Issues        []IssueRef `yaml:"issues,omitempty"`
	PullRequests  []IssueRef `yaml:"pull_requests,omitempty"`
	Evidence      []Evidence `yaml:"evidence,omitempty"`
	ResolvedDate  string     `yaml:"resolved_date,omitempty"`
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

type LoadedFile struct {
	Path string
	Data AuditFile
}

type FindingRef struct {
	File    *LoadedFile
	Index   int
	Finding *Finding
}

func Load(auditsDir string) (*AuditSet, error) {
	set := &AuditSet{
		FindingsByID:      make(map[string]*FindingRef),
		FindingsByControl: make(map[string][]*FindingRef),
	}

	entries, err := os.ReadDir(auditsDir)
	if err != nil {
		return nil, fmt.Errorf("reading audits dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}

		path := filepath.Join(auditsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", entry.Name(), err)
		}

		var af AuditFile
		if err := yaml.Unmarshal(data, &af); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", entry.Name(), err)
		}

		lf := LoadedFile{Path: path, Data: af}
		set.Files = append(set.Files, lf)

		file := &set.Files[len(set.Files)-1]
		for i := range file.Data.Findings {
			f := &file.Data.Findings[i]
			ref := &FindingRef{File: file, Index: i, Finding: f}

			if _, dup := set.FindingsByID[f.ID]; dup {
				return nil, fmt.Errorf("duplicate finding ID: %s in %s", f.ID, entry.Name())
			}
			set.FindingsByID[f.ID] = ref

			for _, ctrlID := range f.Controls {
				set.FindingsByControl[ctrlID] = append(set.FindingsByControl[ctrlID], ref)
			}
		}
	}

	return set, nil
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

func (f *Finding) IsResolved() bool {
	return f.Status == "resolved"
}

func (f *Finding) HasEvidence() bool {
	return len(f.Evidence) > 0
}
