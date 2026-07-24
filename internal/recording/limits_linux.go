//go:build linux

package recording

import "syscall"

func init() {
	freeBytes = freeBytesLinux
}

func freeBytesLinux(path string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}
