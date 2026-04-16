package derive

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
)

func NewCommand() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "derive",
		Short: "Derive control and mapping statuses from findings and evidence",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show derived changes without writing")
	return cmd
}

type update struct {
	id, oldVal, newVal string
}

func run(root string, dryRun bool) error {
	cfg := config.New(root)

	cat, err := catalog.Load(cfg.CatalogDir)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}
	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}
	maps, err := mapping.Load(cfg.MappingsDir)
	if err != nil {
		return fmt.Errorf("loading mappings: %w", err)
	}

	cu := deriveControlStatuses(cat, audits)
	eu := deriveEUDIResults(maps, cat)
	iu := deriveISOCoverage(maps, cat)
	gu := deriveGDPRCoverage(maps, cat)

	for _, u := range cu {
		fmt.Printf("  control %s: %s -> %s\n", u.id, u.oldVal, u.newVal)
	}
	for _, u := range eu {
		fmt.Printf("  eudi    %s: %s -> %s\n", u.id, u.oldVal, u.newVal)
	}
	for _, u := range iu {
		fmt.Printf("  iso     %s: %s -> %s\n", u.id, u.oldVal, u.newVal)
	}
	for _, u := range gu {
		fmt.Printf("  gdpr    %s: %s -> %s\n", u.id, u.oldVal, u.newVal)
	}

	total := len(cu) + len(eu) + len(iu) + len(gu)
	if dryRun {
		fmt.Printf("\nDry run: %d changes would be made.\n", total)
		return nil
	}

	if err := maps.Save(cfg.MappingsDir); err != nil {
		return fmt.Errorf("saving mappings: %w", err)
	}

	fmt.Printf("\nDone: %d changes applied.\n", total)
	return nil
}

func deriveControlStatuses(cat *catalog.Catalog, audits *audit.AuditSet) []update {
	var updates []update
	for id, ctrl := range cat.Controls {
		findings := audits.FindingsByControl[id]
		if len(findings) == 0 {
			continue
		}
		allResolved, allEvidence, anyInProgress := true, true, false
		for _, fref := range findings {
			f := fref.Finding
			if f.Status != "resolved" {
				allResolved = false
			}
			if !f.HasEvidence() {
				allEvidence = false
			}
			if f.Status == "in_progress" {
				anyInProgress = true
			}
		}
		var derived string
		switch {
		case allResolved && allEvidence:
			derived = "validated"
		case allResolved:
			derived = "verified"
		case anyInProgress:
			derived = "planned"
		default:
			derived = "to_do"
		}
		if derived != ctrl.Status {
			updates = append(updates, update{id, ctrl.Status, derived})
			ctrl.DerivedStatus = derived
		}
	}
	return updates
}

func deriveEUDIResults(maps *mapping.Mappings, cat *catalog.Catalog) []update {
	if maps.EUDI == nil {
		return nil
	}
	var updates []update
	for i := range maps.EUDI.Requirements {
		req := &maps.EUDI.Requirements[i]
		if req.Owner == "operator" {
			continue
		}
		result, status := deriveFromControls(req.Controls, cat)
		if result != req.Result {
			updates = append(updates, update{req.ID, req.Result, result})
			req.Result = result
		}
		if status != req.Status {
			req.Status = status
		}
	}
	return updates
}

func deriveISOCoverage(maps *mapping.Mappings, cat *catalog.Catalog) []update {
	if maps.ISO == nil {
		return nil
	}
	var updates []update
	for i := range maps.ISO.Mappings {
		m := &maps.ISO.Mappings[i]
		if m.Owner == "operator" {
			continue
		}
		cov := deriveCoverage(m.Controls, cat)
		if cov != m.Coverage {
			updates = append(updates, update{m.AnnexA, m.Coverage, cov})
			m.Coverage = cov
		}
	}
	return updates
}

func deriveGDPRCoverage(maps *mapping.Mappings, cat *catalog.Catalog) []update {
	if maps.GDPR == nil {
		return nil
	}
	var updates []update
	for i := range maps.GDPR.Mappings {
		m := &maps.GDPR.Mappings[i]
		if m.Owner == "operator" {
			continue
		}
		cov := deriveCoverage(m.Controls, cat)
		if cov != m.Coverage {
			updates = append(updates, update{m.MatchName, m.Coverage, cov})
			m.Coverage = cov
		}
	}
	return updates
}

func deriveFromControls(controlIDs []string, cat *catalog.Catalog) (string, string) {
	if len(controlIDs) == 0 {
		return "not_assessed", "to_do"
	}
	allValidated := true
	anyVerified := false
	for _, cid := range controlIDs {
		ctrl, ok := cat.Controls[cid]
		if !ok {
			return "not_assessed", "to_do"
		}
		effective := ctrl.Status
		if ctrl.DerivedStatus != "" {
			effective = ctrl.DerivedStatus
		}
		switch effective {
		case "validated":
			anyVerified = true
		case "verified":
			anyVerified = true
			allValidated = false
		default:
			allValidated = false
		}
	}
	switch {
	case allValidated:
		return "compliant", "done"
	case anyVerified:
		return "partially_compliant", "in_progress"
	default:
		return "non_compliant", "to_do"
	}
}

func deriveCoverage(controlIDs []string, cat *catalog.Catalog) string {
	if len(controlIDs) == 0 {
		return "not_assessed"
	}
	allValidated, anyDone := true, false
	for _, cid := range controlIDs {
		ctrl, ok := cat.Controls[cid]
		if !ok {
			return "not_assessed"
		}
		effective := ctrl.Status
		if ctrl.DerivedStatus != "" {
			effective = ctrl.DerivedStatus
		}
		switch effective {
		case "validated", "verified":
			anyDone = true
		default:
			allValidated = false
		}
	}
	switch {
	case allValidated:
		return "full"
	case anyDone:
		return "partial"
	default:
		return "none"
	}
}
