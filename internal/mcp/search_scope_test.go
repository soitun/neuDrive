package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/agi-bar/neudrive/internal/models"
	"github.com/agi-bar/neudrive/internal/services"
	sqlitestorage "github.com/agi-bar/neudrive/internal/storage/sqlite"
)

func TestSearchMemoryCoversImportedRoots(t *testing.T) {
	store, err := sqlitestorage.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	user, err := store.EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"/conversations/demo/conversation.md", "/platforms/demo/archive.json", "/custom/CaseSensitive/note.md", "/roles/hidden.md", "/inbox/hidden.md", "/custom/private.md"}
	for _, path := range paths {
		trust := models.TrustLevelWork
		if path == "/custom/private.md" {
			trust = models.TrustLevelFull
		}
		if _, err := store.WriteEntry(ctx, user.ID, path, "scopecoverageprobe", "text/plain", models.FileTreeWriteOptions{MinTrustLevel: trust}); err != nil {
			t.Fatal(err)
		}
	}
	s := &MCPServer{UserID: user.ID, TrustLevel: models.TrustLevelWork, Scopes: []string{models.ScopeReadMemory}, FileTree: services.NewFileTreeServiceWithRepo(sqlitestorage.NewFileTreeRepo(store))}
	for _, tc := range []struct {
		scope string
		want  []string
	}{{"", paths[:3]}, {"all", paths[:3]}, {"conversations", paths[:1]}, {"platforms", paths[1:2]}, {"/custom/CaseSensitive", paths[2:3]}} {
		t.Run(tc.scope, func(t *testing.T) {
			text, failed := mcpToolCall(t, s, "search_memory", map[string]interface{}{"query": "scopecoverageprobe", "scope": tc.scope})
			if failed {
				t.Fatal(text)
			}
			var hits []struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal([]byte(text), &hits); err != nil {
				t.Fatal(err, text)
			}
			if len(hits) != len(tc.want) {
				t.Fatalf("got %s, want %v", text, tc.want)
			}
			found := map[string]bool{}
			for _, hit := range hits {
				found[hit.Path] = true
			}
			for _, path := range tc.want {
				if !found[path] {
					t.Errorf("missing %s", path)
				}
			}
		})
	}
}
