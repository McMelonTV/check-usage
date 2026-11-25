// Package filelock provides cross-process and in-process advisory file locks so
// local server CLI administration and the running HTTP server can safely update
// shared state.
package filelock

import (
	"os"
	"path/filepath"
	"sync"
)

var pathLocks sync.Map

func mutexFor(path string) *sync.Mutex {
	cleaned := filepath.Clean(path)
	if existing, ok := pathLocks.Load(cleaned); ok {
		return existing.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := pathLocks.LoadOrStore(cleaned, mu)
	return actual.(*sync.Mutex)
}

// Lock acquires an exclusive lock (both in-process and cross-process)
// associated with path using a sibling ".lock" file. Call the returned unlock
// function to release it.
func Lock(path string) (func(), error) {
	lockPath := path + ".lock"
	mu := mutexFor(lockPath)
	mu.Lock()
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		mu.Unlock()
		return nil, err
	}
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		mu.Unlock()
		return nil, err
	}
	if err := lockFile(file); err != nil {
		_ = file.Close()
		mu.Unlock()
		return nil, err
	}
	return func() {
		_ = unlockFile(file)
		_ = file.Close()
		mu.Unlock()
	}, nil
}
