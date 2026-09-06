-- name: GetLiveSession :one
SELECT
    sessions.session_id,
    sessions.state,
    sessions.version,
    sessions.user_id,
    sessions.data,
    sessions.expires_at,
    sessions.challenge_kind,
    sessions.challenge_email,
    sessions.verify_registration
FROM backend.sessions
LEFT JOIN backend.users ON backend.users.id = sessions.user_id
WHERE sessions.session_id = @session_id
  AND sessions.state <> 'revoked'
  AND sessions.expires_at > NOW()
  AND (
      (
          sessions.state = 'pending'
          AND sessions.challenge_kind = 'registration'
          AND sessions.user_id IS NULL
      )
      OR (
          (
              sessions.state = 'authenticated'
              OR (
                  sessions.state = 'pending'
                  AND sessions.challenge_kind = 'sign_in'
              )
          )
          AND sessions.user_id IS NOT NULL
          AND backend.users.id IS NOT NULL
          AND backend.users.enabled
          AND backend.users.deleted_at IS NULL
      )
  );

-- name: IssueSignInChallenge :one
INSERT INTO backend.sessions AS sessions (
    session_id, state, user_id, data, expires_at,
    challenge_kind, challenge_code, challenge_email, challenge_expires_at
)
SELECT
    sqlc.arg(session_id), 'pending', users.id, sqlc.arg(data), NOW() + sqlc.arg(session_ttl)::INTERVAL,
    'sign_in', sqlc.arg(challenge_code), users.email, NOW() + sqlc.arg(challenge_ttl)::INTERVAL
FROM backend.users
WHERE users.id = sqlc.arg(user_id)
  AND users.enabled
  AND users.deleted_at IS NULL
ON CONFLICT (session_id) DO UPDATE SET
    state = EXCLUDED.state,
    version = sessions.version + 1,
    user_id = EXCLUDED.user_id,
    data = EXCLUDED.data,
    expires_at = EXCLUDED.expires_at,
    challenge_kind = EXCLUDED.challenge_kind,
    challenge_code = EXCLUDED.challenge_code,
    challenge_email = EXCLUDED.challenge_email,
    challenge_expires_at = EXCLUDED.challenge_expires_at,
    verify_registration = FALSE,
    registration_invite_id = NULL
WHERE sessions.state = 'pending'
  AND sessions.expires_at > NOW()
  AND sessions.failed_attempts < sqlc.arg(max_attempts)
RETURNING sessions.*;

-- name: IssueRegistrationChallenge :one
INSERT INTO backend.sessions AS sessions (
    session_id, state, data, expires_at,
    challenge_kind, challenge_code, challenge_email, challenge_expires_at,
    registration_invite_id
)
VALUES (
    sqlc.arg(session_id), 'pending', sqlc.arg(data), NOW() + sqlc.arg(session_ttl)::INTERVAL,
    'registration', sqlc.arg(challenge_code), sqlc.arg(challenge_email), NOW() + sqlc.arg(challenge_ttl)::INTERVAL,
    sqlc.narg(invite_id)
)
ON CONFLICT (session_id) DO UPDATE SET
    state = EXCLUDED.state,
    version = sessions.version + 1,
    user_id = NULL,
    data = EXCLUDED.data,
    expires_at = EXCLUDED.expires_at,
    challenge_kind = EXCLUDED.challenge_kind,
    challenge_code = EXCLUDED.challenge_code,
    challenge_email = EXCLUDED.challenge_email,
    challenge_expires_at = EXCLUDED.challenge_expires_at,
    registration_invite_id = EXCLUDED.registration_invite_id
WHERE sessions.state = 'pending'
  AND sessions.expires_at > NOW()
  AND sessions.failed_attempts < sqlc.arg(max_attempts)
RETURNING sessions.*;

-- name: SetVerifyRegistration :one
UPDATE backend.sessions AS sessions
SET verify_registration = TRUE
WHERE session_id = @session_id
  AND state = 'pending'
  AND challenge_kind = 'registration'
  AND expires_at > NOW()
RETURNING sessions.*;

-- name: IssueEmailChangeChallenge :one
UPDATE backend.sessions AS sessions
SET challenge_kind = 'email_change',
    challenge_code = sqlc.arg(challenge_code),
    challenge_email = users.email,
    challenge_expires_at = NOW() + sqlc.arg(challenge_ttl)::INTERVAL,
    failed_attempts = 0,
    version = sessions.version + 1
FROM backend.users AS users
WHERE sessions.session_id = sqlc.arg(session_id)
  AND sessions.state = 'authenticated'
  AND sessions.expires_at > NOW()
  AND sessions.user_id = users.id
  AND users.enabled
  AND users.deleted_at IS NULL
RETURNING sessions.*;

-- name: ResendPendingChallenge :one
UPDATE backend.sessions AS sessions
SET challenge_code = sqlc.arg(challenge_code),
    challenge_expires_at = NOW() + sqlc.arg(challenge_ttl)::INTERVAL,
    version = sessions.version + 1
WHERE session_id = sqlc.arg(session_id)
  AND state = 'pending'
  AND challenge_kind IN ('sign_in', 'registration')
  AND expires_at > NOW()
  AND challenge_expires_at > NOW()
  AND failed_attempts < sqlc.arg(max_attempts)
RETURNING sessions.*;

-- name: InspectSessionChallenge :one
SELECT
    state,
    challenge_kind,
    expires_at <= NOW() AS session_expired,
    challenge_expires_at IS NULL OR challenge_expires_at <= NOW() AS challenge_expired,
    failed_attempts,
    verify_registration
FROM backend.sessions
WHERE session_id = sqlc.arg(session_id);

-- name: ConsumeSignInChallenge :one
WITH changed AS (
    UPDATE backend.sessions AS sessions
    SET state = CASE WHEN challenge_code = sqlc.arg(challenge_code) THEN 'revoked' ELSE state END,
        version = version + 1,
        failed_attempts = CASE
            WHEN challenge_code = sqlc.arg(challenge_code) THEN 0
            ELSE failed_attempts + 1
        END,
        challenge_code = CASE WHEN challenge_code = sqlc.arg(challenge_code) THEN NULL ELSE challenge_code END
    WHERE session_id = sqlc.arg(session_id)
      AND state = 'pending'
      AND challenge_kind = 'sign_in'
      AND expires_at > NOW()
      AND challenge_expires_at > NOW()
      AND failed_attempts < sqlc.arg(max_attempts)
      AND EXISTS (
          SELECT 1
          FROM backend.users
          WHERE users.id = sessions.user_id
            AND users.enabled
            AND users.deleted_at IS NULL
      )
    RETURNING sessions.*
), successor AS (
    INSERT INTO backend.sessions AS sessions (session_id, state, user_id, data, expires_at)
    SELECT
        sqlc.arg(successor_session_id), 'authenticated', changed.user_id,
        sqlc.arg(successor_data), NOW() + sqlc.arg(successor_ttl)::INTERVAL
    FROM changed
    WHERE changed.state = 'revoked'
    RETURNING sessions.*
)
SELECT
    CASE WHEN changed.state = 'revoked' THEN 'succeeded' ELSE 'invalid_code' END::TEXT AS outcome,
    changed.*,
    successor.*
FROM changed
LEFT JOIN successor ON TRUE;

-- name: ConsumeRegistrationChallenge :one
UPDATE backend.sessions AS sessions
SET state = CASE WHEN challenge_code = sqlc.arg(challenge_code) THEN 'revoked' ELSE state END,
    version = version + 1,
    failed_attempts = CASE
        WHEN challenge_code = sqlc.arg(challenge_code) THEN 0
        ELSE failed_attempts + 1
    END,
    challenge_code = CASE WHEN challenge_code = sqlc.arg(challenge_code) THEN NULL ELSE challenge_code END
WHERE session_id = sqlc.arg(session_id)
  AND state = 'pending'
  AND challenge_kind = 'registration'
  AND expires_at > NOW()
  AND challenge_expires_at > NOW()
  AND failed_attempts < sqlc.arg(max_attempts)
  AND (
      challenge_code IS DISTINCT FROM sqlc.arg(challenge_code)
      OR verify_registration = FALSE
  )
RETURNING
    CASE WHEN sessions.state = 'revoked' THEN 'succeeded' ELSE 'invalid_code' END::TEXT AS outcome,
    sessions.*;

-- name: CreateRegistrationSuccessor :one
INSERT INTO backend.sessions AS sessions (session_id, state, user_id, data, expires_at)
SELECT
    sqlc.arg(session_id), 'authenticated', users.id,
    sqlc.arg(data), NOW() + sqlc.arg(session_ttl)::INTERVAL
FROM backend.users
WHERE users.id = sqlc.arg(user_id)
  AND users.enabled
  AND users.deleted_at IS NULL
ON CONFLICT (session_id) DO NOTHING
RETURNING sessions.*;

-- name: ConsumeEmailChangeChallenge :one
UPDATE backend.sessions AS sessions
SET version = version + 1,
    failed_attempts = CASE
        WHEN challenge_code = sqlc.arg(challenge_code) THEN 0
        ELSE failed_attempts + 1
    END,
    challenge_kind = CASE WHEN challenge_code = sqlc.arg(challenge_code) THEN NULL ELSE challenge_kind END,
    challenge_code = CASE WHEN challenge_code = sqlc.arg(challenge_code) THEN NULL ELSE challenge_code END,
    challenge_email = CASE WHEN challenge_code = sqlc.arg(challenge_code) THEN NULL ELSE challenge_email END,
    challenge_expires_at = CASE WHEN challenge_code = sqlc.arg(challenge_code) THEN NULL ELSE challenge_expires_at END
WHERE session_id = sqlc.arg(session_id)
  AND state = 'authenticated'
  AND challenge_kind = 'email_change'
  AND expires_at > NOW()
  AND challenge_expires_at > NOW()
  AND failed_attempts < sqlc.arg(max_attempts)
  AND EXISTS (
      SELECT 1
      FROM backend.users
      WHERE users.id = sessions.user_id
        AND users.enabled
        AND users.deleted_at IS NULL
  )
RETURNING
    CASE WHEN sessions.challenge_kind IS NULL THEN 'succeeded' ELSE 'invalid_code' END::TEXT AS outcome,
    sessions.*;

-- name: UpdateSessionPayloads :many
WITH payload_updates AS (
    SELECT
        UNNEST(sqlc.arg(session_ids)::TEXT[]) AS session_id,
        UNNEST(sqlc.arg(expected_versions)::INT[]) AS expected_version,
        UNNEST(sqlc.arg(payloads)::BYTEA[]) AS data
)
UPDATE backend.sessions AS sessions
SET data = payload_updates.data,
    version = sessions.version + 1
FROM payload_updates
WHERE sessions.session_id = payload_updates.session_id
  AND sessions.version = payload_updates.expected_version
  AND sessions.state IN ('pending', 'authenticated')
  AND sessions.expires_at > NOW()
RETURNING sessions.session_id, sessions.version;

-- name: RenewSessionExpirations :many
UPDATE backend.sessions
SET expires_at = GREATEST(expires_at, NOW() + sqlc.arg(ttl)::INTERVAL)
WHERE session_id = ANY(sqlc.arg(session_ids)::TEXT[])
  AND state = 'authenticated'
  AND expires_at > NOW()
RETURNING session_id, version, expires_at;

-- name: RevokeSession :one
UPDATE backend.sessions
SET state = 'revoked',
    version = version + CASE WHEN state = 'revoked' THEN 0 ELSE 1 END,
    challenge_code = NULL
WHERE session_id = @session_id
  AND expires_at > NOW()
RETURNING session_id, state, version, user_id;

-- name: DeleteExpiredSessions :execrows
DELETE FROM backend.sessions
WHERE expires_at <= NOW();

-- name: RevokeUserSessions :many
UPDATE backend.sessions
SET state = 'revoked',
    version = version + CASE WHEN state = 'revoked' THEN 0 ELSE 1 END,
    challenge_code = NULL
WHERE user_id = @user_id
  AND expires_at > NOW()
RETURNING session_id, state, version;
