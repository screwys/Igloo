package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/screwys/igloo/internal/db/query"
)

// #16 — auth session + refresh token storage.
// One auth_sessions row per login. Zero-or-more auth_refresh_tokens rows
// (only the most recently issued one is unconsumed at any time). Replay
// detection: a second consume of an already-consumed refresh token ID
// marks the whole session revoked, killing every paired access token
// on the next probe.

var ErrSessionRevoked = errors.New("session revoked")
var ErrRefreshTokenExpired = errors.New("refresh token expired")
var ErrRefreshTokenConsumed = errors.New("refresh token already consumed")
var ErrRefreshTokenUnknown = errors.New("refresh token unknown")

const AuthSessionActivityInterval = time.Minute

// AuthSession is the row shape returned from lookup calls.
type AuthSession struct {
	SessionID      string
	Username       string
	CreatedAtMs    int64
	LastActiveAtMs int64
	Revoked        bool
	RevokeReason   string
}

// NewRandomID returns a 128-bit hex-encoded random identifier for use as
// a session_id or refresh token_id.
func NewRandomID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("rand.Read: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// CreateAuthSession opens a new session for username. Returns the
// session_id. Caller pairs this with CreateRefreshToken and signs the
// access + refresh token pair.
func (db *DB) CreateAuthSession(username string) (string, error) {
	sessionID := NewRandomID()
	now := time.Now().UnixMilli()
	err := db.WithWrite(func(tx *sql.Tx) error {
		return query.New(tx).CreateAuthSession(context.Background(), query.CreateAuthSessionParams{
			SessionID: sql.NullString{String: sessionID, Valid: true}, Username: username,
			CreatedAtMs: now, LastActiveAtMs: now,
		})
	})
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

// GetAuthSession returns the session row, or sql.ErrNoRows if missing.
func (db *DB) GetAuthSession(sessionID string) (*AuthSession, error) {
	row, err := query.New(db.conn).GetAuthSession(context.Background(), sql.NullString{String: sessionID, Valid: true})
	if err != nil {
		return nil, err
	}
	return &AuthSession{SessionID: row.SessionID.String, Username: row.Username,
		CreatedAtMs: row.CreatedAtMs, LastActiveAtMs: row.LastActiveAtMs,
		Revoked: row.Revoked != 0, RevokeReason: row.RevokeReason.String}, nil
}

// TouchAuthSession updates activity at most once per interval. Callers use the
// authenticated session's timestamp to avoid scheduling writes before it is due.
func (db *DB) TouchAuthSession(sessionID string) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		nowMs := time.Now().UnixMilli()
		return query.New(tx).TouchAuthSession(context.Background(), query.TouchAuthSessionParams{
			LastActiveAtMs: nowMs, SessionID: sql.NullString{String: sessionID, Valid: true},
			LastActiveAtMs_2: nowMs - AuthSessionActivityInterval.Milliseconds(),
		})
	})
}

// RevokeAuthSession marks a session revoked. Idempotent.
func (db *DB) RevokeAuthSession(sessionID, reason string) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		return query.New(tx).RevokeAuthSession(context.Background(), query.RevokeAuthSessionParams{
			RevokeReason: sql.NullString{String: reason, Valid: true}, SessionID: sql.NullString{String: sessionID, Valid: true},
		})
	})
}

// RevokeAuthSessionsForUser revokes every session belonging to a user.
// Used by account-delete.
func (db *DB) RevokeAuthSessionsForUser(username, reason string) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		return query.New(tx).RevokeAuthSessionsForUser(context.Background(), query.RevokeAuthSessionsForUserParams{
			RevokeReason: sql.NullString{String: reason, Valid: true}, Username: username,
		})
	})
}

// CreateRefreshToken issues a fresh refresh token for a session. Called
// at login and after every successful rotation.
func (db *DB) CreateRefreshToken(sessionID string, ttl time.Duration) (tokenID string, issuedAtMs int64, expiresAtMs int64, err error) {
	tokenID = NewRandomID()
	issuedAtMs = time.Now().UnixMilli()
	expiresAtMs = issuedAtMs + ttl.Milliseconds()
	err = db.WithWrite(func(tx *sql.Tx) error {
		return query.New(tx).CreateRefreshToken(context.Background(), query.CreateRefreshTokenParams{
			TokenID: sql.NullString{String: tokenID, Valid: true}, SessionID: sessionID, IssuedAtMs: issuedAtMs, ExpiresAtMs: expiresAtMs,
		})
	})
	return
}

// ConsumeRefreshToken atomically checks-and-marks a refresh token as used.
// Returns (session_id, nil) on successful first-use.
// Returns ErrRefreshTokenUnknown if the token ID has no row.
// Returns ErrRefreshTokenExpired when expires_at_ms has passed.
// Returns ErrRefreshTokenConsumed (AND revokes the session) on replay.
// Returns ErrSessionRevoked if the session was already revoked.
func (db *DB) ConsumeRefreshToken(tokenID string) (sessionID string, err error) {
	now := time.Now().UnixMilli()
	var outcome error
	txErr := db.WithWrite(func(tx *sql.Tx) error {
		q := query.New(tx)
		row, qErr := q.GetRefreshTokenForUpdate(context.Background(), sql.NullString{String: tokenID, Valid: true})
		if errors.Is(qErr, sql.ErrNoRows) {
			outcome = ErrRefreshTokenUnknown
			return nil
		}
		if qErr != nil {
			return qErr
		}
		if row.Revoked != 0 {
			outcome = ErrSessionRevoked
			return nil
		}
		if row.ConsumedAtMs.Valid {
			// Replay: revoke the whole session within this tx so the
			// revoke commits alongside the outcome classification.
			if e := q.RevokeAuthSession(context.Background(), query.RevokeAuthSessionParams{
				RevokeReason: sql.NullString{String: "refresh_replay", Valid: true}, SessionID: sql.NullString{String: row.SessionID, Valid: true},
			}); e != nil {
				return e
			}
			outcome = ErrRefreshTokenConsumed
			return nil
		}
		if now > row.ExpiresAtMs {
			outcome = ErrRefreshTokenExpired
			return nil
		}
		if e := q.ConsumeRefreshToken(context.Background(), query.ConsumeRefreshTokenParams{
			ConsumedAtMs: sql.NullInt64{Int64: now, Valid: true}, TokenID: sql.NullString{String: tokenID, Valid: true},
		}); e != nil {
			return e
		}
		sessionID = row.SessionID
		return nil
	})
	if txErr != nil {
		return "", txErr
	}
	return sessionID, outcome
}
