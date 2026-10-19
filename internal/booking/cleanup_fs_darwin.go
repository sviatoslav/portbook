package booking

import "syscall"

func localCleanupFilesystem(path string) bool {
	var fs syscall.Statfs_t
	if syscall.Statfs(path, &fs) != nil {
		return false
	}
	var name []byte
	for _, c := range fs.Fstypename {
		if c == 0 {
			break
		}
		name = append(name, byte(c))
	}
	return string(name) == "apfs" || string(name) == "hfs"
}
