// Package github provides a client for syncing compliance findings with GitHub issues.
//
// It wraps go-github to:
//   - Create tracking issues for findings
//   - Discover linked issues/PRs from comments
//   - Sync issue state back to finding status
//   - Collect evidence from closed issues/PRs (merge info)
package github

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	gh "github.com/google/go-github/v72/github"
)

// Client wraps the GitHub API for compliance operations.
type Client struct {
	client *gh.Client
}

// IssueState holds the state of a GitHub issue relevant to compliance sync.
type IssueState struct {
	Number      int
	State       string // OPEN, CLOSED
	StateReason string // COMPLETED, NOT_PLANNED, REOPENED
	Labels      []string
	MergedAt    string // for PRs only
}

// IssueRef is a (repo, number) pair.
type IssueRef struct {
	Repo   string
	Number int
}

var (
	reIssueRef  = regexp.MustCompile(`([\w.-]+/[\w.-]+)#(\d+)`)
	reGitHubURL = regexp.MustCompile(`https://github\.com/([\w.-]+/[\w.-]+)/(?:issues|pull)/(\d+)`)
	rePullURL   = regexp.MustCompile(`https://github\.com/([\w.-]+/[\w.-]+)/pull/(\d+)`)
)

// New creates a Client. It expects GITHUB_TOKEN in the environment (via go-github default).
func New(ctx context.Context) (*Client, error) {
	client := gh.NewClient(nil).WithAuthToken("")
	// go-github v72 reads GITHUB_TOKEN from env automatically when token is empty
	// For explicit token, callers should set it via WithAuthToken.
	return &Client{client: client}, nil
}

// NewWithToken creates a Client with an explicit token.
func NewWithToken(ctx context.Context, token string) *Client {
	client := gh.NewClient(nil).WithAuthToken(token)
	return &Client{client: client}
}

func splitRepo(repo string) (string, string, error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repo: %q (expected owner/name)", repo)
	}
	return parts[0], parts[1], nil
}

// GetIssueState fetches the state of a single issue.
func (c *Client) GetIssueState(ctx context.Context, repo string, number int) (*IssueState, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	issue, _, err := c.client.Issues.Get(ctx, owner, name, number)
	if err != nil {
		return nil, fmt.Errorf("getting %s#%d: %w", repo, number, err)
	}

	state := &IssueState{
		Number: number,
		State:  strings.ToUpper(issue.GetState()),
	}
	if issue.StateReason != nil {
		state.StateReason = *issue.StateReason
	}
	for _, l := range issue.Labels {
		state.Labels = append(state.Labels, l.GetName())
	}

	return state, nil
}

// DiscoverCommentLinks finds issue/PR references in comments on a tracking issue.
// Returns (issues, prs) that are not self-references.
func (c *Client) DiscoverCommentLinks(ctx context.Context, repo string, number int) ([]IssueRef, []IssueRef, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, nil, err
	}

	opts := &gh.IssueListCommentsOptions{
		ListOptions: gh.ListOptions{PerPage: 100},
	}

	var issues, prs []IssueRef
	seen := make(map[string]bool)

	for {
		comments, resp, err := c.client.Issues.ListComments(ctx, owner, name, number, opts)
		if err != nil {
			return nil, nil, fmt.Errorf("listing comments on %s#%d: %w", repo, number, err)
		}

		for _, comment := range comments {
			body := comment.GetBody()
			// Find all /pull/ URLs first (these are definitively PRs)
			for _, m := range rePullURL.FindAllStringSubmatch(body, -1) {
				refRepo, refNum := m[1], mustAtoi(m[2])
				key := fmt.Sprintf("%s#%d", refRepo, refNum)
				if refRepo == repo && refNum == number {
					continue
				}
				if !seen[key] {
					seen[key] = true
					prs = append(prs, IssueRef{Repo: refRepo, Number: refNum})
				}
			}
			// Then find all generic refs
			for _, m := range reIssueRef.FindAllStringSubmatch(body, -1) {
				refRepo, refNum := m[1], mustAtoi(m[2])
				key := fmt.Sprintf("%s#%d", refRepo, refNum)
				if refRepo == repo && refNum == number {
					continue
				}
				if !seen[key] {
					seen[key] = true
					issues = append(issues, IssueRef{Repo: refRepo, Number: refNum})
				}
			}
			for _, m := range reGitHubURL.FindAllStringSubmatch(body, -1) {
				refRepo, refNum := m[1], mustAtoi(m[2])
				key := fmt.Sprintf("%s#%d", refRepo, refNum)
				if !seen[key] {
					if refRepo == repo && refNum == number {
						continue
					}
					seen[key] = true
					issues = append(issues, IssueRef{Repo: refRepo, Number: refNum})
				}
			}
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return issues, prs, nil
}

// CreateIssue creates a new GitHub issue and returns the issue number.
func (c *Client) CreateIssue(ctx context.Context, repo, title, body string, labels []string) (int, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return 0, err
	}

	req := &gh.IssueRequest{
		Title:  &title,
		Body:   &body,
		Labels: &labels,
	}

	issue, _, err := c.client.Issues.Create(ctx, owner, name, req)
	if err != nil {
		return 0, fmt.Errorf("creating issue in %s: %w", repo, err)
	}

	return issue.GetNumber(), nil
}

// EnsureLabels creates labels that don't exist yet.
func (c *Client) EnsureLabels(ctx context.Context, repo string, labels map[string]string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}

	existing, _, err := c.client.Issues.ListLabels(ctx, owner, name, &gh.ListOptions{PerPage: 200})
	if err != nil {
		return fmt.Errorf("listing labels in %s: %w", repo, err)
	}

	existingSet := make(map[string]bool, len(existing))
	for _, l := range existing {
		existingSet[l.GetName()] = true
	}

	for label, color := range labels {
		if existingSet[label] {
			continue
		}
		_, _, err := c.client.Issues.CreateLabel(ctx, owner, name, &gh.Label{
			Name:  &label,
			Color: &color,
		})
		if err != nil {
			return fmt.Errorf("creating label %q in %s: %w", label, repo, err)
		}
	}

	return nil
}

// GetPRMergeInfo returns merge metadata for a pull request.
// Returns (merged bool, mergedAt string, mergeCommitSHA string, err).
func (c *Client) GetPRMergeInfo(ctx context.Context, repo string, number int) (bool, string, string, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return false, "", "", err
	}

	pr, _, err := c.client.PullRequests.Get(ctx, owner, name, number)
	if err != nil {
		return false, "", "", fmt.Errorf("getting PR %s#%d: %w", repo, number, err)
	}

	if !pr.GetMerged() {
		return false, "", "", nil
	}

	mergedAt := ""
	if pr.MergedAt != nil {
		mergedAt = pr.MergedAt.Format("2006-01-02")
	}

	return true, mergedAt, pr.GetMergeCommitSHA(), nil
}

// AddLabels adds labels to an existing issue.
func (c *Client) AddLabels(ctx context.Context, repo string, number int, labels []string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	_, _, err = c.client.Issues.AddLabelsToIssue(ctx, owner, name, number, labels)
	if err != nil {
		return fmt.Errorf("adding labels to %s#%d: %w", repo, number, err)
	}
	return nil
}
func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
