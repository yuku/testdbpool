-- name: CreateComment :one
INSERT INTO comments (
    post_id, user_id, content
) VALUES (
    $1, $2, $3
)
RETURNING *;

-- name: GetComment :one
SELECT * FROM comments
WHERE id = $1 LIMIT 1;

-- name: ListCommentsByPost :many
SELECT 
    c.*,
    u.username as author_username
FROM comments c
JOIN users u ON c.user_id = u.id
WHERE c.post_id = $1
ORDER BY c.created_at ASC;

-- name: ListCommentsByUser :many
SELECT * FROM comments
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: UpdateComment :one
UPDATE comments
SET content = $2
WHERE id = $1
RETURNING *;

-- name: DeleteComment :exec
DELETE FROM comments
WHERE id = $1;

-- name: DeleteCommentsByPost :exec
DELETE FROM comments
WHERE post_id = $1;

-- name: CountCommentsByPost :one
SELECT COUNT(*) FROM comments
WHERE post_id = $1;

-- name: GetPostsWithCommentCounts :many
SELECT 
    p.*,
    COUNT(c.id) as comment_count
FROM posts p
LEFT JOIN comments c ON p.id = c.post_id
WHERE p.published = true
GROUP BY p.id
ORDER BY p.published_at DESC;