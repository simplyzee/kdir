package cli

import (
	"runtime/debug"
	"testing"
)

func TestBuildInfo(t *testing.T) {
	setting := func(kv ...string) []debug.BuildSetting {
		var s []debug.BuildSetting
		for i := 0; i < len(kv); i += 2 {
			s = append(s, debug.BuildSetting{Key: kv[i], Value: kv[i+1]})
		}
		return s
	}
	tests := []struct {
		name                  string
		ldflagsVersion        string
		info                  *debug.BuildInfo
		ok                    bool
		version, commit, date string
	}{
		{
			name:           "release build keeps ldflags values",
			ldflagsVersion: "1.0.2",
			info:           &debug.BuildInfo{Main: debug.Module{Version: "v9.9.9"}},
			ok:             true,
			version:        "1.0.2", commit: "abc1234", date: "2026-11-26T23:00:00Z",
		},
		{
			name:    "go install at a tag",
			info:    &debug.BuildInfo{Main: debug.Module{Version: "v1.0.2"}},
			ok:      true,
			version: "1.0.2", commit: "none", date: "unknown",
		},
		{
			name: "build from a dirty checkout",
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: setting(
					"vcs.revision", "cba2f20c3924f32d7c0333cbe517a71e241238c4",
					"vcs.time", "2026-11-26T23:09:49Z",
					"vcs.modified", "true",
				),
			},
			ok:      true,
			version: "dev", commit: "cba2f20-dirty", date: "2026-11-26T23:09:49Z",
		},
		{
			name:    "no build info",
			ok:      false,
			version: "dev", commit: "none", date: "unknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldV, oldC, oldD := Version, Commit, Date
			t.Cleanup(func() { Version, Commit, Date = oldV, oldC, oldD })
			Version, Commit, Date = "dev", "none", "unknown"
			if tt.ldflagsVersion != "" {
				Version, Commit, Date = tt.ldflagsVersion, "abc1234", "2026-11-26T23:00:00Z"
			}
			v, c, d := buildInfo(func() (*debug.BuildInfo, bool) { return tt.info, tt.ok })
			if v != tt.version || c != tt.commit || d != tt.date {
				t.Errorf("got (%q, %q, %q), want (%q, %q, %q)", v, c, d, tt.version, tt.commit, tt.date)
			}
		})
	}
}
