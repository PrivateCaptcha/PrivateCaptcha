-- name: AddUsageLimitViolations :exec
INSERT INTO usage_limit_violations (user_id, paddle_product_id, requests_limit, requests_count, detection_date)
SELECT unnest(@user_ids::INT[]) AS user_id,
       unnest(@products::TEXT[]) AS paddle_product_id,
       unnest(@limits::BIGINT[]) AS requests_limit,
       unnest(@counts::BIGINT[]) AS requests_count,
       unnest(@dates::date[]) AS detection_date
ON CONFLICT (user_id, paddle_product_id, detection_date)
DO UPDATE SET
    paddle_product_id = EXCLUDED.paddle_product_id,
    requests_limit = EXCLUDED.requests_limit,
    requests_count = EXCLUDED.requests_count,
    detection_date = EXCLUDED.detection_date;

-- name: GetUsersWithConsecutiveViolations :many
SELECT sqlc.embed(u)
FROM usage_limit_violations v1
JOIN usage_limit_violations v2 ON v1.user_id = v2.user_id
JOIN users u ON v1.user_id = u.id
WHERE EXTRACT(YEAR FROM v1.detection_date) = EXTRACT(YEAR FROM CURRENT_DATE)
  AND EXTRACT(MONTH FROM v1.detection_date) = EXTRACT(MONTH FROM CURRENT_DATE)
  AND EXTRACT(YEAR FROM v2.detection_date) = EXTRACT(YEAR FROM CURRENT_DATE - INTERVAL '1 MONTH')
  AND EXTRACT(MONTH FROM v2.detection_date) = EXTRACT(MONTH FROM CURRENT_DATE - INTERVAL '1 MONTH');

-- name: GetUsersWithLargeViolations :many
SELECT sqlc.embed(u)
FROM users u
JOIN usage_limit_violations uv ON u.id = uv.user_id
WHERE uv.requests_count >= ($1::float * uv.requests_limit)
  AND EXTRACT(YEAR FROM uv.detection_date) = EXTRACT(YEAR FROM CURRENT_DATE)
  AND EXTRACT(MONTH FROM uv.detection_date) = EXTRACT(MONTH FROM CURRENT_DATE);
