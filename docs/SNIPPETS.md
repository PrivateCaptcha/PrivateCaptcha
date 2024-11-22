# Dev snippets

## Obtain Postgres cmdline

```bash
docker exec -it docker-postgres-1 psql -U postgres
```

## Seed clickhouse events

```sql
INSERT INTO privatecaptcha.request_logs (user_id, org_id, property_id, fingerprint, timestamp)
SELECT
    1 as user_id,
    1 as org_id,
    1 as property_id,
    rand64() as fingerprint,
    now() - toIntervalSecond(rand() % ((3600 * 24) * 365)) AS timestamp
FROM numbers(100000);
```
