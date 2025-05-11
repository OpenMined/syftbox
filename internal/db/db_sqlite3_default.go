//go:build !sqlite3_cgo

package db

import (
	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
)

const driverID = "ncruces/go-sqlite3"
const driverName = "sqlite3"
