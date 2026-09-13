# Search storage and indexes

Search covers live, non-directory file-tree entries. The searchable fields are
path, text content and serialized metadata. User, trust-level and path filters
are applied before returning results. Public API/MCP visibility rules still
exclude hidden feature roots. Binary payloads are not decoded for text search.

## SQLite

On database open, neuDrive creates two external-content FTS5 indexes and backfills
existing live files in one transaction:

- `file_tree_fts`: Unicode word matching and BM25 ranking.
- `file_tree_trigram`: literal substring matching, including Chinese strings and
  code fragments of at least three Unicode characters.

Database triggers maintain both indexes for inserts, updates, soft deletion,
restoration and physical deletion. They roll back with the content transaction.
Subsequent opens reuse the indexes rather than rebuilding them. External-content
tables reference the original document bodies instead of storing another copy.
The two inverted indexes still consume additional disk space and write work.

User input is quoted as literal FTS terms; it is not an FTS query language.
Whitespace-separated terms are combined with AND. Whole-word matches rank before
substring-only matches, then BM25 and update time order the results. One- and
two-character substring queries fall back to a filtered scan. Chinese substring
matching is supported; linguistic Chinese word segmentation is not implemented.

## PostgreSQL

Migration `021_file_tree_search_indexes.sql` enables `pg_trgm`, replaces the old
content-only GIN index and adds a trigram GIN index. Both indexes cover the exact
path/content/metadata expression used by `FileTreeService.Search`, restricted to
live files. The migration builds indexes for existing data and can block writes
while it runs; the migration runner executes it transactionally.

Word search uses `to_tsvector('simple', ...)` and `plainto_tsquery`. Literal
substring search uses `ILIKE`, with SQL wildcard characters escaped. Full-text
matches rank first, followed by `ts_rank_cd` and update time. The existing
50-result limit is retained. Patterns with insufficient extractable trigrams can
still require scanning. PostgreSQL and SQLite tokenization and rank values are
not identical. The API's existing numeric `score` field remains a placeholder;
results are ordered by the database's ranking.

GIN uses a pending list for writes; normal autovacuum helps merge that list after
bulk imports. Index availability does not force the planner to use an index for
small tables or low-selectivity queries.

## Verification

- `go test ./...`
- `NEUDRIVE_TEST_DB=... go test ./internal/services -run TestIndexedSearchPostgres -count=1`

Tests cover fields, word and substring matching, Chinese and code queries,
isolation, update/delete/restore behavior, SQLite backfill and rollback, and
index access plans. The PostgreSQL test requires a disposable test database.
