package acl

type User struct {
	ID string
}

type File struct {
	Path      string
	IsDir     bool
	IsSymlink bool
	Size      int64
}
