package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

var (
	// Name of the application
	AppName = "SyftBox"

	// Version of the application
	Version = "0.5.0-dev"

	// Git commit hash of the application
	Revision = "HEAD"

	// Build date of the application
	BuildDate = ""
)

func applyBuildInfo(mainVersion string, settings map[string]string) {
	// Prefer module version when set by release builds.
	if Version == "0.5.0-dev" || Version == "" {
		if v := mainVersion; v != "" && v != "(devel)" {
			Version = strings.TrimPrefix(v, "v")
		}
	}

	// Prefer VCS revision for local/dev builds.
	if Revision == "HEAD" || Revision == "" {
		if r := settings["vcs.revision"]; r != "" {
			if settings["vcs.modified"] == "true" {
				r += "-dirty"
			}
			Revision = r
		}
	}

	if BuildDate == "" {
		if t := settings["vcs.time"]; t != "" {
			BuildDate = t
		}
	}
}

// resolveFromBuildInfo populates Version/Revision/BuildDate from Go build metadata
// when ldflags didn't provide real values.
func resolveFromBuildInfo() {
	info, ok := debug.ReadBuildInfo()
	if !ok || info == nil {
		return
	}

	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}

	applyBuildInfo(info.Main.Version, settings)
}

// Short returns a concise version string - `0.1.0 (5e23a4)`
func Short() string {
	return fmt.Sprintf("%s (%s)", Version, Revision)
}

// ShortWithApp returns a concise version string with the application name - `SyftBox 0.1.0 (5e23a4)`
func ShortWithApp() string {
	return fmt.Sprintf("%s %s", AppName, Short())
}

// Returns a detailed version string - `0.1.0 (5e23a4; go1.16.3; darwin/amd64)`
func Detailed() string {
	return fmt.Sprintf("%s (%s; %s; %s/%s; %s)", Version, Revision, runtime.Version(), runtime.GOOS, runtime.GOARCH, BuildDate)
}

// Returns a detailed version string with the application name - `SyftBox 0.1.0 (5e23a4; go1.16.3; darwin/amd64)`
func DetailedWithApp() string {
	return fmt.Sprintf("%s %s", AppName, Detailed())
}

func init() {
	resolveFromBuildInfo()
	if BuildDate == "" {
		BuildDate = time.Now().UTC().Format(time.RFC3339)
	}
}
