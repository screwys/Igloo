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

// RefreshToken contains the values needed to sign a token pair.
type RefreshToken struct {
	SessionID   string
	TokenID     string
	IssuedAtMs  int64
	ExpiresAtMs int64
}

// RefreshTokenOverlap allows tabs and retried requests to receive the same pair.
const RefreshTokenOverlap = time.Minute

// RotateRefreshToken consumes and replaces a token in one transaction. Recent
// duplicates receive the current pair; older replays revoke the session.
func (db *DB) RotateRefreshToken(tokenID string, ttl time.Duration) (token RefreshToken, err error) {
	now := time.Now().UnixMilli()
	var outcome error
	err = db.WithWrite(func(tx *sql.Tx) error {
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
		if now > row.ExpiresAtMs {
			outcome = ErrRefreshTokenExpired
			return nil
		}
		if row.ConsumedAtMs.Valid {
			if now-row.ConsumedAtMs.Int64 <= RefreshTokenOverlap.Milliseconds() {
				current, e := q.GetCurrentRefreshToken(context.Background(), row.SessionID)
				if e != nil {
					return e
				}
				token = RefreshToken{row.SessionID, current.TokenID.String, current.IssuedAtMs, current.ExpiresAtMs}
				return nil
			}
			if e := q.RevokeAuthSession(context.Background(), query.RevokeAuthSessionParams{
				RevokeReason: sql.NullString{String: "refresh_replay", Valid: true}, SessionID: sql.NullString{String: row.SessionID, Valid: true},
			}); e != nil {
				return e
			}
			outcome = ErrRefreshTokenConsumed
			return nil
		}
		if e := q.ConsumeRefreshToken(context.Background(), query.ConsumeRefreshTokenParams{
			ConsumedAtMs: sql.NullInt64{Int64: now, Valid: true}, TokenID: sql.NullString{String: tokenID, Valid: true},
		}); e != nil {
			return e
		}
		token = RefreshToken{row.SessionID, NewRandomID(), now, now + ttl.Milliseconds()}
		return q.CreateRefreshToken(context.Background(), query.CreateRefreshTokenParams{
			TokenID: sql.NullString{String: token.TokenID, Valid: true}, SessionID: token.SessionID,
			IssuedAtMs: token.IssuedAtMs, ExpiresAtMs: token.ExpiresAtMs,
		})
	})
	if err != nil {
		return RefreshToken{}, err
	}
	return token, outcome
}

func (db *DB) SetAuthSessionClient(sessionID, clientName string, expiresAtMs int64) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		return query.New(tx).SetAuthSessionClient(context.Background(), query.SetAuthSessionClientParams{
			ClientName: clientName, ExpiresAtMs: expiresAtMs, SessionID: sql.NullString{String: sessionID, Valid: true},
		})
	})
}

type ConnectedSession struct {
	SessionID      string `json:"session_id"`
	ClientName     string `json:"client_name"`
	CreatedAtMs    int64  `json:"created_at_ms"`
	LastActiveAtMs int64  `json:"last_active_at_ms"`
	ExpiresAtMs    int64  `json:"expires_at_ms"`
}

func (db *DB) ListAuthSessions(username string) ([]ConnectedSession, error) {
	rows, err := query.New(db.conn).ListAuthSessions(context.Background(), username)
	if err != nil {
		return nil, err
	}
	result := make([]ConnectedSession, 0, len(rows))
	now := time.Now().UnixMilli()
	for _, row := range rows {
		if row.ExpiresAtMs <= now {
			continue
		}
		result = append(result, ConnectedSession{row.SessionID.String, row.ClientName, row.CreatedAtMs, row.LastActiveAtMs, row.ExpiresAtMs})
	}
	return result, nil
}

func (db *DB) RevokeOwnedAuthSession(sessionID, username string) (bool, error) {
	var count int64
	err := db.WithWrite(func(tx *sql.Tx) error {
		var err error
		count, err = query.New(tx).RevokeOwnedAuthSession(context.Background(), query.RevokeOwnedAuthSessionParams{
			SessionID: sql.NullString{String: sessionID, Valid: true}, Username: username,
		})
		return err
	})
	return count > 0, err
}
