package e2e_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/easytier/easytier-console/internal/auth"
	"github.com/easytier/easytier-console/internal/db"
	"github.com/easytier/easytier-console/internal/db/dal"
	"github.com/easytier/easytier-console/internal/session"
	"github.com/easytier/easytier-console/internal/settings"
	"github.com/easytier/easytier-console/internal/wire"
)

// TestShutdownReleasesGoroutines covers F-077: cancelling the root context must
// stop the session goroutines and their watchdogs. A leak here means every
// restart-in-place (config reload, SIGHUP) accumulates goroutines and sockets.
func TestShutdownReleasesGoroutines(t *testing.T) {
	rootCtx, shutdown := context.WithCancel(context.Background())
	defer shutdown()

	store, _, err := db.OpenWith(rootCtx, db.Config{
		Driver: dal.SQLite, DSN: filepath.Join(t.TempDir(), "et.db"), AutoAlign: true, Backup: false,
	})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	defer store.Close()
	if err := settings.New(store).MarkInitialized(rootCtx); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	hash, err := auth.Hash("Sup3r-Secret-Pass!")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if _, err := store.CreateUser(rootCtx, "tess", hash); err != nil {
		t.Fatalf("user: %v", err)
	}

	manager, err := session.NewManager(store, &session.LocalAuthenticator{Store: store},
		session.Policy{Interval: time.Second, Timeout: 6 * time.Second})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}

	// Baseline after the runtime has settled.
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	listener, err := wire.ListenTCP("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept(rootCtx)
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = recover() }()
				_ = manager.Serve(rootCtx, conn)
			}()
		}
	}()

	const devices = 5
	closers := make([]func(), 0, devices)
	for index := 0; index < devices; index++ {
		conn := connectOnce(t, manager, "tess", machineIDFor(index), nil)
		closers = append(closers, conn.close)
	}
	time.Sleep(300 * time.Millisecond)

	// Shut everything down: the sessions must disappear and the goroutines must
	// come back down.
	shutdown()
	for _, closeFn := range closers {
		closeFn()
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		if runtime.NumGoroutine() <= baseline+3 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("goroutines after shutdown = %d, baseline = %d (leak)", runtime.NumGoroutine(), baseline)
}

func machineIDFor(index int) string {
	hex := "0123456789abcdef"
	d := hex[index%16]
	return "aaaaaaaa-0000-4000-8000-00000000000" + string(d)
}
