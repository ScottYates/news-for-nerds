-- Track visited links for 48 hours
CREATE TABLE IF NOT EXISTS visited_links (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id TEXT NOT NULL,
    link_url TEXT NOT NULL,
    visited_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_visited_links_user ON visited_links(user_id, link_url);
CREATE INDEX IF NOT EXISTS idx_visited_links_time ON visited_links(visited_at);

-- Record execution of this migration
INSERT OR IGNORE INTO migrations (migration_number, migration_name)
VALUES (004, '004-visited-links');
