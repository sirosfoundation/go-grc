package render

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/mapping"
)

func writePage(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func categoryJSON(label string, position int) string {
	data, _ := json.Marshal(map[string]interface{}{
		"label":    label,
		"position": position,
	})
	return string(data) + "\n"
}

func idSlug(id string) string {
	return strings.ToLower(strings.ReplaceAll(id, "-", "_"))
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)

func entrySlug(key string) string {
	s := strings.ToLower(key)
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, ".", "_")
	s = nonAlnum.ReplaceAllString(s, "_")
	return strings.Trim(s, "_")
}

func groupKind(group catalog.Group) string {
	if group.SourceDir != "" {
		return group.SourceDir
	}
	return "technical"
}

func kindLabel(kind string) string {
	if kind == "organizational" {
		return "Organizational"
	}
	return "Technical"
}

func kindPosition(kind string) int {
	if kind == "organizational" {
		return 3
	}
	return 2
}

// --- Badge helpers (HTML matching the Docusaurus CSS classes) ---

func statusBadge(s string) string {
	m := map[string]string{
		"verified":    `<span class="badge--verified">verified</span>`,
		"to_do":       `<span class="badge--to-do">to_do</span>`,
		"in_progress": `<span class="badge--to-do">in_progress</span>`,
		"validated":   `<span class="badge--verified">validated</span>`,
	}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func ownerBadge(s string) string {
	badges := map[string]string{
		"platform": `<span class="badge--platform">platform</span>`,
		"operator": `<span class="badge--operator">operator</span>`,
		"shared":   `<span class="badge--platform">platform</span> <span class="badge--operator">operator</span>`,
	}
	parts := strings.Split(s, ", ")
	var out []string
	for _, p := range parts {
		if v, ok := badges[p]; ok {
			out = append(out, v)
		} else {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

func csfBadge(s string) string {
	anchors := map[string]string{
		"govern":   "govern-gv",
		"identify": "identify-id",
		"protect":  "protect-pr",
		"detect":   "detect-de",
		"respond":  "respond-rs",
		"recover":  "recover-rc",
	}
	if a, ok := anchors[s]; ok {
		return fmt.Sprintf(`[<span class="badge--csf">%s</span>](/csf#%s)`, s, a)
	}
	return s
}

func coverageSpan(s string) string {
	m := map[string]string{
		"full":         `<span class="coverage--full">full</span>`,
		"partial":      `<span class="coverage--partial">partial</span>`,
		"none":         `<span class="coverage--none">none</span>`,
		"not_assessed": `<span class="coverage--not-assessed">\u2014</span>`,
	}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func findingStatusBadge(s string) string {
	m := map[string]string{
		"open":        `<span class="badge--to-do">open</span>`,
		"in_progress": `<span class="badge--to-do">in progress</span>`,
		"resolved":    `<span class="badge--verified">resolved</span>`,
		"accepted":    `<span class="badge--verified">accepted</span>`,
	}
	if v, ok := m[s]; ok {
		return v
	}
	return s
}

func sevIcon(s string) string {
	m := map[string]string{"critical": "\U0001f534", "high": "\U0001f7e0", "medium": "\U0001f7e1", "low": "\U0001f7e2"}
	if v, ok := m[s]; ok {
		return v
	}
	return ""
}

func sevRank(s string) int {
	switch s {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	}
	return 0
}

// --- Link helpers ---

func controlLinks(ids []string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = controlLink(id)
	}
	return strings.Join(parts, ", ")
}

func controlLink(id string) string {
	if url, ok := controlURL[id]; ok {
		return "[" + id + "](" + url + ")"
	}
	return "`" + id + "`"
}

func fwReqLink(key, fwID string) string {
	if urls, ok := frameworkURLs[fwID]; ok {
		if url, ok2 := urls[key]; ok2 {
			return "[" + key + "](" + url + ")"
		}
	}
	return "`" + key + "`"
}

func findingLink(f *audit.Finding) string {
	if f.TrackingIssue != nil {
		return fmt.Sprintf("[%s](https://github.com/%s/issues/%d)", f.ID, f.TrackingIssue.Repo, f.TrackingIssue.Number)
	}
	return "`" + f.ID + "`"
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

// --- Framework reverse index (generic) ---

func buildFrameworkRefs(maps mapping.Mappings) map[string]map[string][]string {
	refs := map[string]map[string][]string{}
	for fwID, fm := range maps {
		fwRefs := map[string][]string{}
		for _, e := range fm.Entries {
			for _, cid := range e.Controls {
				fwRefs[cid] = append(fwRefs[cid], e.Key)
			}
		}
		refs[fwID] = fwRefs
	}
	return refs
}

// --- ENISA reference resolution (applied to EUDI descriptions) ---

var enisaRefPatterns []struct {
	re          *regexp.Regexp
	replacement string
}

var arfRe *regexp.Regexp

func init() {
	enisaRefPatterns = []struct {
		re          *regexp.Regexp
		replacement string
	}{
		{regexp.MustCompile(`OWASP\s+Application\s+Security\s+Verification\s+Standard\s*\[i\.10\]`),
			"[OWASP ASVS](https://owasp.org/www-project-application-security-verification-standard/)"},
		{regexp.MustCompile(`ECCG\s+Agreed\s+Cryptograph(?:y|ic)\s+Mechanisms\s*\[2\]`),
			"[ECCG Agreed Cryptographic Mechanisms](https://www.enisa.europa.eu/topics/certification/eccg)"},
		{regexp.MustCompile(`CIR\s*\(EU\)\s*2024/2981\s*\[i\.3\]`),
			"[CIR (EU) 2024/2981](https://eur-lex.europa.eu/eli/reg_impl/2024/2981/oj)"},
		{regexp.MustCompile(`CIR\s*\(EU\)\s*2015/1502\s*\[i\.2\]`),
			"[CIR (EU) 2015/1502](https://eur-lex.europa.eu/eli/reg_impl/2015/1502/oj)"},
		{regexp.MustCompile(`EN\s+319\s+401\s*\[1\]`),
			"[EN 319 401](https://www.etsi.org/deliver/etsi_en/319400_319499/319401/)"},
	}
	arfRe = regexp.MustCompile(`\[ARF ([^\]]+)\]`)
}

func resolveENISARefs(text string) string {
	for _, r := range enisaRefPatterns {
		text = r.re.ReplaceAllString(text, r.replacement)
	}
	text = arfRe.ReplaceAllString(text, "[ARF $1](https://eudi.dev/2.8.0/architecture-and-reference-framework-main/)")
	return text
}
