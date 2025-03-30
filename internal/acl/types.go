package acl

type User struct {
	ID      string
	IsOwner bool
}

type File struct {
	Path      string
	IsDir     bool
	IsSymlink bool
	Size      int64
}
