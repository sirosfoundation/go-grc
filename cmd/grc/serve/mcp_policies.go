package serve

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/sirosfoundation/go-grc/pkg/policy"
)

// policyRecord is one row of the document register: the metadata of a policy
// as the document card shows it, plus its place in git.
type policyRecord struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	DocumentID  string   `json:"document_id,omitempty"`
	Visibility  string   `json:"visibility"`
	Status      string   `json:"status"`
	Version     string   `json:"version,omitempty"`
	Owner       string   `json:"owner,omitempty"`
	Approver    string   `json:"approver,omitempty"`
	ReviewCycle string   `json:"review_cycle,omitempty"`
	ApprovedOn  string   `json:"approved_date,omitempty"`
	NextReview  string   `json:"next_review,omitempty"`
	Overdue     bool     `json:"review_overdue,omitempty"`
	Revisions   int      `json:"revisions,omitempty"`
	LastChange  string   `json:"last_change,omitempty"`
	LastCommit  string   `json:"last_commit,omitempty"`
	Missing     []string `json:"missing_metadata,omitempty"`
}

func newPolicyRecord(ctx context.Context, p *policy.Policy, now time.Time) policyRecord {
	r := policyRecord{
		Slug: p.Slug, Title: p.Title, DocumentID: p.DocID, Visibility: p.Visibility, Status: p.State(),
		Version: p.Version, Owner: p.Owner, Approver: p.Approver, ReviewCycle: p.ReviewCycle,
		ApprovedOn: p.ApprovedDate, Missing: p.Warnings(),
	}
	if r.Visibility == "" {
		r.Visibility = policy.VisibilityPrivate
	}
	if next, ok := p.NextReview(); ok {
		r.NextReview = next.Format("2006-01-02")
		r.Overdue = now.After(next)
	}
	r.Revisions = policy.Revisions(ctx, p.Path)
	if c := policy.LastChange(ctx, p.Path); c != nil {
		r.LastChange = c.Time.Format(time.RFC3339)
		r.LastCommit = c.SHA
	}
	return r
}

// registerPolicyResources exposes the document register and each policy, so
// that an agent can follow the document workflow without the repository.
func registerPolicyResources(s *mcpserver.MCPServer, data *complianceData) {
	s.AddResource(
		mcp.NewResource("grc://policies", "Document Register",
			mcp.WithResourceDescription("Every policy and procedure with its metadata (document ID, visibility, status, version, owner, approver, review cycle, approval and next review dates, last change in git). Metadata lives in each document's front matter; drafts have no version, only revisions."),
			mcp.WithMIMEType("application/json"),
		),
		func(ctx context.Context, _ mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			now := time.Now()
			recs := make([]policyRecord, 0, len(data.policies))
			for _, p := range data.policies {
				recs = append(recs, newPolicyRecord(ctx, p, now))
			}
			return jsonResource("grc://policies", recs)
		},
	)
	s.AddResourceTemplate(
		mcp.NewResourceTemplate("grc://policy/{slug}", "Policy",
			mcp.WithTemplateDescription("The full text of one policy (Markdown, front matter removed) after its register entry"),
		),
		func(ctx context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
			data.mu.RLock()
			defer data.mu.RUnlock()
			slug := extractTemplateParam(req.Params.URI, "grc://policy/")
			for _, p := range data.policies {
				if p.Slug != slug {
					continue
				}
				rec := newPolicyRecord(ctx, p, time.Now())
				var b strings.Builder
				fmt.Fprintf(&b, "<!-- register: %s -->\n\n%s", registerLine(rec), p.Body)
				return []mcp.ResourceContents{mcp.TextResourceContents{URI: req.Params.URI, MIMEType: "text/markdown", Text: b.String()}}, nil
			}
			return nil, fmt.Errorf("policy %q not found", slug)
		},
	)
}

func registerLine(r policyRecord) string {
	parts := []string{"id=" + r.DocumentID, "status=" + r.Status, "visibility=" + r.Visibility}
	if r.Version != "" {
		parts = append(parts, "version="+r.Version)
	}
	if r.Owner != "" {
		parts = append(parts, "owner="+r.Owner)
	}
	if r.NextReview != "" {
		parts = append(parts, "next_review="+r.NextReview)
	}
	return strings.Join(parts, " ")
}
