package filelock

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestLockSerializesAccess(t *testing.T) {
	target := filepath.Join(t.TempDir(), "accounts.json")
	counter := 0
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				unlock, err := Lock(target)
				if err != nil {
					t.Errorf("Lock() error = %v", err)
					return
				}
				counter++
				unlock()
			}
		}()
	}
	wg.Wait()
	if counter != 80 {
		t.Fatalf("counter = %d, want 80", counter)
	}
}
