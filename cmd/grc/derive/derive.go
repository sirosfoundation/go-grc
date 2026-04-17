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
var updates []update
for i := range fm.Entries {
e := &fm.Entries[i]
if e.Owner == "operator" {
continue
}
switch fw.DeriveMode {
case "result":
result, workStatus := deriveFromControls(e.Controls, cat)
if result != e.Status {
updates = append(updates, update{e.Key, e.Status, result})
e.Status = result
}
if workStatus != e.WorkStatus {
e.WorkStatus = workStatus
}
default: // "coverage"
cov := deriveCoverage(e.Controls, cat)
if cov != e.Status {
updates = append(updates, update{e.Key, e.Status, cov})
e.Status = cov
}
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
effective := catalog.EffectiveStatus(ctrl)
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
allDone, anyPartial := true, false
for _, cid := range controlIDs {
ctrl, ok := cat.Controls[cid]
if !ok {
return "not_assessed"
}
effective := catalog.EffectiveStatus(ctrl)
switch effective {
case "validated", "verified":
anyPartial = true
default:
allDone = false
}
}
switch {
case allDone:
return "full"
case anyPartial:
return "partial"
default:
return "none"
}
}
