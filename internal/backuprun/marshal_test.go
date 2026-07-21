package backuprun

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReplayIsSerialised(t *testing.T) {
	raw, err := json.Marshal(DatabaseDump{Technology: "postgresql", Service: "db",
		User: "app", Level: ProtectionExported, Path: "d.sql"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"replay":"docker compose exec`) {
		t.Fatalf("the replay command must reach the API: %s", raw)
	}
	if !strings.Contains(string(raw), `"level":"exported"`) {
		t.Fatalf("the level must reach the API: %s", raw)
	}
}

// TestEmptyListsStayLists is a crash this actually caused. An empty Docker
// volume produced no ownership entries, Go marshalled the nil slice as null,
// and the snapshot details called .length on it — which unmounts the React
// tree and blanks the whole page.
//
// An API that returns null where it usually returns an array makes every
// consumer defend against both, and one of them will forget.
func TestEmptyListsStayLists(t *testing.T) {
	raw, err := json.Marshal(Manifest{SchemaVersion: 1, Project: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"volumes":[]`, `"databases":[]`} {
		if !strings.Contains(string(raw), field) {
			t.Errorf("expected %s in %s", field, raw)
		}
	}
	if strings.Contains(string(raw), "null") {
		t.Errorf("a manifest must not serialise nulls: %s", raw)
	}

	volume, err := json.Marshal(VolumeManifest{Name: "empty-volume"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(volume), `"ownership":[]`) {
		t.Errorf("an empty volume must still carry an ownership list: %s", volume)
	}
	if strings.Contains(string(volume), "null") {
		t.Errorf("a volume manifest must not serialise nulls: %s", volume)
	}
}
