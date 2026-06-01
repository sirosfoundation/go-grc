package status

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
)

func NewCommand() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show compliance status overview (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root, format)
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", `Output format: "text" or "json"`)
	return cmd
}

// StatusReport is the structured output of the status command.
type StatusReport struct {
	Findings FindingsSummary `json:"findings"`
	Controls ControlsSummary `json:"controls"`
	Audits   []AuditSummary  `json:"audits"`
}

type FindingsSummary struct {
	Total        int `json:"total"`
	Open         int `json:"open"`
	InProgress   int `json:"in_progress"`
	Resolved     int `json:"resolved"`
	WithEvidence int `json:"with_evidence"`
	Accepted     int `json:"accepted"`
	Tracked      int `json:"tracked"`
	Untracked    int `json:"untracked"`
}

type ControlsSummary struct {
	Total      int `json:"total"`
	Verified   int `json:"verified"`
	InProgress int `json:"in_progress"`
	ToDo       int `json:"to_do"`
}

type AuditSummary struct {
	ID         string `json:"id"`
	Total      int    `json:"total"`
	Open       int    `json:"open"`
	InProgress int    `json:"in_progress"`
	Done       int    `json:"done"`
}

func run(root, format string) error {
	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cat, err := catalog.Load(cfg.CatalogDir, cfg.CatalogSubdirs...)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}

	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}

	report := collect(cat, audits)

	if format == "json" {
		return writeJSON(report)
	}
	writeText(report)
	return nil
}

func collect(cat *catalog.Catalog, audits *audit.AuditSet) StatusReport {
	var fs FindingsSummary
	for _, ref := range audits.FindingsByID {
		fs.Total++
		f := ref.Finding
		if f.TrackingIssue != nil {
			fs.Tracked++
		} else {
			fs.Untracked++
		}
		switch f.Status {
		case "open":
			fs.Open++
		case "in_progress":
			fs.InProgress++
		case "resolved":
			fs.Resolved++
			if f.HasEvidence() {
				fs.WithEvidence++
			}
		case "accepted":
			fs.Accepted++
		}
	}

	var cs ControlsSummary
	cs.Total = len(cat.Controls)
	for _, ctrl := range cat.Controls {
		switch ctrl.Status {
		case "verified", "validated":
			cs.Verified++
		case "in_progress":
			cs.InProgress++
		default:
			cs.ToDo++
		}
	}

	var as []AuditSummary
	for _, file := range audits.Files {
		a := file.Data.Audit
		s := AuditSummary{ID: a.ID, Total: len(file.Data.Findings)}
		for _, f := range file.Data.Findings {
			switch f.Status {
			case "open":
				s.Open++
			case "in_progress":
				s.InProgress++
			case "resolved", "accepted":
				s.Done++
			}
		}
		as = append(as, s)
	}

	return StatusReport{Findings: fs, Controls: cs, Audits: as}
}

func writeJSON(report StatusReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func writeText(report StatusReport) {
	fs := report.Findings
	fmt.Printf("Findings: %d total\n", fs.Total)
	fmt.Printf("  Open:          %d\n", fs.Open)
	fmt.Printf("  In Progress:   %d\n", fs.InProgress)
	fmt.Printf("  Resolved:      %d (%d with evidence)\n", fs.Resolved, fs.WithEvidence)
	fmt.Printf("  Accepted:      %d\n", fs.Accepted)
	fmt.Printf("  Tracked:       %d\n", fs.Tracked)
	fmt.Printf("  Untracked:     %d\n", fs.Untracked)
	fmt.Println()

	cs := report.Controls
	fmt.Printf("Controls: %d total\n", cs.Total)
	fmt.Printf("  Verified:      %d\n", cs.Verified)
	fmt.Printf("  In Progress:   %d\n", cs.InProgress)
	fmt.Printf("  To Do:         %d\n", cs.ToDo)
	fmt.Println()

	fmt.Printf("%-20s %5s %5s %5s %5s\n", "Audit", "Total", "Open", "InPrg", "Done")
	fmt.Println("-------------------------------------------------------")
	for _, a := range report.Audits {
		fmt.Printf("%-20s %5d %5d %5d %5d\n", a.ID, a.Total, a.Open, a.InProgress, a.Done)
	}
}
