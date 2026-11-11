-- One row per (user, link). Earlier versions inserted a new row on
-- every click, so remove duplicates (keeping the newest) before adding
-- the unique index the upsert in the app relies on.
DELETE FROM visited_links
WHERE id NOT IN (SELECT MAX(id) FROM visited_links GROUP BY user_id, link_url);

DROP INDEX IF EXISTS idx_visited_links_user;
CREATE UNIQUE INDEX IF NOT EXISTS idx_visited_links_user_url ON visited_links(user_id, link_url);

-- Record execution of this migration
INSERT OR IGNORE INTO migrations (migration_number, migration_name)
VALUES (012, '012-visited-links-unique');
