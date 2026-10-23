package main

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// make build sets these values through the linker. Direct go builds retain
// useful defaults and still report Go's embedded VCS metadata when available.
var (
	version   = "dev"
	buildTime = "unknown"
)

func versionInfo() string {
	modified := "unknown"
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.modified":
				modified = setting.Value
			}
		}
	}
	return fmt.Sprintf("filegate %s\nmodified: %s\nbuilt: %s\ngo: %s\nplatform: %s/%s",
		version, modified, buildTime, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
