package risk

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/risk"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "risk",
		Short: "Manage the risk register",
	}
	cmd.AddCommand(newListCommand())
	cmd.AddCommand(newValidateCommand())
	cmd.AddCommand(newSummaryCommand())
	return cmd
}

func newListCommand() *cobra.Command {
	var (
		owner   string
		profile string
		overdue bool
		format  string
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List risks from the risk register",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return runList(root, owner, profile, overdue, format)
		},
	}
	cmd.Flags().StringVar(&owner, "owner", "", "Filter by owner (platform|operator)")
	cmd.Flags().StringVar(&profile, "profile", "", "Filter by deployment profile")
	cmd.Flags().BoolVar(&overdue, "overdue", false, "Show only risks past review date")
	cmd.Flags().StringVar(&format, "format", "text", `Output format: "text" or "json"`)
	return cmd
}

func newValidateCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate risk register entries against findings",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return runValidate(root)
		},
	}
	return cmd
}

func newSummaryCommand() *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "summary",
		Short: "Show risk register summary",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return runSummary(root, format)
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", `Output format: "text" or "json"`)
	return cmd
}

func loadRisks(cfg *config.Config) (*risk.RiskSet, error) {
	return risk.Load(cfg.RiskDir, cfg.RiskRegister.Files)
}

// RiskEntry is the JSON output format for a single risk.
type RiskEntry struct {
	ID               string   `json:"id"`
	Finding          string   `json:"finding"`
	Profiles         []string `json:"profiles,omitempty"`
	Owner            string   `json:"owner"`
	Title            string   `json:"title"`
	Severity         string   `json:"severity"`
	ResidualSeverity string   `json:"residual_severity"`
	Status           string   `json:"status"`
}

func runList(root, owner, profile string, overdue bool, format string) error {
	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	risks, err := loadRisks(cfg)
	if err != nil {
		return fmt.Errorf("loading risk register: %w", err)
	}

	var entries []RiskEntry
	for _, file := range risks.Files {
		regOwner := file.Data.Register.Owner
		if owner != "" && regOwner != owner {
			continue
		}
		isOverdue := risk.IsOverdueRegister(file.Data.Register)
		if overdue && !isOverdue {
			continue
		}
		for _, r := range file.Data.Risks {
			if profile != "" && !r.AppliesToProfile(profile) {
				continue
			}
			entries = append(entries, RiskEntry{
				ID:               r.ID,
				Finding:          r.Finding,
				Profiles:         r.Profiles,
				Owner:            regOwner,
				Title:            r.Title,
				Severity:         r.Severity,
				ResidualSeverity: r.ResidualSeverity,
				Status:           r.Status,
			})
		}
	}

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(entries)
	}

	if len(entries) == 0 {
		fmt.Println("No risks found.")
		return nil
	}

	fmt.Printf("%-12s %-12s %-10s %-10s %-10s %-10s %s\n",
		"ID", "Finding", "Owner", "Severity", "Residual", "Status", "Title")
	fmt.Println("--------------------------------------------------------------------------------------------")
	for _, e := range entries {
		fmt.Printf("%-12s %-12s %-10s %-10s %-10s %-10s %s\n",
			e.ID, e.Finding, e.Owner, e.Severity, e.ResidualSeverity, e.Status, truncate(e.Title, 40))
	}
	return nil
}

func runValidate(root string) error {
	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	risks, err := loadRisks(cfg)
	if err != nil {
		return fmt.Errorf("loading risk register: %w", err)
	}

	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}

	var problems []string
	var warnings []string

	// Check every risk entry references a valid finding
	for _, file := range risks.Files {
		for _, r := range file.Data.Risks {
			if !risk.ValidStatuses[r.Status] {
				problems = append(problems, fmt.Sprintf("risk %s: invalid status %q", r.ID, r.Status))
			}
			if _, ok := audits.FindingsByID[r.Finding]; !ok {
				problems = append(problems, fmt.Sprintf("risk %s: references unknown finding %q", r.ID, r.Finding))
			}
			if r.Decision.Date == "" {
				problems = append(problems, fmt.Sprintf("risk %s: missing decision date", r.ID))
			}
			if r.Decision.Reviewer == "" {
				problems = append(problems, fmt.Sprintf("risk %s: missing decision reviewer", r.ID))
			}
			if len(r.CompensatingControls) == 0 {
				warnings = append(warnings, fmt.Sprintf("risk %s: no compensating controls listed", r.ID))
			}
			// Validate profile references
			for _, p := range r.Profiles {
				if !cfg.HasProfile(p) {
					problems = append(problems, fmt.Sprintf("risk %s: references unknown profile %q", r.ID, p))
				}
			}
		}
		// Check register-level review date
		if risk.IsOverdueRegister(file.Data.Register) {
			warnings = append(warnings, fmt.Sprintf("register %s: review overdue (next_review: %s)",
				file.Data.Register.ID, file.Data.Register.NextReview))
		}
	}

	// Check every accepted finding has a risk entry
	for _, ref := range audits.FindingsByID {
		if ref.Finding.Status == audit.StatusAccepted {
			if _, ok := risks.RisksByFinding[ref.Finding.ID]; !ok {
				problems = append(problems, fmt.Sprintf("finding %s: status is 'accepted' but no risk register entry exists", ref.Finding.ID))
			}
		}
	}

	if len(warnings) > 0 {
		fmt.Printf("%d warning(s):\n", len(warnings))
		for _, w := range warnings {
			fmt.Printf("  ⚠ %s\n", w)
		}
	}

	if len(problems) == 0 {
		fmt.Println("Risk register validation passed.")
		return nil
	}
	fmt.Printf("Risk register validation found %d problem(s):\n", len(problems))
	for _, p := range problems {
		fmt.Printf("  - %s\n", p)
	}
	return fmt.Errorf("%d validation error(s)", len(problems))
}

type RiskSummary struct {
	Total    int            `json:"total"`
	ByOwner  map[string]int `json:"by_owner"`
	ByStatus map[string]int `json:"by_status"`
	Overdue  int            `json:"overdue"`
}

func runSummary(root, format string) error {
	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	risks, err := loadRisks(cfg)
	if err != nil {
		return fmt.Errorf("loading risk register: %w", err)
	}

	summary := RiskSummary{
		ByOwner:  make(map[string]int),
		ByStatus: make(map[string]int),
	}

	for _, file := range risks.Files {
		owner := file.Data.Register.Owner
		if risk.IsOverdueRegister(file.Data.Register) {
			summary.Overdue += len(file.Data.Risks)
		}
		for _, r := range file.Data.Risks {
			summary.Total++
			summary.ByOwner[owner]++
			summary.ByStatus[r.Status]++
		}
	}

	if format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}

	fmt.Printf("Risk Register: %d total\n", summary.Total)
	for k, v := range summary.ByOwner {
		fmt.Printf("  %-12s %d\n", k+":", v)
	}
	for k, v := range summary.ByStatus {
		fmt.Printf("  %-12s %d\n", k+":", v)
	}
	if summary.Overdue > 0 {
		fmt.Printf("  Overdue:     %d\n", summary.Overdue)
	}
	return nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
