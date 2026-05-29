package github

import (
	"testing"
)

func TestSplitRepo(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{"owner/repo", "owner", "repo", false},
		{"org/my-repo", "org", "my-repo", false},
		{"noslash", "", "", true},
		{"a/b/c", "a", "b/c", false}, // SplitN with n=2
	}
	for _, tt := range tests {
		owner, name, err := splitRepo(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("splitRepo(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr {
			if owner != tt.wantOwner || name != tt.wantName {
				t.Errorf("splitRepo(%q) = (%q, %q), want (%q, %q)", tt.input, owner, name, tt.wantOwner, tt.wantName)
			}
		}
	}
}

func TestIssueRefRegex(t *testing.T) {
	tests := []struct {
		input string
		want  []string // "repo#number"
	}{
		{"sirosfoundation/wallet-frontend#74", []string{"sirosfoundation/wallet-frontend#74"}},
		{"see owner/repo#123 and org/other#456", []string{"owner/repo#123", "org/other#456"}},
		{"no refs here", nil},
	}
	for _, tt := range tests {
		matches := reIssueRef.FindAllStringSubmatch(tt.input, -1)
		var got []string
		for _, m := range matches {
			got = append(got, m[0])
		}
		if len(got) != len(tt.want) {
			t.Errorf("reIssueRef on %q: got %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("reIssueRef on %q[%d]: got %q, want %q", tt.input, i, got[i], tt.want[i])
			}
		}
	}
}

func TestGitHubURLRegex(t *testing.T) {
	tests := []struct {
		input    string
		wantRepo string
		wantNum  string
	}{
		{"https://github.com/org/repo/issues/42", "org/repo", "42"},
		{"https://github.com/org/repo/pull/7", "org/repo", "7"},
	}
	for _, tt := range tests {
		m := reGitHubURL.FindStringSubmatch(tt.input)
		if m == nil {
			t.Errorf("reGitHubURL did not match %q", tt.input)
			continue
		}
		if m[1] != tt.wantRepo || m[2] != tt.wantNum {
			t.Errorf("reGitHubURL on %q = (%q, %q), want (%q, %q)", tt.input, m[1], m[2], tt.wantRepo, tt.wantNum)
		}
	}
}

func TestPullURLRegex(t *testing.T) {
	m := rePullURL.FindStringSubmatch("https://github.com/org/repo/pull/99")
	if m == nil {
		t.Fatal("rePullURL did not match")
	}
	if m[1] != "org/repo" || m[2] != "99" {
		t.Errorf("got (%q, %q), want (org/repo, 99)", m[1], m[2])
	}

	// Should not match issues URLs
	m2 := rePullURL.FindStringSubmatch("https://github.com/org/repo/issues/42")
	if m2 != nil {
		t.Error("rePullURL should not match issues URL")
	}
}
