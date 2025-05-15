package utils

import (
	"github.com/denisbrodbeck/machineid"
)

var (
	HWID, _ = machineid.ProtectedID("syftbox")
)
