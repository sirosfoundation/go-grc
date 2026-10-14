package render

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/config"
	"github.com/sirosfoundation/go-grc/pkg/result"
	"github.com/sirosfoundation/go-grc/pkg/soa"
)

// staticAuditsDir is where supporting documents of audit results are copied,
// under the site's static directory. It is rebuilt on every render.
const staticAuditsDir = "grc-audits"

// auditView is one audit cycle as shown on a profile: its result (the
// certificate) and its statement of applicability, each present only when
// the profile may show it.
type auditView struct {
	Name   string
	Slug   string
	Title  string
	SoA    *soa.Loaded
	Result *result.Loaded
}

// assessmentView is one internal assessment, shown on the private profile only.
type assessmentView struct {
	Name     string
	Slug     string
	Title    string
	Catalogs []string
	Findings int
	SoA      *soa.Loaded
}

// collectAudits decides what each profile may show.
//
// Public: a real audit appears only through a result that has been issued or
// a statement that has been approved, and only those parts. Findings,
// pre-audit links and internal assessments never appear.
// Private: every audit with everything it has, and every assessment.
func collectAudits(set *audit.AuditSet, soas []*soa.Loaded, results []*result.Loaded, isPublic bool) ([]*auditView, []*assessmentView) {
	views := map[string]*auditView{}
	get := func(name string) *auditView {
		v := views[name]
		if v == nil {
			v = &auditView{Name: name, Slug: cycleSlug(name), Title: name}
			views[name] = v
		}
		return v
	}
	for i := range set.Files {
		f := &set.Files[i]
		if f.Kind != audit.KindAudit {
			continue
		}
		v := get(f.Name)
		if t := f.Data.Meta().Title; t != "" {
			v.Title = t
		}
	}
	for _, s := range soas {
		if s.Kind != soa.KindAudit {
			continue
		}
		if isPublic && !s.SoA.Published() {
			continue
		}
		v := get(s.Cycle)
		v.SoA = s
		if v.Title == v.Name && s.SoA.Header.Title != "" {
			v.Title = s.SoA.Header.Title
		}
	}
	for _, r := range results {
		if isPublic && !r.Result.Published() {
			continue
		}
		get(r.Cycle).Result = r
	}

	var audits []*auditView
	for _, v := range views {
		if isPublic && v.SoA == nil && v.Result == nil {
			continue // nothing about this audit is public
		}
		audits = append(audits, v)
	}
	sort.Slice(audits, func(i, j int) bool { return audits[i].Name < audits[j].Name })

	var assessments []*assessmentView
	if !isPublic {
		byName := map[string]*assessmentView{}
		for i := range set.Files {
			f := &set.Files[i]
			if f.Kind != audit.KindAssessment {
				continue
			}
			m := f.Data.Meta()
			v := &assessmentView{Name: f.Name, Slug: cycleSlug(f.Name), Title: m.Title, Catalogs: m.Catalogs, Findings: len(f.Data.Findings)}
			if v.Title == "" {
				v.Title = f.Name
			}
			byName[f.Name] = v
		}
		for _, s := range soas {
			if s.Kind != soa.KindAssessment {
				continue
			}
			v := byName[s.Cycle]
			if v == nil {
				v = &assessmentView{Name: s.Cycle, Slug: cycleSlug(s.Cycle), Title: s.Cycle}
				byName[s.Cycle] = v
			}
			v.SoA = s
		}
		for _, v := range byName {
			assessments = append(assessments, v)
		}
		sort.Slice(assessments, func(i, j int) bool { return assessments[i].Name < assessments[j].Name })
	}
	return audits, assessments
}

// generateAudits writes the audits section: an index, one directory per
// audit with its result and statement, and, on the private profile, the
// internal assessments. Supporting documents are copied under the site's
// static directory.
func generateAudits(cfg *config.Config, audits []*auditView, assessments []*assessmentView, isPublic bool) error {
	dir := filepath.Join(cfg.SiteDir, "audits")
	if err := writePage(filepath.Join(dir, "_category_.json"), categoryJSON("Audits", 4)); err != nil {
		return err
	}
	if err := writePage(filepath.Join(dir, "index.md"), renderAuditsIndex(audits, assessments, isPublic)); err != nil {
		return err
	}
	seen := map[string]string{}
	for _, v := range audits {
		if v.Slug == "" || v.Slug == "assessments" {
			return fmt.Errorf("audit %q has no usable page name", v.Name)
		}
		if other, ok := seen[v.Slug]; ok {
			return fmt.Errorf("audits %q and %q map to the same page %q", other, v.Name, v.Slug)
		}
		seen[v.Slug] = v.Name

		docs, err := publishDocuments(cfg, v)
		if err != nil {
			return err
		}
		if err := writePage(filepath.Join(dir, v.Slug, "index.md"), renderAuditPage(v, docs, isPublic)); err != nil {
			return err
		}
		if v.SoA != nil {
			if err := writePage(filepath.Join(dir, v.Slug, "soa.md"), renderSoAPage(v.SoA, isPublic)); err != nil {
				return err
			}
		}
	}
	if len(assessments) == 0 {
		return nil
	}
	adir := filepath.Join(dir, "assessments")
	if err := writePage(filepath.Join(adir, "_category_.json"), categoryJSON("Internal assessments", 9)); err != nil {
		return err
	}
	if err := writePage(filepath.Join(adir, "index.md"), renderAssessmentsIndex(assessments)); err != nil {
		return err
	}
	aseen := map[string]string{}
	for _, v := range assessments {
		if v.Slug == "" {
			return fmt.Errorf("assessment %q has no usable page name", v.Name)
		}
		if other, ok := aseen[v.Slug]; ok {
			return fmt.Errorf("assessments %q and %q map to the same page %q", other, v.Name, v.Slug)
		}
		aseen[v.Slug] = v.Name
		if err := writePage(filepath.Join(adir, v.Slug, "index.md"), renderAssessmentPage(v)); err != nil {
			return err
		}
		if v.SoA != nil {
			if err := writePage(filepath.Join(adir, v.Slug, "soa.md"), renderSoAPage(v.SoA, false)); err != nil {
				return err
			}
		}
	}
	return nil
}

func renderAuditsIndex(audits []*auditView, assessments []*assessmentView, isPublic bool) string {
	var b strings.Builder
	b.WriteString("---\ntitle: \"Audits\"\nsidebar_label: \"Overview\"\nsidebar_position: 0\n---\n\n# Audits\n\n")
	if isPublic {
		b.WriteString("Results of external audits, with their statements of applicability.\n\n")
	} else {
		b.WriteString("External audits and their results. Internal assessments are listed separately.\n\n")
	}
	b.WriteString("| Audit | Result | Statement of applicability |\n|---|---|---|\n")
	for _, v := range audits {
		res, st := "–", "–"
		if v.Result != nil {
			res = resultLabel(v.Result)
		}
		if v.SoA != nil {
			st = stateLabel(v.SoA.SoA.State())
		}
		fmt.Fprintf(&b, "| [%s](/audits/%s) | %s | %s |\n", cell(v.Title), v.Slug, res, st)
	}
	if !isPublic && len(assessments) > 0 {
		b.WriteString("\nSee [internal assessments](/audits/assessments).\n")
	}
	return b.String()
}

func resultLabel(r *result.Loaded) string {
	h := r.Result.Header
	switch r.Result.State() {
	case result.StatusIssued:
		if h.ValidUntil != "" {
			return "Issued (valid until " + cell(h.ValidUntil) + ")"
		}
		return "Issued"
	case result.StatusPreAudit:
		return "Pre-audit"
	case result.StatusInAudit:
		return "In audit"
	case result.StatusNotIssued:
		return "Not issued"
	case result.StatusWithdrawn:
		return "Withdrawn"
	}
	return "Planned"
}

// docLink is a supporting document with the URL it is served at.
type docLink struct {
	Title string
	URL   string
}

// publishDocuments copies the files of a visible result into the site's
// static directory and returns links for every document.
func publishDocuments(cfg *config.Config, v *auditView) ([]docLink, error) {
	if v.Result == nil {
		return nil, nil
	}
	var out []docLink
	for _, d := range v.Result.Result.Header.Documents {
		if d.URL != "" {
			out = append(out, docLink{Title: d.Title, URL: d.URL})
			continue
		}
		src, err := result.SafeFile(v.Result.Dir, d.File)
		if err != nil {
			return nil, fmt.Errorf("audit %s: %w", v.Name, err)
		}
		rel := path.Join(v.Slug, filepath.ToSlash(filepath.Clean(d.File)))
		dst := filepath.Join(filepath.Dir(cfg.SiteDir), "static", staticAuditsDir, filepath.FromSlash(rel))
		if err := copyFile(src, dst); err != nil {
			return nil, err
		}
		out = append(out, docLink{Title: d.Title, URL: "/" + staticAuditsDir + "/" + rel})
	}
	return out, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func renderAuditPage(v *auditView, docs []docLink, isPublic bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: %q\nsidebar_label: %q\n---\n\n# %s\n\n", v.Title, v.Title, v.Title)
	if r := v.Result; r != nil {
		h := r.Result.Header
		if !r.Result.Published() {
			fmt.Fprintf(&b, ":::caution %s\nThis audit has not produced an issued result. It is internal and not published on the public site.\n:::\n\n", resultLabel(r))
		}
		b.WriteString("## Result\n\n| | |\n|---|---|\n")
		fmt.Fprintf(&b, "| **Status** | %s |\n", resultLabel(r))
		for _, row := range []struct{ k, v string }{
			{"Scheme", h.Scheme}, {"Certification body", h.Body}, {"Certificate", h.CertificateID},
			{"Scope", h.Scope}, {"Issued", h.Issued}, {"Valid until", h.ValidUntil},
		} {
			if row.v != "" {
				fmt.Fprintf(&b, "| **%s** | %s |\n", row.k, cell(row.v))
			}
		}
		if h.Summary != "" {
			fmt.Fprintf(&b, "\n%s\n", cell(h.Summary))
		}
		if len(docs) > 0 {
			b.WriteString("\n### Supporting documents\n\n")
			for _, d := range docs {
				fmt.Fprintf(&b, "- [%s](%s)\n", cell(d.Title), d.URL)
			}
		}
		b.WriteString("\n")
	}
	if v.SoA != nil {
		fmt.Fprintf(&b, "## Statement of applicability\n\n[%s](/audits/%s/soa) · %s\n", cell(soaTitle(v.SoA)), v.Slug, stateLabel(v.SoA.SoA.State()))
	}
	return b.String()
}

func soaTitle(s *soa.Loaded) string {
	if s.SoA.Header.Title != "" {
		return s.SoA.Header.Title
	}
	return s.SoA.Header.ID
}

func renderAssessmentsIndex(as []*assessmentView) string {
	var b strings.Builder
	b.WriteString("---\ntitle: \"Internal assessments\"\nsidebar_label: \"Overview\"\nsidebar_position: 0\n---\n\n# Internal assessments\n\n")
	b.WriteString(":::caution Internal\nAssessments generate findings and tracking issues. They are never published on the public site.\n:::\n\n")
	b.WriteString("| Assessment | Catalogs | Findings | Statement of applicability |\n|---|---|---|---|\n")
	for _, v := range as {
		st := "implicit (catalogs)"
		if v.SoA != nil {
			st = stateLabel(v.SoA.SoA.State())
		}
		fmt.Fprintf(&b, "| [%s](/audits/assessments/%s) | %s | %d | %s |\n", cell(v.Title), v.Slug, cell(strings.Join(v.Catalogs, ", ")), v.Findings, st)
	}
	return b.String()
}

func renderAssessmentPage(v *assessmentView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "---\ntitle: %q\nsidebar_label: %q\n---\n\n# %s\n\n", v.Title, v.Title, v.Title)
	b.WriteString(":::caution Internal assessment\nNever published on the public site.\n:::\n\n")
	if len(v.Catalogs) > 0 {
		fmt.Fprintf(&b, "**Catalogs (implicit statement of applicability):** %s\n\n", cell(strings.Join(v.Catalogs, ", ")))
	}
	fmt.Fprintf(&b, "**Findings:** %d\n\n", v.Findings)
	if v.SoA != nil {
		fmt.Fprintf(&b, "## Statement of applicability\n\n[%s](/audits/assessments/%s/soa) · %s\n", cell(soaTitle(v.SoA)), v.Slug, stateLabel(v.SoA.SoA.State()))
	}
	return b.String()
}
