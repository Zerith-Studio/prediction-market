// Package db embeds the canonical Postgres schema so store.Bootstrap can apply
// it at boot without depending on the working directory. schema.sql stays the
// single source of truth — edit it, never this file.
package db

import _ "embed"

//go:embed schema.sql
var Schema string
