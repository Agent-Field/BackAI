// SPDX-License-Identifier: Apache-2.0

// Package buildinfo exposes the CLI's own version to packages that need to
// pin published artifacts (release images) to the binary that wrote them.
// main sets Version from its -ldflags "-X main.version=..." value at startup.
package buildinfo

// Version is the CLI version, or a development placeholder when the binary
// was built without the release linker flag.
var Version = "0.0.1"
