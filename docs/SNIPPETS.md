# Dev snippets

## Obtain Postgres cmdline

```bash
docker exec -it docker-postgres-1 psql -U postgres
```

## Clickhouse cmdline

```bash
docker exec -it docker-clickhouse-1 clickhouse-client
```

## Seed clickhouse events

```sql
INSERT INTO privatecaptcha.request_logs (user_id, org_id, property_id, fingerprint, rule_id, timestamp)
SELECT
    1 as user_id,
    1 as org_id,
    1 as property_id,
    rand64() as fingerprint,
    (rand() % 3) as rule_id,
    now() - toIntervalSecond((rand() % ((3600 * 24) * 365)) + number) AS timestamp
FROM numbers(100000);
```

```sql
INSERT INTO privatecaptcha.verify_logs (user_id, org_id, property_id, puzzle_id, status, timestamp)
SELECT
    1 as user_id,
    1 as org_id,
    1 as property_id,
    rand64() as puzzle_id,
    0 as status,
    now() - toIntervalSecond((rand() % ((3600 * 24) * 365)) + number) AS timestamp
FROM numbers(50000);
```

## Create new user

```sql
INSERT INTO backend.subscriptions (external_product_id, external_price_id, status, source, trial_ends_at) VALUES ('pctrial_CGK710ObXUu3hnErY87KMx4gnt3', 'pctrial_qD6rwF1UomfdkgbOjaepoDn0RxX', 'trialing', 'internal', NOW() + INTERVAL '1 year') RETURNING *;
```

```sql
INSERT INTO backend.users (name, email, subscription_id) VALUES ('Foo Bar', 'foo.bar@privatecaptcha.local', 2);
```
