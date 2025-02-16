package acl

import (
	"path/filepath"
	"strings"
)

type aclRule struct {
	rule        *Rule
	node        *aclNode // back reference to the node
	fullPattern string
}

func (r *aclRule) CanAccess(user string, action Action) bool {
	isAdmin := r.rule.Access.Admin.Contains(user) || r.rule.Access.Admin.Contains(Everyone)
	isWriter := r.rule.Access.Write.Contains(user) || r.rule.Access.Write.Contains(Everyone)
	isReader := r.rule.Access.Read.Contains(user) || r.rule.Access.Read.Contains(Everyone)

	switch action {
	case ActionFileWriteACL:
		return isAdmin
	case ActionFileWrite:
		return isAdmin || isWriter
	case ActionFileRead:
		return isAdmin || isWriter || isReader
	default:
		return false
	}
}

func (r *aclRule) WithinLimts(info *FileInfo) bool {
	if r.rule.Limits == nil {
		return true
	}

	if r.rule.Limits.MaxFiles > 0 && info.Size > r.rule.Limits.MaxFileSize {
		return false
	}

	if !r.rule.Limits.AllowDirs && (info.IsDir || strings.Count(info.Path, string(filepath.Separator)) > 0) {
		return false
	}

	if !r.rule.Limits.AllowSymlinks && info.IsSymlink {
		return false
	}

	return true
}
