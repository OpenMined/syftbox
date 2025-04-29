package sync3

type OpType uint8

var opTypeNames = []string{
	"WriteRemote",
	"WriteLocal",
	"DeleteRemote",
	"DeleteLocal",
	"RenameRemote",
	"RenameLocal",
	"Conflict",
	"Error",
}

const (
	OpWriteRemote OpType = iota
	OpWriteLocal
	OpDeleteRemote
	OpDeleteLocal
	OpRenameRemote
	OpRenameLocal
	OpConflict
	OpError
)

type SyncOperation struct {
	Op         OpType
	RelPath    string // Datasite relative path
	Local      *FileMetadata
	Remote     *FileMetadata
	LastSynced *FileMetadata
}

func (op OpType) String() string {
	return opTypeNames[op]
}
