package render

import (
	"strings"
	"testing"
)

func TestPatchNavbar(t *testing.T) {
	const items = "      items: [\n        {\n          type: 'docSidebar',\n          sidebarId: 'controlsSidebar',\n          position: 'left',\n          label: 'Controls',\n        },\n"
	htmlAnchor := items + "        {\n          type: 'html',\n          position: 'right',\n          value: '<a>gh</a>',\n        },\n      ],\n"
	hrefAnchor := items + "        {\n          href: 'https://github.com/sirosfoundation',\n          position: 'right',\n        },\n      ],\n"

	for name, cfg := range map[string]string{"html": htmlAnchor, "href": hrefAnchor} {
		t.Run(name, func(t *testing.T) {
			got, err := patchNavbar(cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			for _, label := range []string{"label: 'Findings'", "label: 'Risk Register'"} {
				if strings.Count(got, label) != 1 {
					t.Errorf("expected exactly one %q after patching, got:\n%s", label, got)
				}
			}
			again, err := patchNavbar(got, false)
			if err != nil || again != got {
				t.Errorf("patching is not idempotent (err=%v)", err)
			}
			public, err := patchNavbar(got, true)
			if err != nil || public != cfg {
				t.Errorf("public patch did not restore the original (err=%v):\n%s", err, public)
			}
		})
	}

	if _, err := patchNavbar(items+"      ],\n", false); err == nil {
		t.Error("expected an error when no navbar anchor exists")
	}
}

func TestPatchAuditsNavItem(t *testing.T) {
	base := "navbar: [\n" + navbarAnchors[0] + "\n"
	added := patchAuditsNavItem(base, true)
	if !strings.Contains(added, "auditsSidebar") || strings.Index(added, "auditsSidebar") > strings.Index(added, "https://github.com/sirosfoundation") {
		t.Errorf("Audits item not inserted before the anchor:\n%s", added)
	}
	if again := patchAuditsNavItem(added, true); again != added {
		t.Error("patching twice is not idempotent")
	}
	if removed := patchAuditsNavItem(added, false); removed != base {
		t.Errorf("disabling did not restore the original:\n%s", removed)
	}
	if noAnchor := patchAuditsNavItem("navbar: []\n", true); noAnchor != "navbar: []\n" {
		t.Error("config without an anchor must be left unchanged")
	}
}
