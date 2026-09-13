-- Search indexes must match FileTreeService.Search's expression exactly.
-- Include paths and metadata so all imported records share the same searchable fields.
CREATE EXTENSION IF NOT EXISTS pg_trgm;

DROP INDEX IF EXISTS idx_file_tree_content_search;
CREATE INDEX idx_file_tree_content_search ON file_tree USING GIN (
    to_tsvector('simple', coalesce(path, '') || ' ' || coalesce(content, '') || ' ' || coalesce(metadata::text, ''))
) WHERE deleted_at IS NULL AND is_directory = false;

CREATE INDEX IF NOT EXISTS idx_file_tree_substring_search ON file_tree USING GIN (
    (coalesce(path, '') || ' ' || coalesce(content, '') || ' ' || coalesce(metadata::text, '')) gin_trgm_ops
) WHERE deleted_at IS NULL AND is_directory = false;
