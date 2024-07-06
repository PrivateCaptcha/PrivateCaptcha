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
