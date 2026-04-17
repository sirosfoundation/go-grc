package status

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show compliance status overview (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root)
		},
	}
	return cmd
}

func run(root string) error {
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

	total := 0
	tracked, untracked := 0, 0
	open, inProgress, resolved := 0, 0, 0
	withEvidence := 0

	for _, ref := range audits.FindingsByID {
		total++
		f := ref.Finding
		if f.TrackingIssue != nil {
			tracked++
		} else {
			untracked++
		}
		switch f.Status {
		case "open":
			open++
		case "in_progress":
			inProgress++
		case "resolved":
			resolved++
			if f.HasEvidence() {
				withEvidence++
			}
		}
	}

	fmt.Printf("Findings: %d total\n", total)
	fmt.Printf("  Open:          %d\n", open)
	fmt.Printf("  In Progress:   %d\n", inProgress)
	fmt.Printf("  Resolved:      %d (%d with evidence)\n", resolved, withEvidence)
	fmt.Printf("  Tracked:       %d\n", tracked)
	fmt.Printf("  Untracked:     %d\n", untracked)
	fmt.Println()

	totalCtrls := len(cat.Controls)
	verified, toDo, planned := 0, 0, 0
	for _, ctrl := range cat.Controls {
		switch ctrl.Status {
		case "verified", "validated":
			verified++
		case "planned":
			planned++
		default:
			toDo++
		}
	}

	fmt.Printf("Controls: %d total\n", totalCtrls)
	fmt.Printf("  Verified:      %d\n", verified)
	fmt.Printf("  Planned:       %d\n", planned)
	fmt.Printf("  To Do:         %d\n", toDo)
	fmt.Println()

	fmt.Printf("%-20s %5s %5s %5s %5s\n", "Audit", "Total", "Open", "InPrg", "Done")
	fmt.Println("-------------------------------------------------------")
	for _, file := range audits.Files {
		a := file.Data.Audit
		o, ip, r := 0, 0, 0
		for _, f := range file.Data.Findings {
			switch f.Status {
			case "open":
				o++
			case "in_progress":
				ip++
			case "resolved":
				r++
			}
		}
		fmt.Printf("%-20s %5d %5d %5d %5d\n", a.ID, len(file.Data.Findings), o, ip, r)
	}

	return nil
}
