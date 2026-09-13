package sqlite_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/agi-bar/neudrive/internal/models"
	"github.com/agi-bar/neudrive/internal/storage/sqlite"
	"github.com/google/uuid"
)

func TestIndexedSearchMatchingAndIsolation(t *testing.T) {
	ctx, store, owner := openTestStore(t)
	fixtures := []struct {
		path, content string
		metadata      map[string]interface{}
	}{
		{"/conversations/demo/conversation.md", "alpha other beta 智能搜索系统 C++ foo_bar 100%", nil},
		{"/platforms/pathneedle/config.json", "unrelated", map[string]interface{}{"source_platform": "metaneedle"}},
		{"/custom/other.md", "alphabet", nil},
	}
	for _, f := range fixtures {
		if _, err := store.WriteEntry(ctx, owner, f.path, f.content, "text/plain", models.FileTreeWriteOptions{MinTrustLevel: 2, Metadata: f.metadata}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		query, prefix string
		want          int
	}{
		{"alpha beta", "/", 1}, {"ALPHA", "/", 2}, {"搜索系统", "/", 1}, {"搜索", "/", 1},
		{"C++", "/", 1}, {"foo_bar", "/", 1}, {"%", "/", 1}, {"_", "/", 2},
		{"pathneedle", "/", 1}, {"metaneedle", "/", 1}, {"alpha", "/platforms", 0},
		{`" OR *`, "/", 0}, {"   ", "/", 0},
	} {
		t.Run(tc.query+tc.prefix, func(t *testing.T) {
			hits, err := store.Search(ctx, owner, tc.query, 2, tc.prefix)
			if err != nil || len(hits) != tc.want {
				t.Fatalf("hits=%+v err=%v want=%d", hits, err, tc.want)
			}
		})
	}
	for _, query := range []string{"alpha", "搜索"} {
		for _, tc := range []struct {
			user  uuid.UUID
			trust int
		}{{owner, 1}, {uuid.New(), 4}} {
			hits, err := store.Search(ctx, tc.user, query, tc.trust, "/")
			if err != nil || len(hits) != 0 {
				t.Fatalf("isolation: %+v %v", hits, err)
			}
		}
	}
	hits, err := store.Search(ctx, owner, "alpha", 2, "/")
	if err != nil || len(hits) != 2 || hits[0].Path != fixtures[0].path {
		t.Fatalf("whole-word match should precede substring: %+v %v", hits, err)
	}
}

func TestSearchIndexLifecycleAndBackfill(t *testing.T) {
	ctx, store, owner := openTestStore(t)
	path := "/platforms/demo/record.md"
	write := func(content string) {
		t.Helper()
		if _, err := store.WriteEntry(ctx, owner, path, content, "text/plain", models.FileTreeWriteOptions{MinTrustLevel: 2}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(query string, want int) {
		t.Helper()
		hits, err := store.Search(ctx, owner, query, 2, "/")
		if err != nil || len(hits) != want {
			t.Fatalf("query %q: %+v %v, want %d", query, hits, err, want)
		}
	}
	write("oldneedle")
	check("oldneedle", 1)
	write("newneedle")
	check("oldneedle", 0)
	check("newneedle", 1)
	tx, err := store.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(ctx, `UPDATE file_tree SET content = 'rollbackneedle' WHERE user_id = ? AND content = 'newneedle'`, owner.String()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	check("rollbackneedle", 0)
	check("newneedle", 1)
	if err := store.Delete(ctx, owner, path); err != nil {
		t.Fatal(err)
	}
	check("newneedle", 0)
	write("restoreneedle")
	check("restoreneedle", 1)
	for _, index := range []string{"file_tree_fts", "file_tree_trigram"} {
		if _, err := store.DB().ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s(%s, rank) VALUES('integrity-check', 1)`, index, index)); err != nil {
			t.Fatal(err)
		}
	}
	// Remove only the new search schema to simulate upgrading an existing database.
	for _, index := range []string{"file_tree_fts", "file_tree_trigram"} {
		for _, suffix := range []string{"insert", "update", "delete"} {
			if _, err := store.DB().ExecContext(ctx, "DROP TRIGGER "+index+"_"+suffix); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := store.DB().ExecContext(ctx, "DROP TABLE "+index); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.DB().ExecContext(ctx, "DROP VIEW file_tree_search_content"); err != nil {
		t.Fatal(err)
	}
	dbPath := store.Path()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	check("restoreneedle", 1)
	if _, err := store.DB().ExecContext(ctx, `DELETE FROM file_tree WHERE content = 'restoreneedle'`); err != nil {
		t.Fatal(err)
	}
	check("restoreneedle", 0)
	// Reopening preserves the index instead of rebuilding it each time.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlite.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	write("reopenneedle")
	check("reopenneedle", 1)
}

func TestSearchUsesFTSIndexes(t *testing.T) {
	ctx, store, _ := openTestStore(t)
	for _, index := range []string{"file_tree_fts", "file_tree_trigram"} {
		rows, err := store.DB().QueryContext(ctx, "EXPLAIN QUERY PLAN SELECT rowid FROM "+index+" WHERE "+index+" MATCH ?", `"needle"`)
		if err != nil {
			t.Fatal(err)
		}
		var plan strings.Builder
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan.WriteString(detail)
		}
		err = rows.Err()
		rows.Close()
		if err != nil || !strings.Contains(plan.String(), "VIRTUAL TABLE INDEX") || !strings.Contains(plan.String(), "M") {
			t.Fatalf("index plan: %s, %v", plan.String(), err)
		}
	}
}
