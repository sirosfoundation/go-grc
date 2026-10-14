// Package policy loads policy documents (Markdown with YAML front matter).
//
// Two independent front matter properties matter:
//
//	visibility: public | private   who may read it (decides publication)
//	status:     draft | in_review | approved   where it is in its lifecycle
//
// Status never influences visibility. A missing or unknown visibility is
// private (fail closed); a missing or unknown status reads as draft.
package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"

	StatusDraft    = "draft"
	StatusInReview = "in_review"
	StatusApproved = "approved"
)

// Policy is one loaded document.
type Policy struct {
	Slug         string // file name without extension
	Path         string
	Title        string
	Visibility   string
	Status       string
	ApprovedDate string
	Meta         map[string]any // remaining front matter, passed through to the site
	Body         string         // Markdown after the front matter
}

// Public reports whether the policy may appear on the public site.
// Only an explicit, exact "public" qualifies.
func (p *Policy) Public() bool { return p.Visibility == VisibilityPublic }

// State returns the lifecycle status, defaulting to draft.
func (p *Policy) State() string {
	switch p.Status {
	case StatusInReview, StatusApproved:
		return p.Status
	}
	return StatusDraft
}

var h1 = regexp.MustCompile(`(?m)^#\s+(.+?)\s*$`)

// Load reads every *.md file directly under dir, sorted by slug.
// A missing directory yields no policies.
func Load(dir string) ([]*Policy, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Policy
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".md" || strings.EqualFold(e.Name(), "README.md") {
			continue
		}
		p, err := parse(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

func parse(path string) (*Policy, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	p := &Policy{Slug: strings.TrimSuffix(filepath.Base(path), ".md"), Path: path, Meta: map[string]any{}, Body: text}
	if strings.HasPrefix(text, "---\n") {
		if end := strings.Index(text[4:], "\n---\n"); end >= 0 {
			if err := yaml.Unmarshal([]byte(text[4:4+end]), &p.Meta); err != nil {
				return nil, fmt.Errorf("%s: front matter: %w", path, err)
			}
			p.Body = text[4+end+5:]
		}
	}
	take := func(key string) string {
		v, _ := p.Meta[key].(string)
		delete(p.Meta, key)
		return v
	}
	p.Visibility = take("visibility")
	p.Status = take("status")
	p.ApprovedDate = fmtDate(p.Meta["approved_date"])
	delete(p.Meta, "approved_date")
	p.Title, _ = p.Meta["title"].(string)
	if p.Title == "" {
		if m := h1.FindStringSubmatch(p.Body); m != nil {
			p.Title = m[1]
		} else {
			p.Title = p.Slug
		}
	}
	return p, nil
}

func fmtDate(v any) string {
	switch d := v.(type) {
	case string:
		return d
	case nil:
		return ""
	case time.Time:
		return d.Format("2006-01-02")
	default:
		return strings.TrimSpace(fmt.Sprint(d))
	}
}

// Validate returns a description of every problem in the policy.
func (p *Policy) Validate() []string {
	var problems []string
	switch p.Visibility {
	case VisibilityPublic, VisibilityPrivate:
	case "":
		problems = append(problems, "visibility is not set (want public or private); treated as private")
	default:
		problems = append(problems, fmt.Sprintf("visibility is %q, want public or private", p.Visibility))
	}
	switch p.Status {
	case StatusDraft, StatusInReview, StatusApproved:
	case "":
		problems = append(problems, "status is not set (want draft, in_review or approved); treated as draft")
	default:
		problems = append(problems, fmt.Sprintf("status is %q, want draft, in_review or approved", p.Status))
	}
	if p.Status == StatusApproved && p.ApprovedDate == "" {
		problems = append(problems, "status is approved but approved_date is empty")
	}
	return problems
}
