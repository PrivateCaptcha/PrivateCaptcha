-- name: GetSubscriptionByID :one
SELECT * FROM backend.subscriptions WHERE id = $1;

-- name: CreateSubscription :one
INSERT INTO backend.subscriptions (external_product_id, external_price_id, external_subscription_id, external_customer_id, external_email, status, source, trial_ends_at, next_billed_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING *;

-- name: UpdateInternalSubscriptions :execrows
UPDATE backend.subscriptions
SET status = $1, updated_at = NOW()
WHERE
  source = 'internal' AND
  trial_ends_at IS NOT NULL AND
  trial_ends_at BETWEEN $2 AND $3 AND
  status = $4 AND
  next_billed_at IS NULL;
