package cli

import (
	"runtime/debug"
	"strings"
)

// buildInfo returns the version, commit and date to report. Release builds
// set them with -ldflags. Other builds, such as `go install …@v1.2.3`, fall
// back to what the Go toolchain recorded in the binary: the module version
// and, when built from a checkout, the VCS revision and time.
func buildInfo(read func() (*debug.BuildInfo, bool)) (version, commit, date string) {
	version, commit, date = Version, Commit, Date
	if version != "dev" {
		return version, commit, date
	}
	info, ok := read()
	if !ok || info == nil {
		return version, commit, date
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		version = strings.TrimPrefix(v, "v")
	}
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			commit = s.Value
			if len(commit) > 7 {
				commit = commit[:7]
			}
		case "vcs.time":
			date = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if modified && commit != "none" {
		commit += "-dirty"
	}
	return version, commit, date
}
