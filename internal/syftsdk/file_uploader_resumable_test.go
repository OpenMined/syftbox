package syftsdk

import "testing"

func TestResumableUploader_EmitAdvancedProgress(t *testing.T) {
	var got UploadProgress
	u := &resumableUploader{
		params: &UploadParams{
			AdvancedCallback: func(p UploadProgress) {
				got = p
			},
		},
		session: &uploadSession{
			Size:      100,
			PartSize:  10,
			PartCount: 10,
			Completed: map[int]string{3: "e3", 1: "e1", 2: "e2"},
		},
	}

	u.emitAdvancedProgress(30)

	if got.UploadedBytes != 30 || got.TotalBytes != 100 {
		t.Fatalf("unexpected bytes: %+v", got)
	}
	if got.PartSize != 10 || got.PartCount != 10 {
		t.Fatalf("unexpected part metadata: %+v", got)
	}
	wantParts := []int{1, 2, 3}
	if len(got.CompletedParts) != len(wantParts) {
		t.Fatalf("unexpected completed parts: %+v", got.CompletedParts)
	}
	for i := range wantParts {
		if got.CompletedParts[i] != wantParts[i] {
			t.Fatalf("completed parts not sorted: %+v", got.CompletedParts)
		}
	}
}

