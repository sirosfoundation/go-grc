// Package result loads the outcome of an external audit, the compliance
// result that is published on the site (a certificate) together with its
// supporting documents.
//
// A result lives in "<audits>/<cycle>/result.yaml". It is published only once
// it has been issued: every other state, a missing status included, is
// internal.
package result

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Lifecycle of an external audit's outcome.
const (
	StatusPlanned   = "planned"    // audit scheduled
	StatusPreAudit  = "pre_audit"  // internal pre-audit (assessment) under way
	StatusInAudit   = "in_audit"   // external auditor engaged
	StatusIssued    = "issued"     // certificate / positive result received
	StatusNotIssued = "not_issued" // audit completed without a certificate
	StatusWithdrawn = "withdrawn"  // certificate withdrawn or lapsed
)

var validStatus = map[string]bool{
	StatusPlanned: true, StatusPreAudit: true, StatusInAudit: true,
	StatusIssued: true, StatusNotIssued: true, StatusWithdrawn: true,
}

// Document is a supporting document: a file inside the audit's directory, or
// an external URL.
type Document struct {
	Title string `yaml:"title"`
	File  string `yaml:"file,omitempty"` // relative to the audit directory
	URL   string `yaml:"url,omitempty"`
}

// Header is the "result:" block.
type Header struct {
	Status        string     `yaml:"status"`
	Scheme        string     `yaml:"scheme,omitempty"`
	Body          string     `yaml:"body,omitempty"` // certification body
	CertificateID string     `yaml:"certificate_id,omitempty"`
	Scope         string     `yaml:"scope,omitempty"`
	Issued        string     `yaml:"issued,omitempty"`
	ValidUntil    string     `yaml:"valid_until,omitempty"`
	Summary       string     `yaml:"summary,omitempty"`
	Documents     []Document `yaml:"documents,omitempty"`
}

// Result is a loaded result.yaml.
type Result struct {
	Header Header `yaml:"result"`
}

// Loaded is a result together with the audit it belongs to.
type Loaded struct {
	Cycle  string // audit directory name
	Dir    string // absolute or root-relative path of the audit directory
	Path   string
	Result Result
}

// Published reports whether the result may appear on the public site.
func (r *Result) Published() bool { return r.Header.Status == StatusIssued }

// State returns the status, treating anything unknown as planned.
func (r *Result) State() string {
	if validStatus[r.Header.Status] {
		return r.Header.Status
	}
	return StatusPlanned
}

// Load reads "<auditsDir>/<cycle>/result.yaml" for every cycle that has one,
// ordered by cycle. A missing directory yields none.
func Load(auditsDir string) ([]*Loaded, error) {
	entries, err := os.ReadDir(auditsDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading audits dir: %w", err)
	}
	var out []*Loaded
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(auditsDir, e.Name())
		path := filepath.Join(dir, "result.yaml")
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", path, err)
		}
		var r Result
		if err := yaml.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", path, err)
		}
		out = append(out, &Loaded{Cycle: e.Name(), Dir: dir, Path: path, Result: r})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Cycle < out[j].Cycle })
	return out, nil
}

// AllowedExt are the file types that may be published as supporting documents.
var AllowedExt = map[string]bool{".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".svg": true, ".txt": true}

// SafeFile resolves a document's file inside dir. It rejects absolute paths,
// anything escaping dir, non-regular files and file types outside AllowedExt.
func SafeFile(dir, file string) (string, error) {
	if file == "" || filepath.IsAbs(file) {
		return "", fmt.Errorf("file %q must be a relative path inside the audit directory", file)
	}
	clean := filepath.Clean(file)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file %q escapes the audit directory", file)
	}
	if !AllowedExt[strings.ToLower(filepath.Ext(clean))] {
		return "", fmt.Errorf("file %q: type not allowed for publication", file)
	}
	full := filepath.Join(dir, clean)
	info, err := os.Lstat(full)
	if err != nil {
		return "", fmt.Errorf("file %q: %w", file, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("file %q is not a regular file", file)
	}
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	realFile, err := filepath.EvalSymlinks(full)
	if err != nil {
		return "", err
	}
	if rel, err := filepath.Rel(realDir, realFile); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("file %q resolves outside the audit directory", file)
	}
	return full, nil
}

// Validate returns a description of every problem in the result.
func (l *Loaded) Validate() []string {
	h := l.Result.Header
	var p []string
	if !validStatus[h.Status] {
		p = append(p, fmt.Sprintf("status is %q, want planned, pre_audit, in_audit, issued, not_issued or withdrawn", h.Status))
	}
	if h.Status == StatusIssued {
		if h.Scheme == "" {
			p = append(p, "issued result has no scheme")
		}
		if h.Body == "" {
			p = append(p, "issued result has no certification body")
		}
		if h.Issued == "" {
			p = append(p, "issued result has no issued date")
		}
	}
	for _, d := range []struct{ name, v string }{{"issued", h.Issued}, {"valid_until", h.ValidUntil}} {
		if d.v != "" {
			if _, err := time.Parse("2006-01-02", d.v); err != nil {
				p = append(p, fmt.Sprintf("%s %q is not a YYYY-MM-DD date", d.name, d.v))
			}
		}
	}
	for i, d := range h.Documents {
		if d.Title == "" {
			p = append(p, fmt.Sprintf("document %d has no title", i+1))
		}
		switch {
		case d.File != "" && d.URL != "":
			p = append(p, fmt.Sprintf("document %q sets both file and url", d.Title))
		case d.File != "":
			if _, err := SafeFile(l.Dir, d.File); err != nil {
				p = append(p, fmt.Sprintf("document %q: %v", d.Title, err))
			}
		case d.URL != "":
			if !strings.HasPrefix(d.URL, "https://") {
				p = append(p, fmt.Sprintf("document %q: url must be https", d.Title))
			}
		default:
			p = append(p, fmt.Sprintf("document %q has neither file nor url", d.Title))
		}
	}
	return p
}
