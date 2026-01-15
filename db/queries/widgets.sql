-- name: GetWidgetByID :one
SELECT * FROM widgets WHERE id = ?;

-- name: GetWidgetsByPageID :many
SELECT * FROM widgets WHERE page_id = ? ORDER BY created_at;

-- name: CreateWidget :exec
INSERT INTO widgets (id, page_id, title, widget_type, pos_x, pos_y, width, height, bg_color, text_color, header_color, config, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateWidget :exec
UPDATE widgets SET title = ?, widget_type = ?, pos_x = ?, pos_y = ?, width = ?, height = ?, bg_color = ?, text_color = ?, header_color = ?, config = ?, updated_at = ? WHERE id = ?;

-- name: DeleteWidget :exec
DELETE FROM widgets WHERE id = ?;
