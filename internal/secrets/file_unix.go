//go:build unix

package secrets

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"syscall"
)

func ValidateSecretFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("secret path %q is a directory", path)
	}
	perm := info.Mode().Perm()
	if perm&0o077 != 0 {
		return fmt.Errorf("insecure permissions on %q: %04o", path, perm)
	}
	uid := currentUID()
	if uid >= 0 {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			owner := int(stat.Uid)
			if owner != uid && owner != 0 {
				return fmt.Errorf("secret file %q must be owned by uid %d or root", path, uid)
			}
		}
	}
	return nil
}

func currentUID() int {
	u, err := user.Current()
	if err != nil {
		return -1
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return -1
	}
	return uid
}
