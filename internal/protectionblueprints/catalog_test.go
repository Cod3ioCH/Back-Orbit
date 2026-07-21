package protectionblueprints

import "testing"

func TestBuiltinCatalogContainsFirstWave(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Templates) != 25 {
		t.Fatalf("template count = %d, want 25", len(catalog.Templates))
	}
	for _, template := range catalog.Templates {
		if template.Metadata.Version == "" || len(template.Plan.RequiredData) == 0 || len(template.Plan.RestoreChecks) == 0 {
			t.Errorf("template %q is missing operational protection information", template.Metadata.ID)
		}
	}
}

func TestMatchRequiresAllRequiredEvidence(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name         string
		evidence     Evidence
		wantTemplate string
	}{
		{name: "complete WordPress topology", evidence: Evidence{Images: []string{"wordpress:6", "mariadb:11"}, Technologies: []string{"mariadb"}}, wantTemplate: "wordpress-mariadb"},
		{name: "database image alone is insufficient", evidence: Evidence{Images: []string{"mariadb:11"}, Technologies: []string{"mariadb"}}},
		{name: "application without expected database is insufficient", evidence: Evidence{Images: []string{"wordpress:6"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matches := catalog.Match(test.evidence)
			found := false
			for _, match := range matches {
				if match.TemplateID == "wordpress-mariadb" {
					found = true
					if test.wantTemplate != "" && (match.Score != 100 || len(match.Matched) != 3) {
						t.Fatalf("unexpected match: %#v", match)
					}
				}
			}
			if found != (test.wantTemplate != "") {
				t.Fatalf("WordPress match present = %v, want %v; all matches: %#v", found, test.wantTemplate != "", matches)
			}
		})
	}
}

func TestOptionalEvidenceChangesScoreButNotEligibility(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	withoutRedis := findMatch(catalog.Match(Evidence{Images: []string{"nextcloud:31", "postgres:17"}, Technologies: []string{"postgresql"}}), "nextcloud-postgresql")
	withRedis := findMatch(catalog.Match(Evidence{Images: []string{"nextcloud:31", "postgres:17", "redis:7"}, Technologies: []string{"postgresql", "redis"}}), "nextcloud-postgresql")
	if withoutRedis == nil || withRedis == nil {
		t.Fatal("Nextcloud should remain eligible with or without optional Redis")
	}
	if withRedis.Score <= withoutRedis.Score {
		t.Fatalf("optional evidence did not improve score: without=%d with=%d", withoutRedis.Score, withRedis.Score)
	}
}

func findMatch(matches []Result, id string) *Result {
	for i := range matches {
		if matches[i].TemplateID == id {
			return &matches[i]
		}
	}
	return nil
}
