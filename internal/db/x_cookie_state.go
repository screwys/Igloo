package db

import (
	"database/sql"

	"github.com/screwys/igloo/internal/model"
)

// XCookieStates returns the X cookie files that are resting or dropped.
func (db *DB) XCookieStates() ([]model.XCookieState, error) {
	rows, err := db.reader().Query(`SELECT label, session, fail_count, retry_at_ms, dropped, last_error FROM x_cookie_state`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var states []model.XCookieState
	for rows.Next() {
		var state model.XCookieState
		if err := rows.Scan(&state.Label, &state.Session, &state.FailCount, &state.RetryAtMs, &state.Dropped, &state.LastError); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

// SaveXCookieState records why one X cookie file is resting.
func (db *DB) SaveXCookieState(state model.XCookieState) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO x_cookie_state (label, session, fail_count, retry_at_ms, dropped, last_error)
			VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (label) DO UPDATE SET session=excluded.session, fail_count=excluded.fail_count,
				retry_at_ms=excluded.retry_at_ms, dropped=excluded.dropped, last_error=excluded.last_error`,
			state.Label, state.Session, state.FailCount, state.RetryAtMs, state.Dropped, state.LastError)
		return err
	})
}

// DeleteXCookieState forgets a cookie file's failures once it works or is replaced.
func (db *DB) DeleteXCookieState(label string) error {
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`DELETE FROM x_cookie_state WHERE label=$1`, label)
		return err
	})
}
