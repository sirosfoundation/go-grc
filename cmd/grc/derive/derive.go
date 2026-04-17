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
	maps, err := mapping.Load(cfg.MappingsDir, cfg.Frameworks)
	if err != nil {
		return fmt.Errorf("loading mappings: %w", err)
	}

	cu := deriveControlStatuses(cat, audits)
	for _, u := range cu {
		fmt.Printf("  control   %s: %s -> %s\n", u.id, u.oldVal, u.newVal)
	}

	var fwUpdates []update
	for _, fw := range cfg.Frameworks {
		fu := deriveFrameworkMappings(maps, cat, fw)
		for _, u := range fu {
			fmt.Printf("  %-10s %s: %s -> %s\n", fw.ID, u.id, u.oldVal, u.newVal)
		}
		fwUpdates = append(fwUpdates, fu...)
	}

	total := len(cu) + len(fwUpdates)
	if dryRun {
		fmt.Printf("\nDry run: %d changes would be made.\n", total)
		return nil
	}

	if err := maps.Save(cfg.MappingsDir, cfg.Frameworks); err != nil {
		return fmt.Errorf("saving mappings: %w", err)
	}

	fmt.Printf("\nDone: %d changes applied.\n", total)
	return nil
}

func deriveControlStatuses(cat *catalog.Catalog, audits *audit.AuditSet) []update {
	// Snapshot old statuses before derivation.
	oldStatus := make(map[string]string, len(cat.Controls))
	for id, ctrl := range cat.Controls {
		oldStatus[id] = ctrl.Status
	}

	catalog.DeriveControlStatuses(cat, audits)

	var updates []update
	for id, ctrl := range cat.Controls {
		if ctrl.DerivedStatus != "" && ctrl.DerivedStatus != oldStatus[id] {
			updates = append(updates, update{id, oldStatus[id], ctrl.DerivedStatus})
		}
	}
	return updates
}

// deriveFrameworkMappings derives statuses for one framework's mapping entries.
func deriveFrameworkMappings(maps mapping.Mappings, cat *catalog.Catalog, fw config.FrameworkConfig) []update {
	fm := maps[fw.ID]
	if fm == nil {
		return nil
	}
	vocab := deriveVocab(fw.DeriveMode)
	var updates []update
	for i := range fm.Entries {
		e := &fm.Entries[i]
		if e.Owner == "operator" {
			continue
		}
		result, workStatus := deriveStatus(e.Controls, cat, vocab)
		if result != e.Status {
			updates = append(updates, update{e.Key, e.Status, result})
			e.Status = result
		}
		if fw.WorkStatusField != "" && workStatus != e.WorkStatus {
			e.WorkStatus = workStatus
		}
	}
	return updates
}

// deriveVocab returns the output vocabulary for a derive mode.
type vocab struct {
	requireEvidence bool   // if true, only "validated" counts as full compliance
	all             string // all mapped controls verified/validated
	partial         string // some verified/validated
	none            string // none verified/validated
	empty           string // no controls mapped
}

func deriveVocab(mode string) vocab {
	if mode == "result" {
		return vocab{requireEvidence: true, all: "compliant", partial: "partially_compliant", none: "non_compliant", empty: "not_assessed"}
	}
	return vocab{requireEvidence: false, all: "full", partial: "partial", none: "none", empty: "not_assessed"}
}

// deriveStatus computes framework requirement status from mapped control statuses.
// The vocab determines the output labels and whether "verified" counts as complete.
func deriveStatus(controlIDs []string, cat *catalog.Catalog, v vocab) (string, string) {
	if len(controlIDs) == 0 {
		return v.empty, "to_do"
	}
	allComplete, anyProgress := true, false
	for _, cid := range controlIDs {
		ctrl, ok := cat.Controls[cid]
		if !ok {
			return v.empty, "to_do"
		}
		effective := catalog.EffectiveStatus(ctrl)
		switch effective {
		case "validated":
			anyProgress = true
		case "verified":
			anyProgress = true
			if v.requireEvidence {
				allComplete = false
			}
		default:
			allComplete = false
		}
	}
	switch {
	case allComplete && anyProgress:
		return v.all, "done"
	case anyProgress:
		return v.partial, "in_progress"
	default:
		return v.none, "to_do"
	}
}
