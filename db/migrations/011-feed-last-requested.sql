-- Track when a feed was last asked for by a dashboard, so the
-- background refresher can skip feeds nobody reads and old rows can
-- be removed. NULL means "not recorded yet" (rows from before this
-- migration); the refresher stamps those on its next pass.
ALTER TABLE rss_feeds ADD COLUMN last_requested TIMESTAMP;

-- Record execution of this migration
INSERT OR IGNORE INTO migrations (migration_number, migration_name)
VALUES (011, '011-feed-last-requested');
