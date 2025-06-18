package acl

// AccessLevel represents a permission bit flag for different file operations.
type AccessLevel uint8

// Action constants define different types of file permissions
const (
	AccessRead AccessLevel = 1 << iota
	AccessCreate
	AccessWrite
	AccessAdmin
)

func (a AccessLevel) String() string {
	if a == 0 {
		return "None"
	}

	var parts []string

	if (a & AccessRead) == AccessRead {
		parts = append(parts, "Read")
	}
	if (a & AccessCreate) == AccessCreate {
		parts = append(parts, "Create")
	}
	if (a & AccessWrite) == AccessWrite {
		parts = append(parts, "Write")
	}
	if (a & AccessAdmin) == AccessAdmin {
		parts = append(parts, "Admin")
	}

	if len(parts) == 0 {
		return "Unknown"
	}

	if len(parts) == 1 {
		return parts[0]
	}

	// For multiple permissions, join with "+"
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += "+" + parts[i]
	}
	return result
}
