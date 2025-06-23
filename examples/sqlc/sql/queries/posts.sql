-- name: CreatePost :one
INSERT INTO posts (
    user_id, title, slug, content, published, published_at
) VALUES (
    $1, $2, $3, $4, $5, $6
)
RETURNING *;

-- name: GetPost :one
SELECT * FROM posts
WHERE id = $1 LIMIT 1;

-- name: GetPostBySlug :one
SELECT * FROM posts
WHERE slug = $1 LIMIT 1;

-- name: ListPosts :many
SELECT * FROM posts
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: ListPublishedPosts :many
SELECT * FROM posts
WHERE published = true
ORDER BY published_at DESC
LIMIT $1 OFFSET $2;

-- name: ListPostsByUser :many
SELECT * FROM posts
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: UpdatePost :one
UPDATE posts
SET title = $2,
    slug = $3,
    content = $4,
    published = $5,
    published_at = $6
WHERE id = $1
RETURNING *;

-- name: PublishPost :one
UPDATE posts
SET published = true,
    published_at = CURRENT_TIMESTAMP
WHERE id = $1
RETURNING *;

-- name: DeletePost :exec
DELETE FROM posts
WHERE id = $1;

-- name: CountPosts :one
SELECT COUNT(*) FROM posts;

-- name: CountPublishedPosts :one
SELECT COUNT(*) FROM posts WHERE published = true;

-- name: GetPostWithAuthor :one
SELECT 
    p.*,
    u.id as author_id,
    u.username as author_username,
    u.email as author_email
FROM posts p
JOIN users u ON p.user_id = u.id
WHERE p.id = $1
LIMIT 1;