package sync

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/audit"
	"github.com/sirosfoundation/go-grc/pkg/config"
	ghpkg "github.com/sirosfoundation/go-grc/pkg/github"
)

// NewCommand returns the sync cobra command.
func NewCommand() *cobra.Command {
	var (
		dryRun bool
		repo   string
	)

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync findings with GitHub issues and collect evidence",
		Long: `Synchronize compliance findings with GitHub tracking issues.

Phase 1: Create tracking issues for findings that don't have one.
Phase 2: Discover linked issues/PRs from tracking issue comments.
Phase 3: Sync issue state - when tracking issues close, mark findings resolved.
Phase 4: Collect evidence from closed issues and merged PRs.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root, repo, dryRun)
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show what would happen without making changes")
	cmd.Flags().StringVar(&repo, "repo", "", "Target repo for new tracking issues (default: from .grc.yaml)")

	linkCmd := &cobra.Command{
		Use:   "link FINDING_ID OWNER/REPO#NUMBER",
		Short: "Link an implementation issue or PR to a finding",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			isPR, _ := cmd.Flags().GetBool("pr")
			return runLink(root, args[0], args[1], isPR)
		},
	}
	linkCmd.Flags().Bool("pr", false, "Link as a pull request instead of an issue")
	cmd.AddCommand(linkCmd)

	return cmd
}

func run(root, repo string, dryRun bool) error {
	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if repo == "" {
		repo = cfg.Project.Repo
	}
	ctx := context.Background()

	token := os.Getenv("GITHUB_TOKEN")
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token == "" {
		return fmt.Errorf("GITHUB_TOKEN (or GH_TOKEN) environment variable not set — create one at https://github.com/settings/tokens")
	}

	client := ghpkg.NewWithToken(ctx, token)

	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}

	var stats struct {
		created, checked, updated, evidenceCollected, linksDiscovered int
	}

	// Phase 1: Create tracking issues for untracked findings
	ensureLabels := map[string]string{
		"severity:critical": "b60205",
		"severity:high":     "d93f0b",
		"severity:medium":   "fbca04",
		"severity:low":      "0e8a16",
		"owner:platform":    "1d76db",
		"owner:operator":    "d876e3",
		"status:accepted":   "cfd3d7",
		"compliance":        "0e8a16",
	}

	if !dryRun {
		if err := client.EnsureLabels(ctx, repo, ensureLabels); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not ensure labels: %v\n", err)
		}
	}

	for fi := range audits.Files {
		file := &audits.Files[fi]
		auditID := file.Data.Audit.ID
		created := 0
		for i := range file.Data.Findings {
			f := &file.Data.Findings[i]
			if f.IsTerminal() || f.TrackingIssue != nil {
				continue
			}

			labels := []string{"compliance", fmt.Sprintf("audit:%s", strings.ToLower(auditID))}
			if _, ok := ensureLabels["severity:"+f.Severity]; ok {
				labels = append(labels, "severity:"+f.Severity)
			}
			if f.Owner != "" {
				for _, o := range strings.Split(f.Owner, ", ") {
					labels = append(labels, "owner:"+o)
				}
			}
			title := fmt.Sprintf("[%s] %s", f.ID, f.Title)
			body := buildIssueBody(f, &file.Data.Audit)

			if dryRun {
				fmt.Printf("  DRY RUN: would create issue for %s\n", f.ID)
				continue
			}

			num, err := client.CreateIssue(ctx, repo, title, body, labels)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  ERROR creating issue for %s: %v\n", f.ID, err)
				continue
			}

			f.TrackingIssue = &audit.IssueRef{Repo: repo, Number: num}
			fmt.Printf("  Created #%d: %s\n", num, f.ID)
			created++
			stats.created++
		}

		if !dryRun && created > 0 {
			if err := file.Save(); err != nil {
				return fmt.Errorf("saving %s: %w", file.Path, err)
			}
		}
	}

	// Phase 2-4: Sync existing tracked findings
	modified := make(map[*audit.LoadedFile]bool)

	for fi := range audits.Files {
		file := &audits.Files[fi]
		for i := range file.Data.Findings {
			f := &file.Data.Findings[i]
			if f.TrackingIssue == nil {
				continue
			}

			tRepo := f.TrackingIssue.Repo
			tNum := f.TrackingIssue.Number

			// Phase 2: Discover links from comments
			commentIssues, commentPRs, err := client.DiscoverCommentLinks(ctx, tRepo, tNum)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
			}
			for _, ref := range commentIssues {
				if !hasIssueRef(f.Issues, ref) {
					f.Issues = append(f.Issues, audit.IssueRef{Repo: ref.Repo, Number: ref.Number})
					stats.linksDiscovered++
					modified[file] = true
					if !dryRun {
						fmt.Printf("  %s: discovered issue %s#%d\n", f.ID, ref.Repo, ref.Number)
					}
				}
			}
			for _, ref := range commentPRs {
				if !hasIssueRef(f.PullRequests, ref) {
					f.PullRequests = append(f.PullRequests, audit.IssueRef{Repo: ref.Repo, Number: ref.Number})
					stats.linksDiscovered++
					modified[file] = true
					if !dryRun {
						fmt.Printf("  %s: discovered PR %s#%d\n", f.ID, ref.Repo, ref.Number)
					}
				}
			}

			// Phase 3: Sync tracking issue state and owner
			state, err := client.GetIssueState(ctx, tRepo, tNum)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Warning: %v\n", err)
				continue
			}
			stats.checked++

			// Sync owner: labels → YAML (authoritative)
			issueOwner := ownerFromLabels(state.Labels)
			if issueOwner != "" && issueOwner != f.Owner {
				if dryRun {
					fmt.Printf("  DRY RUN: %s owner %s -> %s\n", f.ID, f.Owner, issueOwner)
				} else {
					fmt.Printf("  %s: owner %s -> %s\n", f.ID, f.Owner, issueOwner)
					f.Owner = issueOwner
					modified[file] = true
					stats.updated++
				}
			} else if issueOwner == "" && f.Owner != "" {
				// No owner labels on issue but YAML has owner → apply labels
				var labels []string
				for _, o := range strings.Split(f.Owner, ", ") {
					labels = append(labels, "owner:"+o)
				}
				if !dryRun {
					if err := client.AddLabels(ctx, tRepo, tNum, labels); err != nil {
						fmt.Fprintf(os.Stderr, "  Warning: %s: could not sync owner labels: %v\n", f.ID, err)
					} else {
						fmt.Printf("  %s: synced owner labels %v to issue\n", f.ID, labels)
					}
				} else {
					fmt.Printf("  DRY RUN: %s would add owner labels %v\n", f.ID, labels)
				}
			}

			oldStatus := f.Status
			newStatus := ""
			if state.State == "CLOSED" && state.StateReason == "NOT_PLANNED" && f.Status != "accepted" {
				newStatus = "accepted"
			} else if state.State == "CLOSED" && state.StateReason != "NOT_PLANNED" && f.Status != "resolved" {
				newStatus = "resolved"
			} else if state.State == "OPEN" && f.Status == "resolved" {
				// Reopened issue → revert resolved finding
				if hasLabel(state.Labels, "in-progress") {
					newStatus = "in_progress"
				} else {
					newStatus = "open"
				}
			} else if state.State == "OPEN" && f.Status == "accepted" {
				// Reopened after risk acceptance → revert
				newStatus = "open"
			} else if state.State == "OPEN" && hasLabel(state.Labels, "in-progress") && f.Status == "open" {
				newStatus = "in_progress"
			}

			// Check if all implementation issues are closed
			if newStatus == "" && (f.Status == "open" || f.Status == "in_progress") && len(f.Issues) > 0 {
				allClosed := true
				for _, impl := range f.Issues {
					implState, err := client.GetIssueState(ctx, impl.Repo, impl.Number)
					if err != nil || implState.State != "CLOSED" || implState.StateReason == "NOT_PLANNED" {
						allClosed = false
						break
					}
				}
				if allClosed {
					newStatus = "resolved"
					fmt.Printf("  %s: all implementation issues closed\n", f.ID)
				}
			}

			if newStatus != "" && newStatus != oldStatus {
				if dryRun {
					fmt.Printf("  DRY RUN: %s %s -> %s\n", f.ID, oldStatus, newStatus)
				} else {
					f.Status = newStatus
					if newStatus == "resolved" || newStatus == "accepted" {
						f.ResolvedDate = time.Now().UTC().Format("2006-01-02")
					}
					if newStatus == "open" || newStatus == "in_progress" {
						f.ResolvedDate = "" // clear on reopen
					}
					modified[file] = true
					stats.updated++
					fmt.Printf("  %s: %s -> %s\n", f.ID, oldStatus, newStatus)
				}
			}

			// Phase 4: Collect evidence from merged PRs
			if f.Status == "resolved" || newStatus == "resolved" {
				for _, pr := range f.PullRequests {
					if hasEvidenceRef(f.Evidence, pr.Repo, pr.Number) {
						continue
					}
					merged, mergedAt, _, err := client.GetPRMergeInfo(ctx, pr.Repo, pr.Number)
					if err != nil {
						fmt.Fprintf(os.Stderr, "  Warning: cannot check PR %s#%d: %v\n", pr.Repo, pr.Number, err)
						continue
					}
					if merged {
						ev := audit.Evidence{
							Type:        "merged_pr",
							Ref:         fmt.Sprintf("%s#%d", pr.Repo, pr.Number),
							Description: fmt.Sprintf("Merged on %s", mergedAt),
							CollectedAt: time.Now().UTC().Format("2006-01-02"),
						}
						f.AddEvidence(ev)
						modified[file] = true
						stats.evidenceCollected++
						if !dryRun {
							fmt.Printf("  %s: collected evidence from PR %s#%d\n", f.ID, pr.Repo, pr.Number)
						}
					}
				}
			}
		}
	}

	if !dryRun {
		for file := range modified {
			if err := file.Save(); err != nil {
				return fmt.Errorf("saving %s: %w", file.Path, err)
			}
		}
	}

	suffix := ""
	if dryRun {
		suffix = " (dry run)"
	}
	fmt.Printf("\nDone. Created %d, checked %d, updated %d, evidence %d, links %d%s\n",
		stats.created, stats.checked, stats.updated, stats.evidenceCollected, stats.linksDiscovered, suffix)

	return nil
}

func runLink(root, findingID, ref string, isPR bool) error {
	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	audits, err := audit.Load(cfg.AuditsDir)
	if err != nil {
		return fmt.Errorf("loading audits: %w", err)
	}

	fref, ok := audits.FindingsByID[findingID]
	if !ok {
		return fmt.Errorf("finding %q not found in any audit file", findingID)
	}

	issueRef, err := parseIssueRef(ref)
	if err != nil {
		return err
	}

	f := fref.Finding
	if isPR {
		if hasIssueRef(f.PullRequests, *issueRef) {
			fmt.Printf("Already linked: %s#%d -> %s\n", issueRef.Repo, issueRef.Number, findingID)
			return nil
		}
		f.PullRequests = append(f.PullRequests, audit.IssueRef{Repo: issueRef.Repo, Number: issueRef.Number})
	} else {
		if hasIssueRef(f.Issues, *issueRef) {
			fmt.Printf("Already linked: %s#%d -> %s\n", issueRef.Repo, issueRef.Number, findingID)
			return nil
		}
		f.Issues = append(f.Issues, audit.IssueRef{Repo: issueRef.Repo, Number: issueRef.Number})
	}

	if err := fref.File.Save(); err != nil {
		return fmt.Errorf("saving: %w", err)
	}

	kind := "Issue"
	if isPR {
		kind = "PR"
	}
	fmt.Printf("Linked %s %s#%d -> %s\n", kind, issueRef.Repo, issueRef.Number, findingID)
	return nil
}

func buildIssueBody(f *audit.Finding, a *audit.Audit) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Compliance Finding: %s\n\n", f.ID)
	fmt.Fprintf(&b, "**Audit:** %s\n", a.Title)
	fmt.Fprintf(&b, "**Severity:** %s\n", f.Severity)
	fmt.Fprintf(&b, "**Owner:** %s\n\n", f.Owner)
	if f.Description != "" {
		fmt.Fprintf(&b, "### Description\n\n%s\n\n", f.Description)
	}
	if len(f.Controls) > 0 {
		fmt.Fprintf(&b, "**Controls:** %s\n\n", strings.Join(f.Controls, ", "))
	}
	fmt.Fprintf(&b, "---\n*Auto-generated by grc sync. Finding ID: `%s`, Audit: `%s`.*\n", f.ID, a.ID)
	return b.String()
}

func parseIssueRef(s string) (*ghpkg.IssueRef, error) {
	parts := strings.SplitN(s, "#", 2)
	if len(parts) == 2 {
		num, err := strconv.Atoi(parts[1])
		if err == nil && strings.Contains(parts[0], "/") {
			return &ghpkg.IssueRef{Repo: parts[0], Number: num}, nil
		}
	}
	return nil, fmt.Errorf("invalid issue reference: %q (expected owner/repo#123)", s)
}

func hasIssueRef(refs []audit.IssueRef, ref ghpkg.IssueRef) bool {
	for _, r := range refs {
		if r.Repo == ref.Repo && r.Number == ref.Number {
			return true
		}
	}
	return false
}

func hasLabel(labels []string, target string) bool {
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

func ownerFromLabels(labels []string) string {
	var owners []string
	for _, l := range labels {
		if strings.HasPrefix(l, "owner:") {
			owners = append(owners, strings.TrimPrefix(l, "owner:"))
		}
	}
	sort.Strings(owners)
	return strings.Join(owners, ", ")
}

func hasEvidenceRef(evidence []audit.Evidence, repo string, number int) bool {
	ref := fmt.Sprintf("%s#%d", repo, number)
	for _, ev := range evidence {
		if ev.Ref == ref {
			return true
		}
	}
	return false
}
