-- name: AddUsageLimitViolations :exec
INSERT INTO usage_limit_violations (user_id, paddle_product_id, requests_limit, requests_count, detection_month, last_violated_at)
SELECT unnest(@user_ids::INT[]) AS user_id,
       unnest(@products::TEXT[]) AS paddle_product_id,
       unnest(@limits::BIGINT[]) AS requests_limit,
       unnest(@counts::BIGINT[]) AS requests_count,
       date_trunc('month', unnest(@dates::date[])) AS detection_month,
       unnest(@dates::date[]) AS last_violated_at
ON CONFLICT (user_id, paddle_product_id, detection_month)
DO UPDATE SET
    paddle_product_id = EXCLUDED.paddle_product_id,
    requests_limit = EXCLUDED.requests_limit,
    requests_count = EXCLUDED.requests_count,
    last_violated_at = GREATEST(usage_limit_violations.last_violated_at, EXCLUDED.last_violated_at);

-- name: GetUsersWithConsecutiveViolations :many
SELECT sqlc.embed(u)
FROM usage_limit_violations v1
JOIN usage_limit_violations v2 ON v1.user_id = v2.user_id
JOIN users u ON v1.user_id = u.id
JOIN subscriptions s ON u.subscription_id = s.id
WHERE s.paddle_product_id = v1.paddle_product_id
  AND u.deleted_at IS NULL
  AND EXTRACT(YEAR FROM v1.detection_month) = EXTRACT(YEAR FROM CURRENT_DATE)
  AND EXTRACT(MONTH FROM v1.detection_month) = EXTRACT(MONTH FROM CURRENT_DATE)
  AND EXTRACT(YEAR FROM v2.detection_month) = EXTRACT(YEAR FROM CURRENT_DATE - INTERVAL '1 MONTH')
  AND EXTRACT(MONTH FROM v2.detection_month) = EXTRACT(MONTH FROM CURRENT_DATE - INTERVAL '1 MONTH');

-- name: GetUsersWithLargeViolations :many
SELECT sqlc.embed(u), sqlc.embed(uv), s.status as status
FROM users u
JOIN usage_limit_violations uv ON u.id = uv.user_id
JOIN subscriptions s ON u.subscription_id = s.id
WHERE s.paddle_product_id = uv.paddle_product_id
  AND u.deleted_at IS NULL
  AND uv.requests_count >= ($1::float * uv.requests_limit)
  AND uv.last_violated_at >= $2::date;
