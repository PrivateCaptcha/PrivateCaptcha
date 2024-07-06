-- name: GetSubscriptionByID :one
SELECT * FROM subscriptions WHERE id = $1;

-- name: GetSubscriptionsByUserIDs :many
SELECT sqlc.embed(s), u.id AS user_id
FROM subscriptions s
JOIN users u on u.subscription_id = s.id
WHERE u.id = ANY($1::INT[]) AND u.subscription_id IS NOT NULL;

-- name: CreateSubscription :one
INSERT INTO subscriptions (paddle_product_id, paddle_price_id, paddle_subscription_id, paddle_customer_id, status, trial_ends_at, next_billed_at) VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING *;

-- name: UpdateSubscription :one
UPDATE subscriptions SET paddle_product_id = $2, status = $3, next_billed_at = $4, cancel_from = $5, updated_at = NOW() WHERE paddle_subscription_id = $1 RETURNING *;
