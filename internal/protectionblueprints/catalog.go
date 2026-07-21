// Package protectionblueprints provides declarative, versioned application
// knowledge that enriches evidence collected by the project analyzer.
// Templates never execute commands and never contain credential values.
package protectionblueprints

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed catalog/*.yaml
var catalogFS embed.FS

// CatalogSchemaVersion is the newest template schema this binary accepts.
const CatalogSchemaVersion = 1

// Catalog is an immutable collection of validated built-in templates.
type Catalog struct {
	Templates []Template
}

// Template describes how to recognise and protect one common application
// topology. Commands are intentionally absent from this schema: executable
// hooks require a separate trust and approval design.
type Template struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Match      Match    `yaml:"match" json:"match"`
	Plan       Plan     `yaml:"plan" json:"plan"`
}

// Metadata identifies a template and its compatibility lifecycle.
type Metadata struct {
	ID       string   `yaml:"id" json:"id"`
	Name     string   `yaml:"name" json:"name"`
	Version  string   `yaml:"version" json:"version"`
	Category string   `yaml:"category" json:"category"`
	Upstream string   `yaml:"upstream" json:"upstream"`
	Tags     []string `yaml:"tags" json:"tags"`
}

// Match contains evidence rules. Every RequiredImageGroups entry represents
// one service role and must match at least one image pattern in that group.
//
// Optional images are grouped for the same reason as required ones: "redis or
// valkey" is one cache component with two implementations, not two components
// of which one is always missing. A flat list would make a complete project
// look incomplete.
type Match struct {
	RequiredImageGroups  [][]string `yaml:"requiredImageGroups" json:"requiredImageGroups"`
	OptionalImageGroups  [][]string `yaml:"optionalImageGroups" json:"optionalImageGroups"`
	RequiredTechnologies []string   `yaml:"requiredTechnologies" json:"requiredTechnologies"`
	OptionalTechnologies []string   `yaml:"optionalTechnologies" json:"optionalTechnologies"`
}

// Plan is advisory protection knowledge used to generate a user-reviewable
// plan from the actual project evidence.
type Plan struct {
	Classification   string   `yaml:"classification" json:"classification"`
	Consistency      string   `yaml:"consistency" json:"consistency"`
	RequiredData     []string `yaml:"requiredData" json:"requiredData"`
	DatabaseStrategy []string `yaml:"databaseStrategy" json:"databaseStrategy"`
	IncludeHints     []string `yaml:"includeHints" json:"includeHints"`
	ExcludeHints     []string `yaml:"excludeHints" json:"excludeHints"`
	Warnings         []string `yaml:"warnings" json:"warnings"`
	RestoreChecks    []string `yaml:"restoreChecks" json:"restoreChecks"`
}

type catalogDocument struct {
	SchemaVersion int        `yaml:"schemaVersion"`
	Templates     []Template `yaml:"templates"`
}

// LoadBuiltin loads and validates the templates embedded in the Back-Orbit
// binary. Unknown YAML fields are rejected to catch misspelled safety rules.
func LoadBuiltin() (Catalog, error) {
	entries, err := fs.Glob(catalogFS, "catalog/*.yaml")
	if err != nil {
		return Catalog{}, fmt.Errorf("protection blueprints: list catalog: %w", err)
	}
	var all []Template
	seen := map[string]bool{}
	for _, name := range entries {
		file, err := catalogFS.Open(name)
		if err != nil {
			return Catalog{}, fmt.Errorf("protection blueprints: open %s: %w", name, err)
		}
		decoder := yaml.NewDecoder(file)
		decoder.KnownFields(true)
		var document catalogDocument
		decodeErr := decoder.Decode(&document)
		closeErr := file.Close()
		if decodeErr != nil {
			return Catalog{}, fmt.Errorf("protection blueprints: decode %s: %w", name, decodeErr)
		}
		if closeErr != nil {
			return Catalog{}, fmt.Errorf("protection blueprints: close %s: %w", name, closeErr)
		}
		if document.SchemaVersion != CatalogSchemaVersion {
			return Catalog{}, fmt.Errorf("protection blueprints: %s uses unsupported schema version %d", name, document.SchemaVersion)
		}
		for _, template := range document.Templates {
			if err := validate(template); err != nil {
				return Catalog{}, fmt.Errorf("protection blueprints: %s: %w", name, err)
			}
			if seen[template.Metadata.ID] {
				return Catalog{}, fmt.Errorf("protection blueprints: duplicate template id %q", template.Metadata.ID)
			}
			seen[template.Metadata.ID] = true
			all = append(all, template)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Metadata.ID < all[j].Metadata.ID })
	return Catalog{Templates: all}, nil
}

func validate(template Template) error {
	if template.APIVersion != "back-orbit.io/v1alpha1" || template.Kind != "ProtectionTemplate" {
		return fmt.Errorf("template %q has unsupported apiVersion or kind", template.Metadata.ID)
	}
	if !validID(template.Metadata.ID) || strings.TrimSpace(template.Metadata.Name) == "" || strings.TrimSpace(template.Metadata.Version) == "" {
		return fmt.Errorf("template has invalid required metadata")
	}
	if len(template.Match.RequiredImageGroups) == 0 {
		return fmt.Errorf("template %q has no required image evidence", template.Metadata.ID)
	}
	groups := append(append([][]string{}, template.Match.RequiredImageGroups...),
		template.Match.OptionalImageGroups...)
	for _, group := range groups {
		if len(group) == 0 {
			return fmt.Errorf("template %q has an empty image group", template.Metadata.ID)
		}
		for _, pattern := range group {
			if strings.TrimSpace(pattern) == "" || strings.ContainsAny(pattern, " \t\r\n") {
				return fmt.Errorf("template %q has an invalid image pattern", template.Metadata.ID)
			}
			// Patterns are compared against a repository path, so a tag or a
			// digest in one can never match and would silently disable the
			// rule it belongs to.
			if strings.ContainsAny(lastSegment(pattern), ":@") {
				return fmt.Errorf("template %q pattern %q carries a tag or digest; use the repository path alone",
					template.Metadata.ID, pattern)
			}
		}
	}
	return nil
}

func lastSegment(pattern string) string {
	if slash := strings.LastIndex(pattern, "/"); slash >= 0 {
		return pattern[slash+1:]
	}
	return pattern
}

func validID(value string) bool {
	if value == "" || len(value) > 80 {
		return false
	}
	for _, r := range value {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}
