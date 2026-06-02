package status

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/risk"
)

func NewCommand() *cobra.Command {
	var format string
	var profile string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show compliance status overview (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root, format, profile)
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", `Output format: "text" or "json"`)
	cmd.Flags().StringVar(&profile, "profile", "", "Deployment profile to report on (default: all profiles)")
	return cmd
}

// StatusReport is the structured output of the status command.
type StatusReport struct {
	Profile  string          `json:"profile,omitempty"`
	Findings FindingsSummary `json:"findings"`
	Controls ControlsSummary `json:"controls"`
	Audits   []AuditSummary  `json:"audits"`
	Risks    RisksSummary    `json:"risks,omitempty"`
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

type RisksSummary struct {
	Total    int `json:"total"`
	Accepted int `json:"accepted"`
	Overdue  int `json:"overdue"`
}

func run(root, format, profile string) error {
	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if profile != "" && !cfg.HasProfile(profile) {
		return fmt.Errorf("unknown profile %q; available: %v", profile, cfg.ProfileIDs())
	}

	cat, err := catalog.Load(cfg.CatalogDir, cfg.CatalogSubdirs...)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}

	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}

	risks, err := risk.Load(cfg.RiskDir, cfg.RiskRegister.Files)
	if err != nil {
		return fmt.Errorf("loading risk register: %w", err)
	}

	report := collect(cat, audits, risks, profile)

	if format == "json" {
		return writeJSON(report)
	}
	writeText(report)
	return nil
}

func collect(cat *catalog.Catalog, audits *audit.AuditSet, risks *risk.RiskSet, profile string) StatusReport {
	var fs FindingsSummary
	for _, ref := range audits.FindingsByID {
		fs.Total++
		f := ref.Finding
		if f.TrackingIssue != nil {
			fs.Tracked++
		} else {
			fs.Untracked++
		}
		status := f.StatusForProfile(profile)
		switch status {
		case "open":
			fs.Open++
		case "in_progress":
			fs.InProgress++
		case "resolved":
			fs.Resolved++
			if len(f.EvidenceForProfile(profile)) > 0 {
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
			status := f.StatusForProfile(profile)
			switch status {
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

	var rs RisksSummary
	for _, file := range risks.Files {
		if risk.IsOverdueRegister(file.Data.Register) {
			rs.Overdue += len(file.Data.Risks)
		}
		for _, r := range file.Data.Risks {
			if profile != "" && !r.AppliesToProfile(profile) {
				continue
			}
			rs.Total++
			if r.Status == risk.StatusAccepted {
				rs.Accepted++
			}
		}
	}

	return StatusReport{Profile: profile, Findings: fs, Controls: cs, Audits: as, Risks: rs}
}

func writeJSON(report StatusReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func writeText(report StatusReport) {
	if report.Profile != "" {
		fmt.Printf("Profile: %s\n\n", report.Profile)
	}

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

	if report.Risks.Total > 0 {
		fmt.Println()
		rs := report.Risks
		fmt.Printf("Risk Register: %d total\n", rs.Total)
		fmt.Printf("  Accepted:      %d\n", rs.Accepted)
		if rs.Overdue > 0 {
			fmt.Printf("  Overdue:       %d\n", rs.Overdue)
		}
	}
}
