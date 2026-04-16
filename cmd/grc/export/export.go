package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
)

type Package struct {
	Metadata PackageMeta     `json:"metadata"`
	Controls []ControlExport `json:"controls"`
	Findings []FindingExport `json:"findings"`
}

type PackageMeta struct {
	GeneratedAt string `json:"generated_at"`
	Tool        string `json:"tool"`
}

type ControlExport struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Status   string   `json:"status"`
	Owner    string   `json:"owner"`
	Findings []string `json:"finding_ids,omitempty"`
}

type FindingExport struct {
	ID           string           `json:"id"`
	Title        string           `json:"title"`
	Severity     string           `json:"severity"`
	Status       string           `json:"status"`
	Controls     []string         `json:"controls"`
	ResolvedDate string           `json:"resolved_date,omitempty"`
	Evidence     []EvidenceExport `json:"evidence,omitempty"`
	TrackingURL  string           `json:"tracking_url,omitempty"`
}

type EvidenceExport struct {
	Type        string `json:"type"`
	Ref         string `json:"ref"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

func NewCommand() *cobra.Command {
	var outputDir string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export an auditor-ready evidence package",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root, outputDir)
		},
	}
	cmd.Flags().StringVarP(&outputDir, "output", "o", "export", "Output directory")
	return cmd
}

func run(root, outputDir string) error {
	cfg := config.New(root)

	cat, err := catalog.Load(cfg.CatalogDir, cfg.CatalogSubdirs...)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}
	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}
	// mappings loaded for completeness but not yet exported in v1
	if _, err := mapping.Load(cfg.MappingsDir); err != nil {
		return fmt.Errorf("loading mappings: %w", err)
	}

	pkg := Package{
		Metadata: PackageMeta{
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			Tool:        "grc",
		},
	}

	for _, group := range cat.Groups {
		for _, ctrl := range group.Controls {
			ce := ControlExport{ID: ctrl.ID, Title: ctrl.Title, Status: ctrl.Status, Owner: ctrl.Owner}
			if ctrl.DerivedStatus != "" {
				ce.Status = ctrl.DerivedStatus
			}
			if refs, ok := audits.FindingsByControl[ctrl.ID]; ok {
				for _, ref := range refs {
					ce.Findings = append(ce.Findings, ref.Finding.ID)
				}
			}
			pkg.Controls = append(pkg.Controls, ce)
		}
	}

	for _, file := range audits.Files {
		for _, f := range file.Data.Findings {
			fe := FindingExport{
				ID: f.ID, Title: f.Title, Severity: f.Severity,
				Status: f.Status, Controls: f.Controls, ResolvedDate: f.ResolvedDate,
			}
			if f.TrackingIssue != nil {
				fe.TrackingURL = fmt.Sprintf("https://github.com/%s/issues/%d", f.TrackingIssue.Repo, f.TrackingIssue.Number)
			}
			for _, ev := range f.Evidence {
				fe.Evidence = append(fe.Evidence, EvidenceExport{
					Type: ev.Type, Ref: ev.Ref, URL: resolveURL(ev), Description: ev.Description,
				})
			}
			pkg.Findings = append(pkg.Findings, fe)
		}
	}

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return err
	}
	outPath := filepath.Join(outputDir, "evidence-package.json")
	if err := os.WriteFile(outPath, data, 0644); err != nil {
		return err
	}

	fmt.Printf("Evidence package: %s (%d controls, %d findings)\n", outPath, len(pkg.Controls), len(pkg.Findings))
	return nil
}

func resolveURL(ev audit.Evidence) string {
	switch ev.Type {
	case "merged_pr":
		return "https://github.com/" + refToPath(ev.Ref, "pull")
	case "policy", "architecture_doc":
		return ev.Ref
	default:
		return ev.Ref
	}
}

func refToPath(ref, kind string) string {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == '#' {
			return ref[:i] + "/" + kind + "/" + ref[i+1:]
		}
	}
	return ref
}
