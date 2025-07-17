package sync

type OpType string

const (
	OpWriteRemote  OpType = "WriteRemote"
	OpWriteLocal   OpType = "WriteLocal"
	OpDeleteRemote OpType = "DeleteRemote"
	OpDeleteLocal  OpType = "DeleteLocal"
	OpConflict     OpType = "Conflict"
	OpError        OpType = "Error"
	OpSkipped      OpType = "Skipped"
)

type SyncOperation struct {
	Type       OpType
	RelPath    SyncPath
	Local      *FileMetadata
	Remote     *FileMetadata
	LastSynced *FileMetadata
}
