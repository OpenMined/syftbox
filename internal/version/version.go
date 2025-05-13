package version

import (
	"fmt"
	"runtime"
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
	if BuildDate == "" {
		BuildDate = time.Now().UTC().Format(time.RFC3339)
	}
}
