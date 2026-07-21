package projectanalyzer

import "testing"

func service(name, image string, mounts ...MountEvidence) ServiceEvidence {
	return ServiceEvidence{Name: name, Image: image, Mounts: mounts}
}

func mount(source, target string) MountEvidence {
	return MountEvidence{Type: "volume", Source: source, Target: target}
}

func detect(svc ServiceEvidence) []Finding {
	return databaseDetector{}.Detect(Input{Services: []ServiceEvidence{svc}})
}

func only(t *testing.T, findings []Finding) Finding {
	t.Helper()
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want exactly one: %+v", len(findings), findings)
	}
	return findings[0]
}

// TestConfidenceIsEarnedFromEvidence covers the rules that replaced counting
// weak hints. The previous scheme reached "confirmed" from a substring in an
// image name, which is the opposite of what confidence levels are for.
func TestConfidenceIsEarnedFromEvidence(t *testing.T) {
	t.Run("running the engine and storing its files is proof", func(t *testing.T) {
		finding := only(t, detect(service("db", "postgres:17", mount("pgdata", "/var/lib/postgresql/data"))))
		if finding.Confidence != ConfidenceConfirmed {
			t.Errorf("confidence = %q, want confirmed", finding.Confidence)
		}
		// The link a dump-aware backup needs: which storage holds this database.
		if finding.DataMount == nil || finding.DataMount.Source != "pgdata" {
			t.Errorf("the data mount must be recorded, got %+v", finding.DataMount)
		}
	})

	t.Run("a custom image is still caught by where it keeps data", func(t *testing.T) {
		finding := only(t, detect(service("core", "mycorp/private:3", mount("pgdata", "/var/lib/postgresql/data"))))
		if finding.Technology != "postgresql" || finding.Confidence != ConfidenceProbable {
			t.Errorf("got %s/%s, want postgresql/probable", finding.Technology, finding.Confidence)
		}
	})

	t.Run("an image name alone is only possible", func(t *testing.T) {
		finding := only(t, detect(service("metrics", "prometheuscommunity/postgres-exporter:v0.15")))
		if finding.Confidence != ConfidencePossible {
			t.Fatalf("confidence = %q, want possible — an exporter holds no data", finding.Confidence)
		}
		if len(finding.Warnings) == 0 {
			t.Error("a possible finding must say why it is uncertain")
		}
	})
}

// TestServiceNamesInventNothing: "db" says a database is likely, not which
// engine. Naming one from it is a guess dressed up as a finding.
func TestServiceNamesInventNothing(t *testing.T) {
	cases := map[string]ServiceEvidence{
		"an application called database-api": service("database-api", "mycorp/api:3"),
		"memcached called cache":             service("cache", "memcached:1.6"),
		"a service simply called db":         service("db", "mycorp/worker:1"),
	}

	for name, svc := range cases {
		t.Run(name, func(t *testing.T) {
			if findings := detect(svc); len(findings) != 0 {
				t.Fatalf("got %+v, want no database finding", findings)
			}
		})
	}
}

// TestSharedDataDirectoryDoesNotInventASecondEngine: MySQL and MariaDB both
// use /var/lib/mysql. Without the image taking precedence, one service reports
// two databases sitting on the same files — and a backup plan would dump the
// wrong one.
func TestSharedDataDirectoryDoesNotInventASecondEngine(t *testing.T) {
	for image, want := range map[string]string{"mysql:8": "mysql", "mariadb:11": "mariadb"} {
		t.Run(image, func(t *testing.T) {
			finding := only(t, detect(service("db", image, mount("mydata", "/var/lib/mysql"))))
			if finding.Technology != want {
				t.Errorf("technology = %q, want %q", finding.Technology, want)
			}
			if finding.Confidence != ConfidenceConfirmed {
				t.Errorf("confidence = %q, want confirmed", finding.Confidence)
			}
		})
	}
}

// TestMissingDataDirectoryIsCalledOut: a database whose files are nowhere in
// the declared mounts will not be captured by a storage backup at all, and
// that silence is exactly what leaves someone unprotected.
func TestMissingDataDirectoryIsCalledOut(t *testing.T) {
	finding := only(t, detect(ServiceEvidence{
		Name: "db", Image: "postgres:17",
		EnvironmentNames: []string{"POSTGRES_DB"},
	}))

	// Identification is certain — image plus engine-specific configuration.
	// What is missing is knowing where the data lives, which is a separate
	// concern and must be stated as one.
	if finding.Confidence != ConfidenceConfirmed {
		t.Fatalf("confidence = %q, want confirmed", finding.Confidence)
	}
	found := false
	for _, warning := range finding.Warnings {
		if len(warning) > 0 && finding.DataMount == nil {
			found = true
		}
	}
	if !found {
		t.Error("a database with no discoverable data directory must be flagged")
	}
}
