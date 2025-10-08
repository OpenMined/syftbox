package accesslog

import (
	"github.com/openmined/syftbox/internal/server/acl"
)

// sanitizeUsername converts a username to a filesystem-safe string
func sanitizeUsername(user string) string {
	result := make([]byte, 0, len(user))
	for i := 0; i < len(user); i++ {
		c := user[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '@' || c == '.' || c == '-' || c == '_' {
			result = append(result, c)
		} else {
			result = append(result, '_')
		}
	}
	return string(result)
}

// ConvertACLLevel converts an ACL access level to an AccessType
func ConvertACLLevel(level acl.AccessLevel) AccessType {
	switch level {
	case acl.AccessAdmin:
		return AccessTypeAdmin
	case acl.AccessWrite:
		return AccessTypeWrite
	case acl.AccessRead:
		return AccessTypeRead
	default:
		return AccessTypeDeny
	}
}
