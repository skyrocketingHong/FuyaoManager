package publicationmedia

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// OpenDirectory walks every component with O_NOFOLLOW. It does not resolve
// an app-controlled symlink even when an ancestor changes during a copy.
// ASVS 5.3.2: trusted roots and descriptor-relative names prevent traversal.
func OpenDirectory(path string) (*os.File, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrUnsafe
	}
	fd, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	for _, part := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		if part == "" {
			continue
		}
		next, err := unix.Openat(fd, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		_ = unix.Close(fd)
		if err != nil {
			return nil, ErrUnsafe
		}
		fd = next
	}
	return os.NewFile(uintptr(fd), path), nil
}

func OpenFile(dir *os.File, name string, uid uint32) (*os.File, error) {
	if !SafeName(name) {
		return nil, ErrUnsafe
	}
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, ErrUnsafe
	}
	file := os.NewFile(uintptr(fd), name)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != uid || stat.Nlink != 1 || stat.Size > MaxFileBytes || stat.Mode&06000 != 0 {
		_ = file.Close()
		return nil, ErrUnsafe
	}
	return file, nil
}

type copyBudget struct {
	files int
	bytes int64
}

func CopyTree(ctx context.Context, source, target string, uid uint32) error {
	dir, err := OpenDirectory(source)
	if err != nil {
		return err
	}
	defer dir.Close()
	return copyDirectory(ctx, dir, target, uid, 0, &copyBudget{})
}

func copyDirectory(ctx context.Context, dir *os.File, target string, uid uint32, depth int, budget *copyBudget) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if depth > 32 {
		return ErrUnsafe
	}
	var stat unix.Stat_t
	if unix.Fstat(int(dir.Fd()), &stat) != nil || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != uid {
		return ErrUnsafe
	}
	if err := os.Mkdir(target, 0700); err != nil {
		return err
	}
	entries, err := dir.ReadDir(MaxFiles + 1)
	if err != nil && err != io.EOF {
		return err
	}
	if len(entries) > MaxFiles {
		return ErrUnsafe
	}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !SafeName(entry.Name()) {
			return ErrUnsafe
		}
		budget.files++
		if budget.files > MaxFiles {
			return ErrUnsafe
		}
		destination := filepath.Join(target, entry.Name())
		if entry.IsDir() {
			fd, err := unix.Openat(int(dir.Fd()), entry.Name(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return ErrUnsafe
			}
			child := os.NewFile(uintptr(fd), entry.Name())
			err = copyDirectory(ctx, child, destination, uid, depth+1, budget)
			_ = child.Close()
			if err != nil {
				return err
			}
			continue
		}
		file, err := OpenFile(dir, entry.Name(), uid)
		if err != nil {
			return err
		}
		info, err := file.Stat()
		if err != nil {
			_ = file.Close()
			return err
		}
		budget.bytes += info.Size()
		if budget.files > MaxFiles || budget.bytes > MaxTreeBytes {
			_ = file.Close()
			return ErrUnsafe
		}
		out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			_ = file.Close()
			return err
		}
		written, copyErr := io.Copy(out, io.LimitReader(file, MaxFileBytes+1))
		closeErr := out.Close()
		after, statErr := file.Stat()
		_ = file.Close()
		if copyErr != nil || closeErr != nil || statErr != nil || written != info.Size() || written > MaxFileBytes || after.Size() != info.Size() || after.ModTime() != info.ModTime() {
			return ErrUnsafe
		}
	}
	return nil
}
