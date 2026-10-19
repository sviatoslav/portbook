package booking

import "syscall"

func localCleanupFilesystem(path string) bool {
	var fs syscall.Statfs_t
	if syscall.Statfs(path, &fs) != nil {
		return false
	}
	// Known local filesystems only: ext, XFS, Btrfs, tmpfs, overlay, ramfs.
	switch uint64(fs.Type) {
	case 0xef53, 0x58465342, 0x9123683e, 0x01021994, 0x794c7630, 0x858458f6:
		return true
	}
	return false
}
