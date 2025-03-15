package acl

import "path/filepath"

type pCounter uint8

const (
	TerminalRule = true
	PathSep      = string(filepath.Separator)
)
