package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "validate",
		Aliases: []string{"lint"},
		Short:   "Validate catalog, mapping, and audit YAML files",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root)
		},
	}
	return cmd
}

var validCategories = map[string]bool{
	"technical": true, "policy": true, "process": true, "physical": true,
}

var validCSFFunctions = map[string]bool{
	"identify": true, "protect": true, "detect": true,
	"respond": true, "recover": true, "govern": true,
}

var validControlStatuses = map[string]bool{
	"verified": true, "to_do": true, "in_progress": true, "validated": true,
}

var validFindingStatuses = map[string]bool{
	"open": true, "in_progress": true, "resolved": true, "accepted": true,
}

var validSeverities = map[string]bool{
	"critical": true, "high": true, "medium": true, "low": true, "info": true,
}

func run(root string) error {
	cfg, err := config.New(root)
	if err != nil {
		return err
	}

	var problems []string
	add := func(msg string) { problems = append(problems, msg) }

	// Validate catalog
	cat, err := catalog.Load(cfg.CatalogDir, cfg.CatalogSubdirs...)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}
	for _, ctrl := range cat.Controls {
		if ctrl.Category != "" && !validCategories[ctrl.Category] {
			add(fmt.Sprintf("control %s: invalid category %q", ctrl.ID, ctrl.Category))
		}
		if ctrl.CSFFunction != "" && !validCSFFunctions[ctrl.CSFFunction] {
			add(fmt.Sprintf("control %s: invalid csf_function %q", ctrl.ID, ctrl.CSFFunction))
		}
		if ctrl.Status != "" && !validControlStatuses[ctrl.Status] {
			add(fmt.Sprintf("control %s: invalid status %q", ctrl.ID, ctrl.Status))
		}
	}

	// Validate mappings
	maps, err := mapping.Load(cfg.MappingsDir, cfg.Frameworks)
	if err != nil {
		return fmt.Errorf("loading mappings: %w", err)
	}
	for _, fw := range cfg.Frameworks {
		fm, ok := maps[fw.ID]
		if !ok {
			add(fmt.Sprintf("framework %s: mapping file %s not found", fw.ID, fw.MappingFile))
			continue
		}
		// Duplicate requirement key detection
		seenKeys := make(map[string]bool)
		for _, e := range fm.Entries {
			if seenKeys[e.Key] {
				add(fmt.Sprintf("framework %s: duplicate requirement key %q", fw.ID, e.Key))
			}
			seenKeys[e.Key] = true
			for _, cid := range e.Controls {
				if _, ok := cat.Controls[cid]; !ok {
					add(fmt.Sprintf("framework %s, entry %s: unknown control %q", fw.ID, e.Key, cid))
				}
			}
		}
		// Check framework catalog file exists
		fwCatPath := filepath.Join(cfg.CatalogDir, cfg.FrameworksSubdir, strings.TrimSuffix(fw.CatalogFile, ".yaml")+".yaml")
		if _, err := os.Stat(fwCatPath); os.IsNotExist(err) {
			// Try without re-adding extension
			fwCatPath2 := filepath.Join(cfg.CatalogDir, cfg.FrameworksSubdir, fw.CatalogFile)
			if _, err := os.Stat(fwCatPath2); os.IsNotExist(err) {
				add(fmt.Sprintf("framework %s: catalog file %s not found in %s/", fw.ID, fw.CatalogFile, cfg.FrameworksSubdir))
			}
		}
	}

	// Validate audits
	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}
	for _, file := range audits.Files {
		for _, f := range file.Data.Findings {
			if !validFindingStatuses[f.Status] {
				add(fmt.Sprintf("finding %s: invalid status %q", f.ID, f.Status))
			}
			if !validSeverities[f.Severity] {
				add(fmt.Sprintf("finding %s: invalid severity %q", f.ID, f.Severity))
			}
			for _, cid := range f.Controls {
				if _, ok := cat.Controls[cid]; !ok {
					add(fmt.Sprintf("finding %s: unknown control %q", f.ID, cid))
				}
			}
			for i, ev := range f.Evidence {
				if ev.Type == "" && ev.Ref == "" && ev.Description == "" {
					add(fmt.Sprintf("finding %s: evidence[%d] has all empty fields", f.ID, i))
				} else if ev.Type == "" {
					add(fmt.Sprintf("finding %s: evidence[%d] missing type", f.ID, i))
				}
			}
		}
	}

	// Detect orphan controls (no findings and no framework mappings) — warnings only
	var warnings []string
	referencedControls := make(map[string]bool)
	for _, file := range audits.Files {
		for _, f := range file.Data.Findings {
			for _, cid := range f.Controls {
				referencedControls[cid] = true
			}
		}
	}
	for _, fm := range maps {
		for _, e := range fm.Entries {
			for _, cid := range e.Controls {
				referencedControls[cid] = true
			}
		}
	}
	for id := range cat.Controls {
		if !referencedControls[id] {
			warnings = append(warnings, fmt.Sprintf("control %s: orphan — not referenced by any finding or framework mapping", id))
		}
	}

	if len(warnings) > 0 {
		fmt.Printf("%d warning(s):\n", len(warnings))
		for _, w := range warnings {
			fmt.Printf("  ⚠ %s\n", w)
		}
	}

	if len(problems) == 0 {
		fmt.Println("Validation passed.")
		return nil
	}
	fmt.Printf("Validation found %d problem(s):\n", len(problems))
	for _, p := range problems {
		fmt.Printf("  - %s\n", p)
	}
	return fmt.Errorf("%d validation error(s)", len(problems))
}
