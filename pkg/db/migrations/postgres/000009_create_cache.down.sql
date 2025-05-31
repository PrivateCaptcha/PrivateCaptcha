DROP PROCEDURE IF EXISTS backend.delete_expired_cache;

DROP INDEX IF EXISTS index_cache_key;

DROP TABLE IF EXISTS backend.cache;
