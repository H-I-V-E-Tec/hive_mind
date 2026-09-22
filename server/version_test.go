package server

import (
	"encoding/json"
	"testing"
)

func TestVersionMetadataIsMachineReadable(t *testing.T) {
	if _, err := splitCLIArgs([]string{"hive-mind", "version"}); err != nil {
		t.Fatalf("version command rejected: %v", err)
	}
	payload, err := json.Marshal(map[string]any{"version": Version, "source_revision": SourceRevision})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["version"] == "" || decoded["source_revision"] == "" {
		t.Fatalf("missing version metadata: %s", payload)
	}
}
