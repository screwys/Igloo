package db

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/screwys/igloo/internal/home"
)

const HomeLayoutsSchema = `CREATE TABLE IF NOT EXISTS home_layouts (
	username TEXT PRIMARY KEY,
	layout_json TEXT NOT NULL,
	updated_at_ms INTEGER NOT NULL
) WITHOUT ROWID`

func (db *DB) GetHomeLayout(username string) (home.Layout, error) {
	var raw string
	err := db.reader().QueryRow(`SELECT layout_json FROM home_layouts WHERE username = ?`, username).Scan(&raw)
	if err == sql.ErrNoRows {
		return home.DefaultLayout(), nil
	}
	if err != nil {
		return home.Layout{}, err
	}
	var layout home.Layout
	if err := json.Unmarshal([]byte(raw), &layout); err != nil {
		return home.Layout{}, err
	}
	return layout, nil
}

func (db *DB) SetHomeLayout(username string, layout home.Layout) error {
	raw, err := json.Marshal(layout)
	if err != nil {
		return err
	}
	return db.WithWrite(func(tx *sql.Tx) error {
		_, err := tx.Exec(`INSERT INTO home_layouts (username, layout_json, updated_at_ms) VALUES (?, ?, ?)
			ON CONFLICT(username) DO UPDATE SET layout_json = excluded.layout_json, updated_at_ms = excluded.updated_at_ms`, username, string(raw), time.Now().UnixMilli())
		return err
	})
}
