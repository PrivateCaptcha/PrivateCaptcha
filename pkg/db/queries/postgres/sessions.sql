-- name: CreateSession :one
INSERT INTO backend.sessions (
    session_id,
    state,
    user_id,
    data,
    expires_at,
    challenge_kind,
    challenge_code,
    challenge_email,
    challenge_expires_at
) VALUES (
    @session_id,
    @state,
    @user_id,
    @data,
    @expires_at,
    @challenge_kind,
    @challenge_code,
    @challenge_email,
    @challenge_expires_at
)
RETURNING *;

-- name: GetSessionByID :one
SELECT *
FROM backend.sessions
WHERE session_id = $1
  AND expires_at >= NOW();

-- name: GetSessionByIDUnfiltered :one
SELECT *
FROM backend.sessions
WHERE session_id = $1;

-- name: UpdateSessionDataCAS :many
WITH input AS (
    SELECT
        unnest(@session_ids::TEXT[]) AS session_id,
        unnest(@expected_versions::INT[]) AS expected_version,
        unnest(@payloads::BYTEA[]) AS data
)
UPDATE backend.sessions AS sessions
SET
    data = input.data,
    version = sessions.version + 1
FROM input
WHERE sessions.session_id = input.session_id
  AND sessions.version = input.expected_version
  AND sessions.state IN ('pending', 'authenticated')
  AND sessions.expires_at >= NOW()
RETURNING sessions.session_id, sessions.version;

-- name: ExtendAuthenticatedSessionExpirations :many
UPDATE backend.sessions
SET expires_at = NOW() + @ttl::INTERVAL
WHERE session_id = ANY(@session_ids::TEXT[])
  AND state = 'authenticated'
  AND expires_at >= NOW()
RETURNING session_id, expires_at;

-- name: IssueEmailChangeChallenge :one
UPDATE backend.sessions AS sessions
SET
    challenge_kind = 'email_change',
    challenge_code = CASE
        WHEN sessions.challenge_code IS DISTINCT FROM @encoded_code::TEXT THEN @encoded_code::TEXT
        ELSE @fallback_encoded_code::TEXT
    END,
    challenge_email = users.email,
    challenge_expires_at = LEAST(sessions.expires_at, NOW() + @challenge_ttl::INTERVAL),
    failed_attempts = 0,
    version = sessions.version + 1
FROM backend.users AS users
WHERE sessions.session_id = @session_id::TEXT
  AND sessions.state = 'authenticated'
  AND sessions.version = @expected_version::INT
  AND sessions.user_id = @expected_user_id::INT
  AND sessions.expires_at >= NOW()
  AND users.id = sessions.user_id
  AND users.enabled
  AND users.deleted_at IS NULL
RETURNING sessions.*;

-- name: ConsumeEmailChangeChallenge :one
WITH challenge AS MATERIALIZED (
    SELECT
        sessions.session_id,
        sessions.state,
        sessions.version,
        sessions.user_id,
        sessions.data,
        sessions.expires_at,
        sessions.challenge_kind,
        sessions.challenge_code,
        sessions.challenge_email,
        sessions.challenge_expires_at,
        sessions.failed_attempts,
        users.id AS old_user_id,
        users.name AS old_user_name,
        users.email AS old_user_email,
        users.subscription_id AS old_user_subscription_id,
        users.created_at AS old_user_created_at,
        users.updated_at AS old_user_updated_at,
        users.deleted_at AS old_user_deleted_at,
        users.enabled AS old_user_enabled
    FROM backend.sessions AS sessions
    JOIN backend.users AS users ON users.id = sessions.user_id
    WHERE sessions.session_id = @session_id::TEXT
    FOR UPDATE OF sessions, users
), eligible AS MATERIALIZED (
    SELECT *
    FROM challenge
    WHERE state = 'authenticated'
      AND user_id = @expected_user_id::INT
      AND challenge_kind = 'email_change'
      AND challenge_code IS NOT NULL
      AND challenge_email = old_user_email
      AND challenge_expires_at >= NOW()
      AND expires_at >= NOW()
      AND old_user_enabled
      AND old_user_deleted_at IS NULL
), updated_user AS (
    UPDATE backend.users AS users
    SET email = @new_email::TEXT, updated_at = NOW()
    FROM eligible
    WHERE users.id = eligible.old_user_id
      AND eligible.failed_attempts < @max_failed_attempts::INT
      AND eligible.challenge_code = @encoded_code::TEXT
    RETURNING users.email, users.updated_at
), attempted AS (
    UPDATE backend.sessions AS sessions
    SET
        challenge_kind = CASE WHEN EXISTS (SELECT 1 FROM updated_user) THEN NULL ELSE sessions.challenge_kind END,
        challenge_code = CASE WHEN EXISTS (SELECT 1 FROM updated_user) THEN NULL ELSE sessions.challenge_code END,
        challenge_email = CASE WHEN EXISTS (SELECT 1 FROM updated_user) THEN NULL ELSE sessions.challenge_email END,
        challenge_expires_at = CASE WHEN EXISTS (SELECT 1 FROM updated_user) THEN NULL ELSE sessions.challenge_expires_at END,
        failed_attempts = CASE
            WHEN EXISTS (SELECT 1 FROM updated_user) THEN 0
            WHEN eligible.failed_attempts < @max_failed_attempts::INT
                AND eligible.challenge_code <> @encoded_code::TEXT
                THEN sessions.failed_attempts + 1
            ELSE sessions.failed_attempts
        END,
        version = sessions.version + CASE
            WHEN EXISTS (SELECT 1 FROM updated_user) THEN 1
            WHEN eligible.failed_attempts < @max_failed_attempts::INT
                AND eligible.challenge_code <> @encoded_code::TEXT
                THEN 1
            ELSE 0
        END
    FROM eligible
    WHERE sessions.session_id = eligible.session_id
    RETURNING
        EXISTS (SELECT 1 FROM updated_user) AS consumed,
        sessions.failed_attempts >= @max_failed_attempts::INT AS attempts_exhausted,
        sessions.session_id,
        sessions.state,
        eligible.version AS previous_version,
        sessions.version,
        sessions.user_id,
        sessions.data,
        sessions.expires_at,
        eligible.old_user_id,
        eligible.old_user_name,
        eligible.old_user_email,
        eligible.old_user_subscription_id,
        eligible.old_user_created_at,
        eligible.old_user_updated_at,
        eligible.old_user_deleted_at,
        eligible.old_user_enabled
)
SELECT attempted.*, updated_user.email AS new_email, updated_user.updated_at AS new_user_updated_at
FROM attempted
LEFT JOIN updated_user ON attempted.consumed;

-- name: ConsumeSignInChallenge :one
WITH challenge AS MATERIALIZED (
    SELECT
        session_id,
        state,
        user_id,
        expires_at,
        challenge_kind,
        challenge_code,
        challenge_email,
        challenge_expires_at,
        failed_attempts,
        users.email AS user_email,
        users.enabled AS user_enabled,
        users.deleted_at AS user_deleted_at
    FROM backend.sessions AS sessions
    JOIN backend.users AS users ON users.id = sessions.user_id
    WHERE sessions.session_id = @old_session_id::TEXT
    FOR UPDATE OF sessions, users
), attempted AS (
    UPDATE backend.sessions AS sessions
    SET
        state = CASE
            WHEN challenge.failed_attempts < @max_failed_attempts::INT
                AND challenge.challenge_code = @encoded_code::TEXT
                THEN 'revoked'::backend.session_state
            ELSE sessions.state
        END,
        version = sessions.version + CASE
            WHEN challenge.failed_attempts < @max_failed_attempts::INT
                AND challenge.challenge_code = @encoded_code::TEXT
                THEN 1
            ELSE 0
        END,
        failed_attempts = sessions.failed_attempts + CASE
            WHEN challenge.failed_attempts < @max_failed_attempts::INT
                AND challenge.challenge_code <> @encoded_code::TEXT
                THEN 1
            ELSE 0
        END
    FROM challenge
    WHERE sessions.session_id = challenge.session_id
      AND challenge.state = 'pending'
      AND challenge.challenge_kind = 'sign_in'
      AND challenge.challenge_expires_at >= NOW()
      AND challenge.expires_at >= NOW()
      AND challenge.user_id = @expected_user_id::INT
      AND challenge.challenge_email = challenge.user_email
      AND challenge.user_enabled
      AND challenge.user_deleted_at IS NULL
    RETURNING
        sessions.state = 'revoked'::backend.session_state AS consumed,
        sessions.failed_attempts >= @max_failed_attempts::INT AS attempts_exhausted,
        sessions.user_id
), successor AS (
    INSERT INTO backend.sessions (
        session_id,
        state,
        user_id,
        data,
        expires_at
    )
    SELECT
        @new_session_id::TEXT,
        'authenticated'::backend.session_state,
        attempted.user_id,
        @successor_data::BYTEA,
        NOW() + @successor_ttl::INTERVAL
    FROM attempted
    WHERE attempted.consumed
    RETURNING session_id, state, version, user_id, expires_at
)
SELECT
    attempted.consumed,
    attempted.attempts_exhausted,
    successor.session_id,
    successor.state,
    successor.version,
    successor.user_id,
    successor.expires_at
FROM attempted
LEFT JOIN successor ON attempted.consumed;

-- name: ReissueSignInChallenge :one
UPDATE backend.sessions AS sessions
SET
    challenge_code = CASE
        WHEN sessions.challenge_code <> @encoded_code::TEXT THEN @encoded_code::TEXT
        ELSE @fallback_encoded_code::TEXT
    END,
    challenge_expires_at = LEAST(sessions.expires_at, NOW() + @challenge_ttl::INTERVAL),
    failed_attempts = 0,
    version = sessions.version + 1
FROM backend.users AS users
WHERE sessions.session_id = @session_id::TEXT
  AND sessions.state = 'pending'
  AND sessions.challenge_kind = 'sign_in'
  AND sessions.challenge_code IS NOT NULL
  AND sessions.challenge_email = users.email
  AND sessions.user_id = users.id
  AND users.enabled
  AND users.deleted_at IS NULL
  AND sessions.expires_at >= NOW()
RETURNING sessions.*;

-- name: ConsumeRegistrationChallenge :one
WITH challenge AS MATERIALIZED (
    SELECT
        session_id,
        state,
        user_id,
        expires_at,
        challenge_kind,
        challenge_code,
        challenge_email,
        challenge_expires_at,
        failed_attempts
    FROM backend.sessions
    WHERE session_id = @old_session_id::TEXT
    FOR UPDATE
), attempted AS (
    UPDATE backend.sessions AS sessions
    SET
        state = CASE
            WHEN challenge.failed_attempts < @max_failed_attempts::INT
                AND challenge.challenge_code = @encoded_code::TEXT
                AND @allow_consumption::BOOL
                THEN 'revoked'::backend.session_state
            ELSE sessions.state
        END,
        version = sessions.version + CASE
            WHEN challenge.failed_attempts < @max_failed_attempts::INT
                AND challenge.challenge_code = @encoded_code::TEXT
                AND @allow_consumption::BOOL
                THEN 1
            ELSE 0
        END,
        failed_attempts = sessions.failed_attempts + CASE
            WHEN challenge.failed_attempts < @max_failed_attempts::INT
                AND challenge.challenge_code <> @encoded_code::TEXT
                THEN 1
            ELSE 0
        END
    FROM challenge
    WHERE sessions.session_id = challenge.session_id
      AND challenge.state = 'pending'
      AND challenge.challenge_kind = 'registration'
      AND challenge.challenge_expires_at >= NOW()
      AND challenge.expires_at >= NOW()
      AND challenge.user_id IS NULL
      AND challenge.challenge_email IS NOT NULL
    RETURNING
        sessions.state = 'revoked'::backend.session_state AS consumed,
        challenge.failed_attempts < @max_failed_attempts::INT
            AND challenge.challenge_code = @encoded_code::TEXT AS verified,
        sessions.failed_attempts >= @max_failed_attempts::INT AS attempts_exhausted,
        challenge.challenge_email
), successor AS (
    INSERT INTO backend.sessions (
        session_id,
        state,
        data,
        expires_at
    )
    SELECT
        @new_session_id::TEXT,
        'registration_processing'::backend.session_state,
        @successor_data::BYTEA,
        NOW() + @successor_ttl::INTERVAL
    FROM attempted
    WHERE attempted.consumed
    RETURNING session_id, state, version, user_id, expires_at
)
SELECT
    attempted.consumed,
    attempted.verified,
    attempted.attempts_exhausted,
    attempted.challenge_email,
    successor.session_id,
    successor.state,
    successor.version,
    successor.user_id,
    successor.expires_at
FROM attempted
LEFT JOIN successor ON attempted.consumed;

-- name: ReissueRegistrationChallenge :one
UPDATE backend.sessions
SET
    challenge_code = CASE
        WHEN challenge_code <> @encoded_code::TEXT THEN @encoded_code::TEXT
        ELSE @fallback_encoded_code::TEXT
    END,
    challenge_expires_at = LEAST(expires_at, NOW() + @challenge_ttl::INTERVAL),
    failed_attempts = 0,
    version = version + 1
WHERE session_id = @session_id::TEXT
  AND state = 'pending'
  AND challenge_kind = 'registration'
  AND challenge_code IS NOT NULL
  AND challenge_email IS NOT NULL
  AND expires_at >= NOW()
RETURNING *;

-- name: FinalizeRegistrationSession :one
UPDATE backend.sessions
SET
    state = 'authenticated',
    user_id = @user_id::INT,
    data = @data::BYTEA,
    version = version + 1
WHERE session_id = @session_id::TEXT
  AND state = 'registration_processing'
  AND version = @expected_version::INT
  AND user_id IS NULL
  AND expires_at >= NOW()
RETURNING *;

-- name: RevokeSession :one
WITH target AS MATERIALIZED (
    SELECT session_id, state, version, user_id
    FROM backend.sessions
    WHERE session_id = @session_id::TEXT
    FOR UPDATE
), revoked AS (
    UPDATE backend.sessions AS sessions
    SET
        state = 'revoked',
        version = sessions.version + 1
    FROM target
    WHERE sessions.session_id = target.session_id
      AND target.state <> 'revoked'
    RETURNING sessions.session_id, sessions.state, sessions.version, sessions.user_id, target.state AS previous_state
)
SELECT session_id, state, version, user_id, previous_state, TRUE AS transitioned
FROM revoked
UNION ALL
SELECT session_id, state, version, user_id, state AS previous_state, FALSE AS transitioned
FROM target
WHERE state = 'revoked'
LIMIT 1;

-- name: RotateSession :one
WITH predecessor AS MATERIALIZED (
    SELECT session_id, state, version, user_id, expires_at
    FROM backend.sessions
    WHERE session_id = @old_session_id::TEXT
    FOR UPDATE
), revoked AS (
    UPDATE backend.sessions AS sessions
    SET
        state = 'revoked',
        version = sessions.version + 1
    FROM predecessor
    WHERE sessions.session_id = predecessor.session_id
      AND predecessor.state = 'authenticated'
      AND predecessor.version = @expected_version::INT
      AND predecessor.user_id = @expected_user_id::INT
      AND predecessor.expires_at >= NOW()
    RETURNING predecessor.user_id
), successor AS (
    INSERT INTO backend.sessions (
        session_id,
        state,
        user_id,
        data,
        expires_at
    )
    SELECT
        @new_session_id::TEXT,
        'authenticated'::backend.session_state,
        revoked.user_id,
        @successor_data::BYTEA,
        NOW() + @successor_ttl::INTERVAL
    FROM revoked
    RETURNING *
)
SELECT * FROM successor;

-- name: DeleteSessionsByID :execrows
DELETE FROM backend.sessions
WHERE session_id = ANY(@session_ids::TEXT[]);

-- name: DeleteExpiredSessions :execrows
DELETE FROM backend.sessions
WHERE expires_at < NOW();
