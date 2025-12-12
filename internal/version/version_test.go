package version

import "testing"

func TestApplyBuildInfo_PopulatesDefaults(t *testing.T) {
	origVersion, origRevision, origBuildDate := Version, Revision, BuildDate
	t.Cleanup(func() {
		Version, Revision, BuildDate = origVersion, origRevision, origBuildDate
	})

	Version = "0.5.0-dev"
	Revision = "HEAD"
	BuildDate = ""

	applyBuildInfo("v9.9.9", map[string]string{
		"vcs.revision": "abcdef1234567890",
		"vcs.modified": "true",
		"vcs.time":     "2025-12-12T01:00:00Z",
	})

	if Version != "9.9.9" {
		t.Fatalf("expected Version from main module, got %q", Version)
	}
	if Revision != "abcdef1234567890-dirty" {
		t.Fatalf("expected dirty revision, got %q", Revision)
	}
	if BuildDate != "2025-12-12T01:00:00Z" {
		t.Fatalf("expected BuildDate from vcs.time, got %q", BuildDate)
	}
}

func TestApplyBuildInfo_DoesNotOverrideLdflags(t *testing.T) {
	origVersion, origRevision, origBuildDate := Version, Revision, BuildDate
	t.Cleanup(func() {
		Version, Revision, BuildDate = origVersion, origRevision, origBuildDate
	})

	Version = "1.2.3"
	Revision = "deadbeef"
	BuildDate = "from-ldflags"

	applyBuildInfo("v9.9.9", map[string]string{
		"vcs.revision": "abcdef",
		"vcs.time":     "2025-12-12T01:00:00Z",
	})

	if Version != "1.2.3" || Revision != "deadbeef" || BuildDate != "from-ldflags" {
		t.Fatalf("expected ldflags to win, got Version=%q Revision=%q BuildDate=%q", Version, Revision, BuildDate)
	}
}

