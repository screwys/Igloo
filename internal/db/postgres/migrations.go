// Package postgres embeds the versioned server schema.
package postgres

import "embed"

// Migrations contains the PostgreSQL schema shipped with the server.
//
//go:embed migrations/*.sql
var Migrations embed.FS
