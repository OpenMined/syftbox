package acl

import "github.com/openmined/syftbox/internal/server/acl"

type ACLCheckRequest struct {
	User  string          `form:"user" binding:"required"`
	Path  string          `form:"path" binding:"required"`
	Size  int64           `form:"size"`
	Level acl.AccessLevel `form:"level" binding:"required"`
}

type ACLCheckResponse struct {
	User  string `json:"user"`
	Path  string `json:"path"`
	Level string `json:"level"`
}
