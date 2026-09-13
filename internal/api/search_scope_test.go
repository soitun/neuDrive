package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/agi-bar/neudrive/internal/models"
)

func TestSearchCoversAllVisibleRoots(t *testing.T) {
	ts, store, token, _, _ := newTestHTTPServer(t)
	ctx := context.Background()
	user, err := store.EnsureOwner(ctx)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"/memory/search.md", "/projects/demo/context.md", "/skills/demo/SKILL.md", "/platforms/demo/archive.json", "/conversations/demo/conversation.md", "/identity/import.json", "/custom/CaseSensitive/note.md"}
	for _, path := range append(append([]string{}, paths...), "/roles/hidden.md", "/inbox/hidden.md") {
		if _, err := store.WriteEntry(ctx, user.ID, path, "scopecoverageprobe", "text/plain", models.FileTreeWriteOptions{MinTrustLevel: models.TrustLevelWork}); err != nil {
			t.Fatal(err)
		}
	}
	for _, endpoint := range []string{"/api/search?q=scopecoverageprobe", "/agent/search?q=scopecoverageprobe", "/agent/search?q=scopecoverageprobe&scope=all", "/agent/search?q=scopecoverageprobe&scope=/custom/CaseSensitive"} {
		t.Run(endpoint, func(t *testing.T) {
			status, env := doJSON(t, http.MethodGet, ts.URL+endpoint, token, nil)
			if status != http.StatusOK || !env.OK {
				t.Fatalf("search: %d %+v", status, env)
			}
			var data struct {
				Results []SearchHit `json:"results"`
			}
			if err := json.Unmarshal(env.Data, &data); err != nil {
				t.Fatal(err)
			}
			expected := paths
			if endpoint == "/agent/search?q=scopecoverageprobe&scope=/custom/CaseSensitive" {
				expected = paths[len(paths)-1:]
			}
			if len(data.Results) != len(expected) {
				t.Fatalf("got %+v, want %v", data.Results, expected)
			}
			found := map[string]bool{}
			for _, hit := range data.Results {
				found[hit.Path] = true
			}
			for _, path := range expected {
				if !found[path] {
					t.Errorf("missing %s", path)
				}
			}
		})
	}
}
