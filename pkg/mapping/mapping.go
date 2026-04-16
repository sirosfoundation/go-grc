// Package mapping provides generic types and I/O for framework-to-control mappings.
//
// Each framework mapping is a list of entries where each entry maps an external
// requirement ID to internal controls and tracks an assessment status.
// The YAML field names are configurable via FrameworkConfig.
package mapping

import (
"fmt"
"os"
"path/filepath"

"github.com/sirosfoundation/go-grc/pkg/config"

"gopkg.in/yaml.v3"
)

// MappingEntry holds one generic mapping entry.
type MappingEntry struct {
Key        string   // value from key_field
Status     string   // value from status_field (result or coverage)
WorkStatus string   // value from work_status_field (optional, e.g. EUDI "status")
Controls   []string // mapped control IDs
Owner      string
Notes      string // value from notes_field
}

// FrameworkMapping holds all mapping entries for one framework.
type FrameworkMapping struct {
Entries []MappingEntry
}

// Mappings maps framework ID → loaded mapping.
type Mappings map[string]*FrameworkMapping

// Load reads mapping YAML files for all configured frameworks.
func Load(mappingsDir string, frameworks []config.FrameworkConfig) (Mappings, error) {
m := make(Mappings)
for _, fw := range frameworks {
path := filepath.Join(mappingsDir, fw.MappingFile)
data, err := os.ReadFile(path)
if err != nil {
if os.IsNotExist(err) {
continue
}
return nil, fmt.Errorf("reading %s: %w", fw.MappingFile, err)
}
fm, err := parseFramework(data, fw)
if err != nil {
return nil, fmt.Errorf("parsing %s: %w", fw.MappingFile, err)
}
m[fw.ID] = fm
}
return m, nil
}

// Save writes mapping files back to disk.
func (m Mappings) Save(mappingsDir string, frameworks []config.FrameworkConfig) error {
for _, fw := range frameworks {
fm, ok := m[fw.ID]
if !ok {
continue
}
entries := make([]map[string]interface{}, len(fm.Entries))
for i, e := range fm.Entries {
raw := buildRaw(e, fw)
entries[i] = raw
}
out := map[string]interface{}{fw.ListKey: entries}
data, err := yaml.Marshal(out)
if err != nil {
return fmt.Errorf("marshaling %s: %w", fw.MappingFile, err)
}
path := filepath.Join(mappingsDir, fw.MappingFile)
if err := os.WriteFile(path, data, 0644); err != nil {
return err
}
}
return nil
}

func parseFramework(data []byte, fw config.FrameworkConfig) (*FrameworkMapping, error) {
var raw map[string]interface{}
if err := yaml.Unmarshal(data, &raw); err != nil {
return nil, err
}
rawList, ok := raw[fw.ListKey]
if !ok {
return nil, fmt.Errorf("expected %q key in YAML", fw.ListKey)
}
rawSlice, ok := rawList.([]interface{})
if !ok {
return nil, fmt.Errorf("expected %q to be a list", fw.ListKey)
}
list := make([]map[string]interface{}, 0, len(rawSlice))
for _, item := range rawSlice {
if m, ok := item.(map[string]interface{}); ok {
list = append(list, m)
}
}
fm := &FrameworkMapping{
Entries: make([]MappingEntry, len(list)),
}
for i, e := range list {
fm.Entries[i] = extractEntry(e, fw)
}
return fm, nil
}

func extractEntry(raw map[string]interface{}, fw config.FrameworkConfig) MappingEntry {
entry := MappingEntry{
Key:    getStr(raw, fw.KeyField),
Status: getStr(raw, fw.StatusField),
Owner:  getStr(raw, "owner"),
Notes:  getStr(raw, fw.NotesField),
}
if fw.WorkStatusField != "" {
entry.WorkStatus = getStr(raw, fw.WorkStatusField)
}
if v, ok := raw["controls"]; ok {
if arr, ok := v.([]interface{}); ok {
for _, item := range arr {
if s, ok := item.(string); ok {
entry.Controls = append(entry.Controls, s)
}
}
}
}
return entry
}

func buildRaw(e MappingEntry, fw config.FrameworkConfig) map[string]interface{} {
raw := map[string]interface{}{
fw.KeyField:    e.Key,
fw.StatusField: e.Status,
"controls":     e.Controls,
"owner":        e.Owner,
}
if fw.WorkStatusField != "" && e.WorkStatus != "" {
raw[fw.WorkStatusField] = e.WorkStatus
}
if e.Notes != "" {
raw[fw.NotesField] = e.Notes
}
return raw
}

func getStr(m map[string]interface{}, key string) string {
if v, ok := m[key]; ok {
if s, ok := v.(string); ok {
return s
}
}
return ""
}
