package protectionblueprints

import (
	"reflect"
	"testing"
)

// TestARoleIsNotStolenByASimilarlyNamedImage.
//
// "mongo-express" contains the string "mongo". Under substring matching the
// admin UI filled the database role, the database role then had nothing left,
// and the template did not match at all — but only when the images happened to
// arrive in that order.
func TestARoleIsNotStolenByASimilarlyNamedImage(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}

	for _, order := range [][]string{
		{"mongo:7", "mongo-express:1.0.2"},
		{"mongo-express:1.0.2", "mongo:7"},
	} {
		matches := catalog.Match(Evidence{Images: order, Technologies: []string{"mongodb"}})
		if findMatch(matches, "mongodb-express") == nil {
			t.Fatalf("images %v did not match the MongoDB template", order)
		}
	}
}

// TestMatchingDoesNotDependOnImageOrder over the whole catalog: the order
// containers come back from Docker is not something the matcher gets to
// choose, so it must not change the answer.
func TestMatchingDoesNotDependOnImageOrder(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}

	forward := []string{
		"nextcloud:31-apache", "postgres:17-alpine", "redis:7-alpine",
		"mongo:7", "mongo-express:1.0.2", "dpage/pgadmin4:8",
	}
	backward := make([]string, len(forward))
	for i, image := range forward {
		backward[len(forward)-1-i] = image
	}
	technologies := []string{"postgresql", "mongodb", "redis"}

	ids := func(images []string) []string {
		out := []string{}
		for _, match := range catalog.Match(Evidence{Images: images, Technologies: technologies}) {
			out = append(out, match.TemplateID)
		}
		return out
	}
	if a, b := ids(forward), ids(backward); !reflect.DeepEqual(a, b) {
		t.Fatalf("reversing the image order changed the matches:\n forward: %v\nbackward: %v", a, b)
	}
}

// TestOneImageCannotFillTwoRoles. A project with a single PostgreSQL container
// does not have the topology a two-database template describes.
func TestOneImageCannotFillTwoRoles(t *testing.T) {
	template := Template{
		APIVersion: "back-orbit.io/v1alpha1", Kind: "ProtectionTemplate",
		Metadata: Metadata{ID: "two-databases", Name: "Two", Version: "1.0.0"},
		Match:    Match{RequiredImageGroups: [][]string{{"postgres"}, {"postgres"}}},
	}
	catalog := Catalog{Templates: []Template{template}}

	if len(catalog.Match(Evidence{Images: []string{"postgres:17"}})) != 0 {
		t.Fatal("one image satisfied two required roles")
	}
	if len(catalog.Match(Evidence{Images: []string{"postgres:17", "postgres:16"}})) != 1 {
		t.Fatal("two images did not satisfy two required roles")
	}
}

// TestAnEarlyRoleDoesNotStrandALaterOne. The first role can match either
// image; taking the wrong one leaves the second role with nothing, though a
// valid assignment exists.
func TestAnEarlyRoleDoesNotStrandALaterOne(t *testing.T) {
	template := Template{
		APIVersion: "back-orbit.io/v1alpha1", Kind: "ProtectionTemplate",
		Metadata: Metadata{ID: "greedy-trap", Name: "Trap", Version: "1.0.0"},
		Match:    Match{RequiredImageGroups: [][]string{{"postgres", "redis"}, {"postgres"}}},
	}
	catalog := Catalog{Templates: []Template{template}}

	if len(catalog.Match(Evidence{Images: []string{"postgres:17", "redis:7"}})) != 1 {
		t.Fatal("a valid assignment was not found")
	}
}

// TestAFullyPresentTopologyScoresOneHundred. The number is shown to the
// operator as a match percentage, so a project that has everything the
// template describes must not be told it is a half match.
func TestAFullyPresentTopologyScoresOneHundred(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}

	full := findMatch(catalog.Match(Evidence{
		Images:       []string{"nextcloud:31-apache", "postgres:17-alpine", "redis:7-alpine"},
		Technologies: []string{"postgresql", "redis"},
	}), "nextcloud-postgresql")
	if full == nil || full.Score != 100 {
		t.Fatalf("a complete Nextcloud scored %v, not 100", full)
	}

	// Valkey is the other implementation of the same component, not a second
	// missing one.
	valkey := findMatch(catalog.Match(Evidence{
		Images:       []string{"nextcloud:31-apache", "postgres:17-alpine", "valkey/valkey:8"},
		Technologies: []string{"postgresql", "valkey"},
	}), "nextcloud-postgresql")
	if valkey == nil || valkey.Score != 100 {
		t.Fatalf("a Nextcloud on Valkey scored %v, not 100", valkey)
	}

	// Without the cache it is still a match, and visibly less than complete.
	bare := findMatch(catalog.Match(Evidence{
		Images:       []string{"nextcloud:31-apache", "postgres:17-alpine"},
		Technologies: []string{"postgresql"},
	}), "nextcloud-postgresql")
	if bare == nil || bare.Score >= 100 {
		t.Fatalf("a Nextcloud without its cache scored %v", bare)
	}
}

// TestRegistryAndTagAreNotEvidence: a pattern must describe the image, not
// happen to appear somewhere in its reference.
func TestRegistryAndTagAreNotEvidence(t *testing.T) {
	cases := map[string]struct {
		path, pattern string
		want          bool
	}{
		"plain image":                {"postgres", "postgres", true},
		"official namespace":         {"library/postgres", "postgres", true},
		"vendor registry":            {"immich-app/postgres", "postgres", true},
		"different product":          {"mongo-express", "mongo", false},
		"registry host lookalike":    {"anything", "postgres", false},
		"full path pattern":          {"vaultwarden/server", "vaultwarden/server", true},
		"path prefix is not a match": {"vaultwarden/server", "vaultwarden", false},
	}
	for name, test := range cases {
		if got := matchesPattern(test.path, test.pattern); got != test.want {
			t.Errorf("%s: %q against %q = %v, want %v", name, test.path, test.pattern, got, test.want)
		}
	}
}

func TestRepositoryPathStripsHostTagAndDigest(t *testing.T) {
	cases := map[string]string{
		"postgres:17-alpine":                    "postgres",
		"ghcr.io/immich-app/immich-server:v2.7": "immich-app/immich-server",
		"docker.gitea.com/gitea:1.26.2":         "gitea",
		"codeberg.org/forgejo/forgejo:16":       "forgejo/forgejo",
		"localhost:5000/private/app:1":          "private/app",
		"valkey/valkey:8-bookworm":              "valkey/valkey",
		"mariadb@sha256:abc":                    "mariadb",
		"":                                      "",
	}
	for image, want := range cases {
		if got := repositoryPath(image); got != want {
			t.Errorf("repositoryPath(%q) = %q, want %q", image, got, want)
		}
	}
}

// TestEveryCatalogPatternMatchesARealImage. A pattern that matches nothing is
// a template that can never fire, and nothing in the code would ever say so.
func TestEveryCatalogPatternMatchesARealImage(t *testing.T) {
	// The images the lab scenarios actually run, plus the published names of
	// the remaining catalogued applications.
	real := repositoryPaths([]string{
		"wordpress:6-apache", "mariadb:11", "mysql:8",
		"nextcloud:31-apache", "postgres:17-alpine", "redis:7-alpine",
		"valkey/valkey:8-bookworm",
		"ghcr.io/paperless-ngx/paperless-ngx:2.20.15",
		"ghcr.io/immich-app/immich-server:v2.7.5",
		"ghcr.io/immich-app/immich-machine-learning:v2.7.5",
		"vaultwarden/server:1.36.0",
		"docker.gitea.com/gitea:1.26.2", "codeberg.org/forgejo/forgejo:16",
		"louislam/uptime-kuma:2", "n8nio/n8n:1", "nodered/node-red:4",
		"ghcr.io/home-assistant/home-assistant:2026.7",
		"jellyfin/jellyfin:10", "deluan/navidrome:0.53",
		"advplyr/audiobookshelf:2", "ghost:5",
		"lscr.io/linuxserver/bookstack:24", "vikunja/vikunja:0.24",
		"grafana/grafana:11", "prom/prometheus:v2", "grafana/loki:3",
		"gotify/server:2", "binwiederhier/ntfy:2",
		"minio/minio:latest", "dpage/pgadmin4:8",
		"mongo:7", "mongo-express:1.0.2",
	})

	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}
	for _, template := range catalog.Templates {
		groups := append(append([][]string{}, template.Match.RequiredImageGroups...),
			template.Match.OptionalImageGroups...)
		for _, group := range groups {
			for _, pattern := range group {
				matched := false
				for _, path := range real {
					if matchesPattern(path, pattern) {
						matched = true
						break
					}
				}
				if !matched {
					t.Errorf("template %q: pattern %q matches none of the real images it is meant to recognise",
						template.Metadata.ID, pattern)
				}
			}
		}
	}
}

// TestCatalogPatternsCarryNoTagOrDigest, which would silently never match.
func TestCatalogRejectsATaggedPattern(t *testing.T) {
	err := validate(Template{
		APIVersion: "back-orbit.io/v1alpha1", Kind: "ProtectionTemplate",
		Metadata: Metadata{ID: "tagged", Name: "Tagged", Version: "1.0.0"},
		Match:    Match{RequiredImageGroups: [][]string{{"postgres:17"}}},
	})
	if err == nil {
		t.Fatal("a pattern carrying a tag was accepted")
	}
}

// TestTheTemplateThatExplainsMoreRanksFirst.
//
// The UI presents the first result as "the" blueprint for the project. A
// single-service infrastructure template reaches 100 as easily as the one
// describing the actual application, so score alone would let an incidental
// cache container name the project — and "valkey-worker" sorts before
// "wordpress-mariadb", so the alphabet does not save it either.
func TestTheTemplateThatExplainsMoreRanksFirst(t *testing.T) {
	catalog, err := LoadBuiltin()
	if err != nil {
		t.Fatal(err)
	}

	matches := catalog.Match(Evidence{
		Images:       []string{"wordpress:6-apache", "mariadb:11", "valkey/valkey:8-bookworm"},
		Technologies: []string{"mariadb", "valkey"},
	})
	ids := []string{}
	for _, match := range matches {
		ids = append(ids, match.TemplateID)
	}
	if len(matches) < 2 {
		t.Fatalf("expected the application and the cache to both match, got %v", ids)
	}
	if matches[0].TemplateID != "wordpress-mariadb" {
		t.Fatalf("the cache outranked the application: %v", ids)
	}
}
