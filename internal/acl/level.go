package acl

// AccessLevel represents a permission bit flag for different file operations.
type AccessLevel uint8

// Action constants define different types of file permissions
const (
	AccessRead AccessLevel = 1 << iota
	AccessCreate
	AccessWrite
	AccessReadACL
	AccessWriteACL
)

func (a AccessLevel) String() string {
	switch a {
	case AccessRead:
		return "Read"
	case AccessCreate:
		return "Create"
	case AccessWrite:
		return "Write"
	case AccessReadACL:
		return "ReadACL"
	case AccessWriteACL:
		return "WriteACL"
	default:
		return "Unknown"
	}
}
