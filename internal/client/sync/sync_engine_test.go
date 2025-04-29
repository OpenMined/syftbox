package sync

// func TestSyncEngineFullSync(t *testing.T) {
// 	dummyDatasite, err := datasite.NewLocalDatasite("~/SyftBox", "yash@openmined.org")
// 	assert.NoError(t, err)

// 	sdk, err := syftsdk.New("https://syftboxdev.openmined.org")
// 	sdk.Login("yash@openmined.org")
// 	assert.NoError(t, err)

// 	ignore := NewSyncIgnore(dummyDatasite.DatasitesDir, dummyDatasite.DatasitesDir)
// 	watcher := NewFileWatcher(dummyDatasite.DatasitesDir)

// 	syncEngine := NewSyncEngine(dummyDatasite, sdk, ignore, watcher)
// 	err = syncEngine.RunSync(context.Background())
// 	assert.NoError(t, err)
// }

// func TestSyncEngineReconcile(t *testing.T) {
// 	syncEngine := &SyncEngine{}

// 	journal := map[string]*FileMetadata{
// 		"/test/file1": {
// 			Path:         "/test/file1",
// 			ETag:         "123",
// 			Version:      "1",
// 			Size:         100,
// 			LastModified: time.Now(),
// 		},
// 		"/test/file4": {
// 			Path:         "/test/file4",
// 			ETag:         "sadlajklsd",
// 			Version:      "1",
// 			Size:         1012310,
// 			LastModified: time.Now(),
// 		},
// 	}

// 	localState := map[string]*FileMetadata{
// 		"/test/file1": {
// 			Path:         "/test/file1",
// 			ETag:         "123",
// 			Version:      "1",
// 			Size:         100,
// 			LastModified: time.Now(),
// 		},
// 		"/test/file3": {
// 			Path:         "/test/file3",
// 			ETag:         "ashldk",
// 			Version:      "1",
// 			Size:         10,
// 			LastModified: time.Now(),
// 		},
// 	}

// 	remoteState := map[string]*FileMetadata{
// 		"/test/file1": {
// 			Path:         "/test/file1",
// 			ETag:         "defg",
// 			Version:      "2", // new version
// 			Size:         100,
// 			LastModified: time.Now(),
// 		},
// 		"/test/file4": {
// 			Path:         "/test/file4",
// 			ETag:         "sadlajklsd",
// 			Version:      "1",
// 			Size:         1012310,
// 			LastModified: time.Now(),
// 		},
// 	}

// 	// this should download file1, delete file4, and write file3
// 	result := syncEngine.reconcile(localState, remoteState, journal)
// 	assert.Equal(t, 1, len(result.RemoteWrites))
// 	assert.Equal(t, 1, len(result.LocalWrites))
// 	assert.Equal(t, 1, len(result.RemoteDeletes))
// 	assert.Equal(t, "/test/file3", result.RemoteWrites["/test/file3"].Path)
// 	assert.Equal(t, "/test/file1", result.LocalWrites["/test/file1"].Path)
// 	assert.Equal(t, "/test/file4", result.RemoteDeletes["/test/file4"].Path)
// }
