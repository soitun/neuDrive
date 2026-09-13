package services

import (
	"fmt"
	"strings"
	"testing"

	"github.com/agi-bar/neudrive/internal/models"
	"github.com/google/uuid"
)

func TestIndexedSearchPostgres(t *testing.T) {
	ctx, owner, tree, _, _, _ := setupBundleIntegration(t)
	path := "/conversations/pathneedle/conversation.md"
	write := func(content string) {
		t.Helper()
		if _, err := tree.WriteEntry(ctx, owner, path, content, "text/plain", models.FileTreeWriteOptions{MinTrustLevel: 2, Metadata: map[string]interface{}{"source_platform": "metaneedle"}}); err != nil {
			t.Fatal(err)
		}
	}
	check := func(query string, want int) {
		t.Helper()
		hits, err := tree.Search(ctx, owner, query, 2, "/")
		if err != nil || len(hits) != want {
			t.Fatalf("query %q: %+v %v want=%d", query, hits, err, want)
		}
	}
	write("alpha other beta 智能搜索系统 C++ foo_bar 100%")
	for _, query := range []string{"alpha beta", "ALPHA", "搜索系统", "搜索", "C++", "foo_bar", "%", "_", "pathneedle", "metaneedle"} {
		check(query, 1)
	}
	for _, query := range []string{"notfound", "   ", `" OR *`} {
		check(query, 0)
	}
	for _, tc := range []struct {
		user   uuid.UUID
		trust  int
		prefix string
	}{{uuid.New(), 4, "/"}, {owner, 1, "/"}, {owner, 2, "/platforms"}} {
		hits, err := tree.Search(ctx, tc.user, "alpha", tc.trust, tc.prefix)
		if err != nil || len(hits) != 0 {
			t.Fatalf("isolation: %+v %v", hits, err)
		}
	}
	write("replacementneedle")
	check("alpha", 0)
	check("replacementneedle", 1)
	if err := tree.Delete(ctx, owner, path); err != nil {
		t.Fatal(err)
	}
	check("replacementneedle", 0)
	write("restoreneedle")
	check("restoreneedle", 1)
	if _, err := tree.db.Exec(ctx, `INSERT INTO file_tree (user_id, path, content)
	 SELECT $1, '/search-plan-fixture/' || n || '.md', repeat('ordinary unrelated document body ', 20)
	 FROM generate_series(1, 2000) AS n`, owner); err != nil {
		t.Fatal(err)
	}
	// Flush GIN's pending list after the bulk fixture insert, as autovacuum does.
	if _, err := tree.db.Exec(ctx, "VACUUM ANALYZE file_tree"); err != nil {
		t.Fatal(err)
	}

	// Small fixtures favor a sequential scan. Disable it only here to verify that
	// the exact search expressions can use both indexes, including the OR branch.
	tx, err := tree.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SET LOCAL enable_seqscan = off"); err != nil {
		t.Fatal(err)
	}
	sql := fmt.Sprintf(`EXPLAIN SELECT id FROM file_tree WHERE deleted_at IS NULL AND is_directory = false
  AND (to_tsvector('simple', %s) @@ plainto_tsquery('simple', $1) OR %s ILIKE $2)`, fileTreeSearchTextExpr, fileTreeSearchTextExpr)
	rows, err := tx.Query(ctx, sql, "needle", "%needle%")
	if err != nil {
		t.Fatal(err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(line)
		plan.WriteByte('\n')
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []string{"idx_file_tree_content_search", "idx_file_tree_substring_search"} {
		if !strings.Contains(plan.String(), index) {
			t.Errorf("missing %s from plan:\n%s", index, plan.String())
		}
	}
}
