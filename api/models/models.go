// Package models is the database's tables, a file each: the struct, and its
// queries as methods. Each query takes a DB, so it runs the same on the
// database or inside a transaction.
package models

import tdb "github.com/Grandbusta/tinio/internal/db"

// DB is the database or a transaction.
type DB = tdb.Querier
