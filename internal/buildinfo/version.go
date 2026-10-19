// Package buildinfo exposes deterministic build metadata supplied by release builds.
package buildinfo

// Version and Commit are set by GoReleaser with -ldflags. Checkout builds use
// explicit fallback values; no clock or local state is consulted at runtime.
var (
	Version = "dev"
	Commit  = "unknown"
)

func String() string { return "portbook " + Version + " (commit " + Commit + ")" }
