package acl

type User struct {
	ID string
}

type File struct {
	IsDir     bool
	IsSymlink bool
	Size      int64
}

type ACLRequest struct {
	Path  string
	Level AccessLevel
	User  *User
	File  *File
}

func NewRequest(path string, user *User, level AccessLevel) *ACLRequest {
	return &ACLRequest{
		Path:  ACLNormPath(path),
		Level: level,
		User:  user,
	}
}

func NewRequestWithFile(path string, user *User, level AccessLevel, file *File) *ACLRequest {
	req := NewRequest(path, user, level)
	req.File = file
	return req
}
