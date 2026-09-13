package sqlite

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/agi-bar/neudrive/internal/hubpath"
	"github.com/agi-bar/neudrive/internal/models"
	"github.com/google/uuid"
)

// External-content indexes avoid storing another copy of imported document bodies.
// Triggers cover all writers (imports, sync, ordinary writes and SQL migrations).
func (s *Store) initSearch(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM sqlite_master WHERE name = 'file_tree_fts'`).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return tx.Commit()
	}
	statements := []string{
		`CREATE VIEW file_tree_search_content AS SELECT rowid, path, content, metadata_json FROM file_tree WHERE deleted_at IS NULL AND is_directory = 0`,
	}
	for _, index := range []struct{ name, tokenizer string }{{"file_tree_fts", "unicode61"}, {"file_tree_trigram", "trigram"}} {
		statements = append(statements,
			fmt.Sprintf(`CREATE VIRTUAL TABLE %s USING fts5(path, content, metadata_json, content='file_tree_search_content', content_rowid='rowid', tokenize='%s')`, index.name, index.tokenizer),
			fmt.Sprintf(`CREATE TRIGGER %s_insert AFTER INSERT ON file_tree WHEN new.deleted_at IS NULL AND new.is_directory = 0 BEGIN
    INSERT INTO %s(rowid, path, content, metadata_json) VALUES(new.rowid, new.path, new.content, new.metadata_json); END`, index.name, index.name),
			fmt.Sprintf(`CREATE TRIGGER %s_delete AFTER DELETE ON file_tree WHEN old.deleted_at IS NULL AND old.is_directory = 0 BEGIN
    INSERT INTO %s(%s, rowid, path, content, metadata_json) VALUES('delete', old.rowid, old.path, old.content, old.metadata_json); END`, index.name, index.name, index.name),
			fmt.Sprintf(`CREATE TRIGGER %s_update AFTER UPDATE OF path, content, metadata_json, deleted_at, is_directory ON file_tree BEGIN
    INSERT INTO %s(%s, rowid, path, content, metadata_json)
     SELECT 'delete', old.rowid, old.path, old.content, old.metadata_json WHERE old.deleted_at IS NULL AND old.is_directory = 0;
    INSERT INTO %s(rowid, path, content, metadata_json)
     SELECT new.rowid, new.path, new.content, new.metadata_json WHERE new.deleted_at IS NULL AND new.is_directory = 0; END`, index.name, index.name, index.name, index.name),
			fmt.Sprintf(`INSERT INTO %s(%s) VALUES('rebuild')`, index.name, index.name),
		)
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize search index: %w", err)
		}
	}
	return tx.Commit()
}

func quoteFTS(text string) string { return `"` + strings.ReplaceAll(text, `"`, `""`) + `"` }

func (s *Store) Search(ctx context.Context, userID uuid.UUID, query string, trustLevel int, rawPrefix string) ([]models.FileTreeEntry, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}
	prefix := hubpath.NormalizeStorage(rawPrefix)
	if prefix == "" {
		prefix = "/"
	}
	terms := strings.Fields(query)
	for i := range terms {
		terms[i] = quoteFTS(terms[i])
	}
	// Materialize BM25 before aggregating duplicate hits from the two indexes.
	candidates := `SELECT rowid, 1 AS tier, rank AS score FROM file_tree_trigram WHERE file_tree_trigram MATCH ?`
	args := []interface{}{strings.Join(terms, " AND "), quoteFTS(query)}
	if utf8.RuneCountInString(query) < 3 {
		// Trigram cannot index fewer than three characters. Keep short literal queries usable.
		candidates = `SELECT rowid, 1 AS tier, 0 AS score FROM file_tree
   WHERE user_id = ? AND deleted_at IS NULL AND is_directory = 0 AND min_trust_level <= ?
    AND path LIKE ? AND (instr(lower(path), lower(?)) > 0 OR instr(lower(content), lower(?)) > 0 OR instr(lower(metadata_json), lower(?)) > 0)`
		args = []interface{}{strings.Join(terms, " AND "), userID.String(), trustLevel, prefixLike(prefix), query, query, query}
	}
	sqlQuery := `WITH matches AS MATERIALIZED (
  SELECT rowid, 0 AS tier, rank AS score FROM file_tree_fts WHERE file_tree_fts MATCH ?
  UNION ALL ` + candidates + `
 ), hits AS (SELECT rowid, min(tier) AS tier,
  coalesce(min(CASE WHEN tier = 0 THEN score END), min(score)) AS score FROM matches GROUP BY rowid)
 SELECT ft.id, ft.user_id, ft.path, ft.kind, ft.is_directory, ft.content, ft.content_type, ft.metadata_json,
  ft.checksum, ft.version, ft.min_trust_level, ft.created_at, ft.updated_at, ft.deleted_at
 FROM hits JOIN file_tree ft ON ft.rowid = hits.rowid
 WHERE ft.user_id = ? AND ft.deleted_at IS NULL AND ft.is_directory = 0 AND ft.min_trust_level <= ? AND ft.path LIKE ?
 ORDER BY hits.tier, hits.score, ft.updated_at DESC, ft.path ASC`
	args = append(args, userID.String(), trustLevel, prefixLike(prefix))
	rows, err := s.db.QueryContext(ctx, sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	results := make([]models.FileTreeEntry, 0, 16)
	for rows.Next() {
		entry, err := scanFileTreeEntry(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, *entry)
	}
	return results, rows.Err()
}
