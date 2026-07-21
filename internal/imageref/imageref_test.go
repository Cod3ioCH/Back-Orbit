package imageref

import (
	"reflect"
	"testing"
)

// TestAPatternMustNameTheSoftwareNotAppearInTheReference. Every case here is
// one a substring search got wrong, in one direction or the other: it claimed
// neighbouring tools that merely carry an engine's name, and it could be
// satisfied by the registry host or the tag rather than the image itself.
func TestAPatternMustNameTheSoftwareNotAppearInTheReference(t *testing.T) {
	cases := map[string]struct {
		path, pattern string
		want          bool
	}{
		"plain image":                 {"postgres", "postgres", true},
		"official namespace":          {"library/postgres", "postgres", true},
		"vendor registry":             {"immich-app/postgres", "postgres", true},
		"admin ui for the engine":     {"mongo-express", "mongo", false},
		"exporter for the engine":     {"prometheuscommunity/postgres-exporter", "postgres", false},
		"unrelated image":             {"anything", "postgres", false},
		"full path pattern":           {"vaultwarden/server", "vaultwarden/server", true},
		"path prefix is not a match":  {"vaultwarden/server", "vaultwarden", false},
		"namespace is not the engine": {"timescale/timescaledb", "timescale", false},
		"empty pattern":               {"postgres", "", false},
		"empty path":                  {"", "postgres", false},
	}
	for name, test := range cases {
		if got := Matches(test.path, test.pattern); got != test.want {
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
		"  MongoDB/MongoDB-Community-Server:8 ": "mongodb/mongodb-community-server",
		"":                                      "",
	}
	for image, want := range cases {
		if got := RepositoryPath(image); got != want {
			t.Errorf("RepositoryPath(%q) = %q, want %q", image, got, want)
		}
	}
}

func TestRepositoryPathsDropsEmptyReferences(t *testing.T) {
	got := RepositoryPaths([]string{"postgres:17", "", "   ", "redis:7"})
	if want := []string{"postgres", "redis"}; !reflect.DeepEqual(got, want) {
		t.Errorf("RepositoryPaths = %v, want %v", got, want)
	}
}
