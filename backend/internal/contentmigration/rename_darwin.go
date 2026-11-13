//go:build darwin

package contentmigration

import "golang.org/x/sys/unix"

func installDirectory(source, target string) error {
	return unix.RenamexNp(source, target, unix.RENAME_EXCL)
}
