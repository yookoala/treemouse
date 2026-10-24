//go:build linux

package main

import (
	"time"

	"golang.org/x/sys/unix"
)

// getCreationTime returns the file's birth/creation time on Linux using statx,
// falling back to ModTime if Btime is not supported by the filesystem.
func getCreationTime(path string, fallback time.Time) time.Time {
	var stx unix.Statx_t
	err := unix.Statx(unix.AT_FDCWD, path, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BTIME, &stx)
	if err == nil && (stx.Mask&unix.STATX_BTIME) != 0 && stx.Btime.Sec > 0 {
		return time.Unix(stx.Btime.Sec, int64(stx.Btime.Nsec))
	}
	return fallback
}
