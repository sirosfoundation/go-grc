package derive

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
)

func NewCommand() *cobra.Command {
	var (
		dryRun    bool
		format    string
		changelog string
		profile   string
	)
	cmd := &cobra.Command{
		Use:   "derive",
		Short: "Derive control and mapping statuses from findings and evidence",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root, dryRun, format, changelog, profile)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show derived changes without writing")
	cmd.Flags().StringVar(&format, "format", "text", `Output format: "text" or "json"`)
	cmd.Flags().StringVar(&changelog, "changelog", "", "Append a changelog entry to this file")
	cmd.Flags().StringVar(&profile, "profile", "", "Derive statuses for a specific deployment profile")
	return cmd
}

// DeriveReport is the structured output for --format json.
type DeriveReport struct {
	Timestamp  string            `json:"timestamp"`
	DryRun     bool              `json:"dry_run"`
	Controls   []Change          `json:"controls"`
	Frameworks []FrameworkChange `json:"frameworks"`
	Total      int               `json:"total"`
}

// Change records a single status transition.
type Change struct {
	ID       string `json:"id"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
}

// FrameworkChange groups changes under a framework ID.
type FrameworkChange struct {
	Framework string   `json:"framework"`
	Changes   []Change `json:"changes"`
}

type update struct {
	id, oldVal, newVal string
}

type fwUpdate struct {
	fwID    string
	updates []update
}

func run(root string, dryRun bool, format, changelogPath, profile string) error {
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
	maps, err := mapping.Load(cfg.MappingsDir, cfg.Frameworks)
	if err != nil {
		return fmt.Errorf("loading mappings: %w", err)
	}

	cu := deriveControlStatuses(cat, audits, profile)

	var fwUpdates []fwUpdate
	for _, fw := range cfg.Frameworks {
		fu := deriveFrameworkMappings(maps, cat, fw)
		if len(fu) > 0 {
			fwUpdates = append(fwUpdates, fwUpdate{fwID: fw.ID, updates: fu})
		}
	}

	total := len(cu)
	for _, fu := range fwUpdates {
		total += len(fu.updates)
	}

	now := time.Now().UTC()

	if format == "json" {
		report := DeriveReport{
			Timestamp: now.Format(time.RFC3339),
			DryRun:    dryRun,
			Total:     total,
		}
		for _, u := range cu {
			report.Controls = append(report.Controls, Change{ID: u.id, OldValue: u.oldVal, NewValue: u.newVal})
		}
		for _, fu := range fwUpdates {
			fc := FrameworkChange{Framework: fu.fwID}
			for _, u := range fu.updates {
				fc.Changes = append(fc.Changes, Change{ID: u.id, OldValue: u.oldVal, NewValue: u.newVal})
			}
			report.Frameworks = append(report.Frameworks, fc)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	// Text output
	for _, u := range cu {
		fmt.Printf("  control   %s: %s -> %s\n", u.id, u.oldVal, u.newVal)
	}
	for _, fu := range fwUpdates {
		for _, u := range fu.updates {
			fmt.Printf("  %-10s %s: %s -> %s\n", fu.fwID, u.id, u.oldVal, u.newVal)
		}
	}

	if dryRun {
		fmt.Printf("\nDry run: %d changes would be made.\n", total)
		return nil
	}

	if err := maps.Save(cfg.MappingsDir, cfg.Frameworks); err != nil {
		return fmt.Errorf("saving mappings: %w", err)
	}

	if changelogPath != "" {
		if err := appendChangelog(changelogPath, now, cu, fwUpdates); err != nil {
			return fmt.Errorf("writing changelog: %w", err)
		}
	}

	fmt.Printf("\nDone: %d changes applied.\n", total)
	return nil
}

func appendChangelog(path string, when time.Time, cu []update, fwUpdates []fwUpdate) error {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", when.Format("2006-01-02 15:04 UTC"))

	if len(cu) > 0 {
		b.WriteString("### Controls\n\n| Control | Old | New |\n|---------|-----|-----|\n")
		for _, u := range cu {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", u.id, u.oldVal, u.newVal)
		}
		b.WriteString("\n")
	}

	for _, fu := range fwUpdates {
		fmt.Fprintf(&b, "### %s\n\n| Requirement | Old | New |\n|-------------|-----|-----|\n", fu.fwID)
		for _, u := range fu.updates {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", u.id, u.oldVal, u.newVal)
		}
		b.WriteString("\n")
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(b.String())
	return err
}

func deriveControlStatuses(cat *catalog.Catalog, audits *audit.AuditSet, profile string) []update {
	// Snapshot old statuses before derivation.
	oldStatus := make(map[string]string, len(cat.Controls))
	for id, ctrl := range cat.Controls {
		oldStatus[id] = ctrl.Status
	}

	if profile != "" {
		catalog.DeriveControlStatusesForProfile(cat, audits, profile)
	} else {
		catalog.DeriveControlStatuses(cat, audits)
	}

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
