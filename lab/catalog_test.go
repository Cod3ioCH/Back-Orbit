package lab_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Cod3ioCH/Back-Orbit/internal/protectionblueprints"
	"gopkg.in/yaml.v3"
)

type labCatalog struct {
	SchemaVersion int          `yaml:"schemaVersion"`
	Projects      []labProject `yaml:"projects"`
}

type labProject struct {
	ID        string `yaml:"id"`
	Blueprint string `yaml:"blueprint"`
	Category  string `yaml:"category"`
	Profile   string `yaml:"profile"`
	Status    string `yaml:"status"`
}

func TestCatalogReferencesValidBlueprintsAndTemplates(t *testing.T) {
	contents, err := os.ReadFile("catalog.yaml")
	if err != nil {
		t.Fatalf("read lab catalog: %v", err)
	}
	var catalog labCatalog
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	decoder.KnownFields(true)
	if err := decoder.Decode(&catalog); err != nil {
		t.Fatalf("decode lab catalog: %v", err)
	}
	if catalog.SchemaVersion != 1 {
		t.Fatalf("unexpected schema version %d", catalog.SchemaVersion)
	}
	if len(catalog.Projects) != 25 {
		t.Fatalf("expected 25 lab projects, got %d", len(catalog.Projects))
	}

	builtin, err := protectionblueprints.LoadBuiltin()
	if err != nil {
		t.Fatalf("load protection blueprints: %v", err)
	}
	blueprints := make(map[string]bool, len(builtin.Templates))
	for _, template := range builtin.Templates {
		blueprints[template.Metadata.ID] = true
	}

	seen := make(map[string]bool, len(catalog.Projects))
	ready := make(map[string]bool)
	for _, project := range catalog.Projects {
		if seen[project.ID] {
			t.Errorf("duplicate project id %q", project.ID)
		}
		seen[project.ID] = true
		if !blueprints[project.Blueprint] {
			t.Errorf("project %q references unknown blueprint %q", project.ID, project.Blueprint)
		}
		switch project.Status {
		case "ready":
			ready[project.ID] = true
			if _, err := os.Stat(filepath.Join("templates", project.ID, "compose.yml")); err != nil {
				t.Errorf("ready project %q has no compose template: %v", project.ID, err)
			}
		case "planned":
		default:
			t.Errorf("project %q has invalid status %q", project.ID, project.Status)
		}
	}

	entries, err := os.ReadDir("templates")
	if err != nil {
		t.Fatalf("read templates: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() && !ready[entry.Name()] {
			t.Errorf("template directory %q is not marked ready", entry.Name())
		}
	}

	err = filepath.WalkDir("templates", func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if entry.Name() == ".env" || strings.Contains(path, string(filepath.Separator)+"secrets"+string(filepath.Separator)) {
			t.Errorf("template contains generated credential material at %q", path)
		}
		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(contents), ":latest") || strings.Contains(string(contents), ":-release}") {
			t.Errorf("template %q uses a moving image tag", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("validate template files: %v", err)
	}
}
