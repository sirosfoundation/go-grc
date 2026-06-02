package oscal

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sirosfoundation/go-grc/pkg/catalog"
	"github.com/sirosfoundation/go-grc/pkg/config"
)

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "oscal",
		Short: "Generate OSCAL component-definition JSON from catalog",
		RunE: func(cmd *cobra.Command, args []string) error {
			root, _ := cmd.Flags().GetString("root")
			return run(root)
		},
	}
	return cmd
}

// OSCAL 1.1.2 component-definition types (subset needed for export)

type ComponentDefinitionDoc struct {
	ComponentDefinition ComponentDefinition `json:"component-definition"`
}

type ComponentDefinition struct {
	UUID       string      `json:"uuid"`
	Metadata   Metadata    `json:"metadata"`
	Components []Component `json:"components"`
}

type Metadata struct {
	Title        string `json:"title"`
	LastModified string `json:"last-modified"`
	Version      string `json:"version"`
	OSCALVersion string `json:"oscal-version"`
	Roles        []Role `json:"roles"`
}

type Role struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type Component struct {
	UUID                   string                  `json:"uuid"`
	Type                   string                  `json:"type"`
	Title                  string                  `json:"title"`
	Description            string                  `json:"description"`
	ControlImplementations []ControlImplementation `json:"control-implementations"`
}

type ControlImplementation struct {
	UUID                    string                   `json:"uuid"`
	Source                  string                   `json:"source"`
	Description             string                   `json:"description"`
	ImplementedRequirements []ImplementedRequirement `json:"implemented-requirements"`
}

type ImplementedRequirement struct {
	UUID        string `json:"uuid"`
	ControlID   string `json:"control-id"`
	Description string `json:"description"`
	Props       []Prop `json:"props"`
	Links       []Link `json:"links,omitempty"`
}

type Prop struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Link struct {
	Href string `json:"href"`
	Text string `json:"text,omitempty"`
}

// uuid5 generates a deterministic v5 UUID from a namespace UUID and a name.
func uuid5(namespace [16]byte, name string) string {
	h := sha1.New()
	h.Write(namespace[:])
	h.Write([]byte(name))
	sum := h.Sum(nil)

	// Set version 5
	sum[6] = (sum[6] & 0x0f) | 0x50
	// Set variant RFC 4122
	sum[8] = (sum[8] & 0x3f) | 0x80

	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		sum[0:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16])
}

// nsURL is the RFC 4122 URL namespace UUID.
var nsURL = [16]byte{
	0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1,
	0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8,
}

func run(root string) error {
	cfg, err := config.New(root)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cat, err := catalog.Load(cfg.CatalogDir, cfg.CatalogSubdirs...)
	if err != nil {
		return fmt.Errorf("loading catalog: %w", err)
	}

	projectURL := cfg.Project.URL
	if projectURL == "" {
		projectURL = "https://compliance.siros.org"
	}

	// Build a map of component name → repo URL from config
	compRepos := make(map[string]string)
	for _, c := range cfg.Components {
		if c.Repo != "" {
			compRepos[c.Name] = "https://github.com/" + c.Repo
		}
	}

	// Build implemented-requirements from catalog controls
	var reqs []ImplementedRequirement

	// Sort groups by ID for deterministic output
	groups := make([]catalog.Group, len(cat.Groups))
	copy(groups, cat.Groups)
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })

	for _, group := range groups {
		// Sort controls within group
		ctrls := make([]catalog.Control, len(group.Controls))
		copy(ctrls, group.Controls)
		sort.Slice(ctrls, func(i, j int) bool { return ctrls[i].ID < ctrls[j].ID })

		for _, ctrl := range ctrls {
			props := []Prop{
				{Name: "status", Value: statusOrDefault(ctrl.Status)},
				{Name: "category", Value: ctrl.Category},
				{Name: "csf_function", Value: ctrl.CSFFunction},
				{Name: "owner", Value: ctrl.Owner},
				{Name: "group", Value: groupSlug(group.Title)},
			}

			// Components as individual props
			for _, comp := range ctrl.Components {
				props = append(props, Prop{Name: "component", Value: comp})
			}

			// Build links from references and component repos
			var links []Link
			for _, ref := range ctrl.References {
				// If ref looks like a path within a component repo, create a link
				for _, comp := range ctrl.Components {
					if repo, ok := compRepos[comp]; ok {
						links = append(links, Link{
							Href: repo,
							Text: ref,
						})
						break
					}
				}
			}

			req := ImplementedRequirement{
				UUID:        uuid5(nsURL, projectURL+"/control/"+ctrl.ID),
				ControlID:   ctrl.ID,
				Description: strings.TrimSpace(ctrl.Description),
				Props:       props,
			}
			if len(links) > 0 {
				req.Links = links
			}
			reqs = append(reqs, req)
		}
	}

	doc := ComponentDefinitionDoc{
		ComponentDefinition: ComponentDefinition{
			UUID: uuid5(nsURL, projectURL+"/component-definition"),
			Metadata: Metadata{
				Title:        cfg.Project.Name + " Component Definition",
				LastModified: time.Now().UTC().Format(time.RFC3339),
				Version:      "1.0",
				OSCALVersion: "1.1.2",
				Roles: []Role{
					{ID: "platform-provider", Title: "Siros Foundation (Platform Provider)"},
					{ID: "deployment-operator", Title: "Deployment Operator"},
				},
			},
			Components: []Component{
				{
					UUID:        uuid5(nsURL, projectURL+"/component/platform"),
					Type:        "software",
					Title:       cfg.Project.Name,
					Description: "Security controls for " + cfg.Project.Name,
					ControlImplementations: []ControlImplementation{
						{
							UUID:                    uuid5(nsURL, projectURL+"/control-implementation/platform"),
							Source:                  projectURL + "/controls",
							Description:             cfg.Project.Name + " security controls",
							ImplementedRequirements: reqs,
						},
					},
				},
			},
		},
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling OSCAL: %w", err)
	}

	outPath := filepath.Join(cfg.OSCALDir, "component-definition.json")
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}
	if err := os.WriteFile(outPath, append(out, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", outPath, err)
	}

	fmt.Printf("OSCAL component-definition written to %s (%d controls)\n", outPath, len(reqs))
	return nil
}

func statusOrDefault(s string) string {
	if s == "" {
		return "to_do"
	}
	return s
}

func groupSlug(title string) string {
	s := strings.ToLower(title)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "&", "and")
	return s
}
