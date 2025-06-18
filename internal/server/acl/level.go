package acl

// AccessLevel represents a permission bit flag for different file operations.
type AccessLevel uint8

// Action constants define different types of file permissions
const (
	AccessRead AccessLevel = iota + 1
	AccessWrite
	AccessAdmin
)

func (a AccessLevel) String() string {
	switch a {
	case AccessRead:
		return "Read"
	case AccessWrite:
		return "Write"
	case AccessAdmin:
		return "Admin"
	default:
		return "Unknown"
	}
}
