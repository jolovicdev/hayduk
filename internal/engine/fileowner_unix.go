//go:build !windows

package engine

import (
	"os"
	"syscall"
)

func fileOwnerUID(info os.FileInfo) int {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return int(st.Uid)
	}
	return -1
}
