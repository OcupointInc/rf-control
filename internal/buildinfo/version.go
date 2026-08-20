// Package buildinfo contains metadata shared by the GUI and CLI entry points.
package buildinfo

// Version is stamped at link time by the release workflow. Tagged builds use
// the tag (for example v1.2.3), rolling builds use latest-<sha>, and local
// builds retain dev unless the builder supplies an override.
var Version = "dev"
