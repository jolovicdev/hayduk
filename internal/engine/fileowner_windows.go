//go:build windows

package engine

import "os"

// Windows ACLs do not map to a POSIX owner uid; the mode check in
// prepareStagingDir is skipped there too.
func fileOwnerUID(os.FileInfo) int { return -1 }
