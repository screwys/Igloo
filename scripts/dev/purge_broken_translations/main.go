// purge_broken_translations deletes cached translations whose source text
// contained @mentions, #hashtags, or URLs. Those were stripped entirely by
// the previous stripForTranslate implementation, producing translations with
// dropped content. Re-translation (now using placeholder protection) will
// preserve them.
//
// Usage: go run scripts/dev/purge_broken_translations/main.go [-dry-run]
package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"

	"github.com/screwys/igloo/internal/config"
	"github.com/screwys/igloo/internal/db"
)

var dryRun = flag.Bool("dry-run", false, "report how many rows would be purged without deleting")

const countSQL = `
SELECT COUNT(*) FROM translations
WHERE (field = 'body' AND tweet_id IN (
    SELECT tweet_id FROM feed_items
    WHERE body_text LIKE '%@%' ESCAPE '' OR body_text LIKE '%#%' ESCAPE '' OR LOWER(body_text COLLATE "C") LIKE '%http%' ESCAPE ''
))
OR (field = 'quote' AND tweet_id IN (
    SELECT tweet_id FROM feed_items
    WHERE quote_body_text LIKE '%@%' ESCAPE '' OR quote_body_text LIKE '%#%' ESCAPE '' OR LOWER(quote_body_text COLLATE "C") LIKE '%http%' ESCAPE ''
))
`

const deleteSQL = `
DELETE FROM translations
WHERE (field = 'body' AND tweet_id IN (
    SELECT tweet_id FROM feed_items
    WHERE body_text LIKE '%@%' ESCAPE '' OR body_text LIKE '%#%' ESCAPE '' OR LOWER(body_text COLLATE "C") LIKE '%http%' ESCAPE ''
))
OR (field = 'quote' AND tweet_id IN (
    SELECT tweet_id FROM feed_items
    WHERE quote_body_text LIKE '%@%' ESCAPE '' OR quote_body_text LIKE '%#%' ESCAPE '' OR LOWER(quote_body_text COLLATE "C") LIKE '%http%' ESCAPE ''
))
`

func main() {
	flag.Parse()

	cfg := config.Load()
	if cfg.ConfigError != nil {
		log.Fatal(cfg.ConfigError)
	}
	var store *db.DB
	var err error
	if *dryRun {
		store, err = db.OpenReadOnlyLayout(cfg.Storage)
	} else {
		store, err = db.OpenExisting(cfg.Storage)
	}
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	var totalTranslations int
	if err := store.QueryRow("SELECT COUNT(*) FROM translations").Scan(&totalTranslations); err != nil {
		log.Fatalf("count total: %v", err)
	}

	var affected int
	if err := store.QueryRow(countSQL).Scan(&affected); err != nil {
		log.Fatalf("count affected: %v", err)
	}

	fmt.Printf("translations total: %d\naffected (source had @/#/http): %d\n", totalTranslations, affected)

	if *dryRun {
		fmt.Println("dry-run: no deletions")
		return
	}
	if affected == 0 {
		fmt.Println("nothing to purge")
		return
	}

	var deleted int64
	err = store.WithWrite(func(tx *sql.Tx) error {
		res, err := tx.Exec(deleteSQL)
		if err != nil {
			return err
		}
		deleted, err = res.RowsAffected()
		return err
	})
	if err != nil {
		log.Fatalf("delete: %v", err)
	}
	fmt.Printf("purged %d translations; they will re-populate on next view\n", deleted)
}
