//go:build cgo && sqlite3_cgo

package db

import (
	_ "github.com/mattn/go-sqlite3"
)

const driverID = "mattn/go-sqlite3"
const driverName = "sqlite3"
