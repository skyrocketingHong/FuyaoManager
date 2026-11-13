package caddymanage

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func (e *Engine) ancestors(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrPermission
	}
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || (stat.Uid != 0 && stat.Uid != e.ownerUID) {
			return ErrPermission
		}
		if info.Mode().Perm()&0022 != 0 && info.Mode()&os.ModeSticky == 0 {
			return ErrPermission
		}
		if current == filepath.Dir(current) {
			break
		}
	}
	return nil
}
func (e *Engine) trustedFile(path string) ([]byte, error) {
	if err := e.ancestors(filepath.Dir(path)); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, ErrPermission
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Uid != e.ownerUID || info.Mode().Perm()&0022 != 0 || info.Size() > maxFileBytes {
		return nil, ErrPermission
	}
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil || len(data) > maxFileBytes {
		return nil, ErrPermission
	}
	return data, nil
}
func (e *Engine) ensureDir(path string, mode os.FileMode) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return ErrPermission
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		parent := filepath.Dir(path)
		if _, err := os.Stat(parent); os.IsNotExist(err) {
			if err := e.ensureDir(parent, 0750); err != nil {
				return err
			}
		}
		if err := e.ancestors(parent); err != nil {
			return err
		}
		if os.Mkdir(path, mode) != nil {
			return ErrPermission
		}
		if os.Chown(path, int(e.ownerUID), e.workGID) != nil {
			return ErrPermission
		}
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrPermission
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != e.ownerUID || info.Mode().Perm()&0022 != 0 {
		return ErrPermission
	}
	if os.Chown(path, int(e.ownerUID), e.workGID) != nil || os.Chmod(path, mode.Perm()) != nil {
		return ErrPermission
	}
	return e.ancestors(filepath.Dir(path))
}
func (e *Engine) writeFile(path string, data []byte, mode os.FileMode) error {
	if len(data) > maxFileBytes {
		return ErrPermission
	}
	if err := e.ensureDir(filepath.Dir(path), 0750); err != nil {
		return err
	}
	return atomicWrite(path, data, mode, int(e.ownerUID), e.workGID)
}
func atomicWrite(path string, data []byte, mode os.FileMode, uid, gid int) error {
	temporary := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".fuyao-"+newID())
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return ErrPermission
	}
	defer func() { _ = file.Close(); _ = os.Remove(temporary) }()
	if file.Chown(uid, gid) != nil || file.Chmod(mode.Perm()) != nil {
		return ErrPermission
	}
	if _, err := file.Write(data); err != nil {
		return ErrUnavailable
	}
	if file.Sync() != nil || file.Close() != nil {
		return ErrUnavailable
	}
	if os.Rename(temporary, path) != nil {
		return ErrUnavailable
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return ErrUnavailable
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return ErrUnavailable
	}
	return nil
}
func (e *Engine) replaceSource(path string, data []byte) error {
	if _, err := e.trustedFile(path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return ErrPermission
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ErrPermission
	}
	return atomicWrite(path, data, info.Mode(), int(stat.Uid), int(stat.Gid))
}
func (e *Engine) writeRecord(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return ErrUnavailable
	}
	return e.writeFile(path, append(data, '\n'), 0600)
}
func (e *Engine) readRecord(path string, target any) error {
	data, err := e.trustedFile(path)
	if err != nil {
		return err
	}
	if strictJSON(data, target) != nil {
		return ErrPermission
	}
	return nil
}
func (e *Engine) loadState(key string) (*currentState, error) {
	var state currentState
	err := e.readRecord(filepath.Join(e.stateRoot, "sites", key+".json"), &state)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}
func (e *Engine) saveState(key string, state currentState) error {
	return e.writeRecord(filepath.Join(e.stateRoot, "sites", key+".json"), state)
}
func (e *Engine) saveRevision(revision rootRevision) error {
	return e.writeRecord(filepath.Join(e.stateRoot, "revisions", revision.ID+".json"), revision)
}
func (e *Engine) lock(ctx context.Context) (*os.File, error) {
	path := filepath.Join(e.stateRoot, ".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, ErrPermission
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, ErrPermission
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != e.ownerUID || info.Mode().Perm()&0022 != 0 {
		file.Close()
		return nil, ErrPermission
	}
	for {
		if syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil {
			return file, nil
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ErrUnavailable
		case <-time.After(20 * time.Millisecond):
		}
	}
}
