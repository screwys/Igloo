package db

import "fmt"

func feedUnseenPredicate(alias string) string {
	// Keep seen checks correlated so each candidate probes indexed rows.
	return fmt.Sprintf(`NOT EXISTS (
			SELECT 1
			FROM feed_seen fs
			WHERE fs.tweet_id = %[1]s.tweet_id
			OFFSET 0
		)
		AND (
			NULLIF(TRIM(COALESCE(%[1]s.content_hash, '')), '') IS NULL
			OR NOT EXISTS (
				SELECT 1
				FROM feed_items seen_fi
				JOIN feed_seen fs ON fs.tweet_id = seen_fi.tweet_id
				WHERE seen_fi.content_hash = %[1]s.content_hash
				  AND seen_fi.content_hash IS NOT NULL
				  AND seen_fi.content_hash != ''
				OFFSET 0
			)
		)`, alias)
}
