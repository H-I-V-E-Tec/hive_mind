package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSpec010InstallsMaintainedSkillTemplates checks that the Claude Code and
// Codex templates land at the documented paths and carry every element the
// agent-integration spec requires.
func TestSpec010InstallsMaintainedSkillTemplates(t *testing.T) {
	required := []string{
		"untrusted_content",                // 1. nature: retrieved text is data
		"never grants permission",          // 1. scope authority comes from the server
		"`hive_get_context`",               // 2. tools table
		"`hive_search`",                    // 2.
		"`get_sync_status`",                // 2.
		"`ingest_workspace`",               // 2.
		"Before acting on an asset",        // 3. workflow
		"discarded hypotheses",             // 3. negative results
		"program_id:",                      // 4. front matter
		"document_type:",                   // 4.
		"collected_at:",                    // 4.
		"asset_refs:",                      // 4.
		"team-a",                           // 5. authorship convention
		"handle",                           // 5.
		"claimed_scope_status: authorized", // 6. prohibitions
		"credentials, tokens",              // 6.
		"scope.json",                       // 6.
		"handoff",                          // 7. handoff session
	}
	cases := map[string]string{"claude": ".claude/skills/hive-mind/SKILL.md", "codex": ".codex/mcp-instructions.md"}
	for key, rel := range cases {
		dest := t.TempDir()
		if err := InstallSkill(key, dest); err != nil {
			t.Fatalf("install %s: %v", key, err)
		}
		body, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("%s template was not written to %s: %v", key, rel, err)
		}
		text := string(body)
		for _, marker := range required {
			if !strings.Contains(text, marker) {
				t.Errorf("%s template lacks required element %q", key, marker)
			}
		}
		if strings.Contains(text, "codebase") || strings.Contains(strings.ToLower(text), "middleware") {
			t.Errorf("%s template still describes the legacy code RAG server", key)
		}
	}
	if strings.Contains(string(mustSkill(t, "claude")), "AGENTS.md") || !strings.HasPrefix(string(mustSkill(t, "claude")), "---\nname: hive-mind\n") {
		t.Fatal("claude template must be a Claude Code skill with front matter")
	}
	if err := InstallSkill("unknown-agent", t.TempDir()); err == nil {
		t.Fatal("unknown agent accepted")
	}
}

func mustSkill(t *testing.T, key string) []byte {
	t.Helper()
	for _, skill := range AvailableSkills {
		if skill.Key == key {
			data, err := skillsFS.ReadFile(skill.EmbedPath)
			if err != nil {
				t.Fatal(err)
			}
			return data
		}
	}
	t.Fatalf("skill %s not registered", key)
	return nil
}
