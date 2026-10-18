-- name: CreateAuthSession :exec
INSERT INTO auth_sessions(session_id, username, created_at_ms, last_active_at_ms, revoked)
VALUES ($1, $2, $3, $4, 0);

-- name: GetAuthSession :one
SELECT session_id, username, created_at_ms, last_active_at_ms, revoked, revoke_reason
FROM auth_sessions WHERE session_id=$1;

-- name: TouchAuthSession :exec
UPDATE auth_sessions SET last_active_at_ms=$1 WHERE session_id=$2 AND last_active_at_ms<=$3;

-- name: RevokeAuthSession :exec
UPDATE auth_sessions SET revoked=1, revoke_reason=$1 WHERE session_id=$2;

-- name: RevokeAuthSessionsForUser :exec
UPDATE auth_sessions SET revoked=1, revoke_reason=$1 WHERE username=$2 AND revoked=0;

-- name: CreateRefreshToken :exec
INSERT INTO auth_refresh_tokens(token_id,session_id,issued_at_ms,expires_at_ms,consumed_at_ms)
VALUES ($1,$2,$3,$4,NULL);

-- name: GetRefreshTokenForUpdate :one
SELECT rt.session_id,rt.expires_at_ms,rt.consumed_at_ms,s.revoked
FROM auth_refresh_tokens rt JOIN auth_sessions s ON s.session_id=rt.session_id
WHERE rt.token_id=$1 FOR UPDATE OF rt,s;

-- name: GetCurrentRefreshToken :one
SELECT token_id, issued_at_ms, expires_at_ms FROM auth_refresh_tokens
WHERE session_id=$1 AND consumed_at_ms IS NULL ORDER BY issued_at_ms DESC LIMIT 1;

-- name: SetAuthSessionClient :exec
UPDATE auth_sessions SET client_name=$1, expires_at_ms=$2 WHERE session_id=$3;

-- name: ListAuthSessions :many
SELECT s.session_id, s.client_name, s.created_at_ms, s.last_active_at_ms,
       CASE WHEN s.expires_at_ms > 0 THEN s.expires_at_ms
            ELSE COALESCE((SELECT MAX(rt.expires_at_ms) FROM auth_refresh_tokens rt WHERE rt.session_id=s.session_id), 0)
       END::bigint AS expires_at_ms
FROM auth_sessions s
WHERE s.username=$1 AND s.revoked=0
ORDER BY s.last_active_at_ms DESC, s.created_at_ms DESC;

-- name: RevokeOwnedAuthSession :execrows
UPDATE auth_sessions SET revoked=1, revoke_reason='user_revoked' WHERE session_id=$1 AND username=$2;

-- name: ConsumeRefreshToken :exec
UPDATE auth_refresh_tokens SET consumed_at_ms=$1 WHERE token_id=$2;
