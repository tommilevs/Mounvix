// Package version holds the build version injected via -ldflags.
package version

// Version is overridden at build time: -X .../version.Version=vX.Y.Z.
var Version = "dev"
