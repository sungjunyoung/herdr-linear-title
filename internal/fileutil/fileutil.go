// Package fileutil provides advisory file locking and atomic file writes for
// state shared between concurrently running plugin processes.
package fileutil

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock blocks until it holds an exclusive advisory lock on path, creating the
// lock file if needed. Call the returned function to release the lock.
//
// Locks are per open file description, so they also exclude other goroutines
// in the same process that call Lock on the same path.
func Lock(path string) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	// Closing the descriptor releases the lock.
	return func() { _ = f.Close() }, nil
}

// WriteAtomic replaces path with data so readers never observe a partial
// file. The file is created with perm.
func WriteAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(perm); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
