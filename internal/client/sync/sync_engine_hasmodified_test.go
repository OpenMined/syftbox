package sync

import (
	"testing"

	"github.com/openmined/syftbox/internal/client/workspace"
)

func TestHasModified_MixedMultipartETags_NonOwnerSameSize_NotModified(t *testing.T) {
	se := &SyncEngine{workspace: &workspace.Workspace{Owner: "client2@sandbox.local"}}
	local := &FileMetadata{
		Path: "client1@sandbox.local/public/a.bin",
		Size: 100,
		ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	remote := &FileMetadata{
		Path: "client1@sandbox.local/public/a.bin",
		Size: 100,
		ETag: "\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-4\"",
	}
	if se.hasModified(local, remote) {
		t.Fatal("expected mixed multipart etags on non-owner path to be treated as unmodified when sizes match")
	}
}

func TestHasModified_MixedMultipartETags_OwnerSameSize_Modified(t *testing.T) {
	se := &SyncEngine{workspace: &workspace.Workspace{Owner: "client1@sandbox.local"}}
	local := &FileMetadata{
		Path: "client1@sandbox.local/public/a.bin",
		Size: 100,
		ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	remote := &FileMetadata{
		Path: "client1@sandbox.local/public/a.bin",
		Size: 100,
		ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-4",
	}
	if !se.hasModified(local, remote) {
		t.Fatal("expected mixed multipart etags on owner path to be treated as modified")
	}
}

func TestHasModified_DifferentPlainETags_Modified(t *testing.T) {
	se := &SyncEngine{workspace: &workspace.Workspace{Owner: "client1@sandbox.local"}}
	f1 := &FileMetadata{Path: "client1@sandbox.local/public/a.bin", Size: 100, ETag: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
	f2 := &FileMetadata{Path: "client1@sandbox.local/public/a.bin", Size: 100, ETag: "cccccccccccccccccccccccccccccccc"}
	if !se.hasModified(f1, f2) {
		t.Fatal("expected differing plain etags to be treated as modified")
	}
}

func TestHasModified_MixedMultipartETags_DifferentSize_Modified(t *testing.T) {
	se := &SyncEngine{workspace: &workspace.Workspace{Owner: "client2@sandbox.local"}}
	local := &FileMetadata{
		Path: "client1@sandbox.local/public/a.bin",
		Size: 100,
		ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	remote := &FileMetadata{
		Path: "client1@sandbox.local/public/a.bin",
		Size: 200,
		ETag: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-4",
	}
	if !se.hasModified(local, remote) {
		t.Fatal("expected mixed multipart etags with different size to be treated as modified")
	}
}

