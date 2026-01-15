-- name: MarkLinkVisited :exec
INSERT INTO visited_links (user_id, link_url, visited_at) VALUES (?, ?, ?);

-- name: GetVisitedLinks :many
SELECT DISTINCT link_url FROM visited_links WHERE user_id = ? AND visited_at > ?;

-- name: CleanupOldVisitedLinks :exec
DELETE FROM visited_links WHERE visited_at < ?;
