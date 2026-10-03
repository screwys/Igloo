-- name: CreateBookmarkCategory :one
INSERT INTO bookmark_categories(name,archive_path,created_at) VALUES ($1,$2,$3) RETURNING id;

-- name: CreateMutationBookmarkCategory :one
INSERT INTO bookmark_categories(name,created_at) VALUES ($1,$2) RETURNING id;
