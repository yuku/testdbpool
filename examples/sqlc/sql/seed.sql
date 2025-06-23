-- Seed data for blog database

-- Insert users
INSERT INTO users (id, username, email, password_hash) VALUES
(1, 'alice', 'alice@example.com', '$2a$10$YKbD3l.8kFHKZMKNQRsvVOJ.WnQvvGd0mXvQKXn.0l7n5F9nOXXXX'),
(2, 'bob', 'bob@example.com', '$2a$10$YKbD3l.8kFHKZMKNQRsvVOJ.WnQvvGd0mXvQKXn.0l7n5F9nOYYYY'),
(3, 'charlie', 'charlie@example.com', '$2a$10$YKbD3l.8kFHKZMKNQRsvVOJ.WnQvvGd0mXvQKXn.0l7n5F9nOZZZZ');

-- Reset sequences
SELECT setval('users_id_seq', 3, true);

-- Insert posts
INSERT INTO posts (id, user_id, title, slug, content, published, published_at) VALUES
(1, 1, 'Getting Started with Go', 'getting-started-with-go', 'Go is a great language for building scalable applications...', true, CURRENT_TIMESTAMP - INTERVAL '5 days'),
(2, 1, 'Understanding Goroutines', 'understanding-goroutines', 'Goroutines are lightweight threads managed by the Go runtime...', true, CURRENT_TIMESTAMP - INTERVAL '3 days'),
(3, 2, 'Database Design Best Practices', 'database-design-best-practices', 'When designing a database, consider these key principles...', true, CURRENT_TIMESTAMP - INTERVAL '2 days'),
(4, 2, 'Draft: PostgreSQL Performance', 'postgresql-performance', 'This post is still in draft...', false, NULL),
(5, 3, 'Testing in Go', 'testing-in-go', 'Writing tests in Go is straightforward with the testing package...', true, CURRENT_TIMESTAMP - INTERVAL '1 day');

-- Reset sequences
SELECT setval('posts_id_seq', 5, true);

-- Insert comments
INSERT INTO comments (id, post_id, user_id, content) VALUES
(1, 1, 2, 'Great introduction! Looking forward to more Go content.'),
(2, 1, 3, 'This helped me get started with my first Go project.'),
(3, 2, 3, 'Could you explain more about channels?'),
(4, 2, 1, 'Sure! I''ll write a follow-up post about channels.'),
(5, 3, 1, 'Excellent tips on normalization.'),
(6, 3, 3, 'What about NoSQL databases?'),
(7, 5, 1, 'Table-driven tests are my favorite Go feature!'),
(8, 5, 2, 'Don''t forget about benchmarks too.');

-- Reset sequences
SELECT setval('comments_id_seq', 8, true);