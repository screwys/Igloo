package db

import (
	"database/sql"
	"fmt"
)

func repairAndroidMomentsCursorHeads(tx *sql.Tx) error {
	if _, err := tx.Exec(`
		DELETE FROM android_sync_heads
		WHERE owner_kind = 'moments_cursor' AND owner_id = 'stories'
	`); err != nil {
		return fmt.Errorf("remove Android-local stories cursor head: %w", err)
	}

	for _, scope := range []string{"all", "following"} {
		var missing bool
		if err := tx.QueryRow(`
			SELECT EXISTS(
				SELECT 1
				FROM moments_cursors cursor
				WHERE cursor.scope = ?
				  AND NOT EXISTS (
					SELECT 1
					FROM android_sync_heads head
					WHERE head.owner_kind = 'moments_cursor'
					  AND head.owner_id = cursor.scope
				  )
			)
		`, scope).Scan(&missing); err != nil {
			return fmt.Errorf("check Android moments cursor head %s: %w", scope, err)
		}
		if !missing {
			continue
		}
		if _, err := tx.Exec(`
			UPDATE android_sync_clock
			SET revision = revision + 1
			WHERE id = 1
		`); err != nil {
			return fmt.Errorf("advance Android sync clock for moments cursor %s: %w", scope, err)
		}
		result, err := tx.Exec(`
			INSERT INTO android_sync_heads (owner_kind, owner_id, revision)
			SELECT 'moments_cursor', ?, revision
			FROM android_sync_clock
			WHERE id = 1
		`, scope)
		if err != nil {
			return fmt.Errorf("seed Android moments cursor head %s: %w", scope, err)
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("count seeded Android moments cursor head %s: %w", scope, err)
		}
		if inserted != 1 {
			return fmt.Errorf("seed Android moments cursor head %s: sync clock is missing", scope)
		}
	}
	return nil
}
