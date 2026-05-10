package db

import (
	"strconv"
	"strings"
)

const twitterSnowflakeEpochMs int64 = 1288834974657

func twitterSnowflakeMillis(id string) int64 {
	id = strings.TrimSpace(id)
	if id == "" {
		return 0
	}
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	ms := (n >> 22) + twitterSnowflakeEpochMs
	if ms <= twitterSnowflakeEpochMs {
		return 0
	}
	return ms
}
