package db

import "fmt"

func feedPrimaryItemPredicate(alias string) string {
	return fmt.Sprintf("COALESCE(%s.is_ghost, 0) = 0", alias)
}
