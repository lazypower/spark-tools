// Package version holds build-time version info injected via ldflags.
//
//	go build -ldflags "-X github.com/lazypower/spark-tools/internal/version.Version=v0.2.3"
package version

var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)
