package hook

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yoshihiko555/baton/internal/config"
)

func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "bh")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("RemoveAll failed: %v", err)
		}
	})
	return filepath.Join(dir, "s.sock")
}

func closeServer(t *testing.T, server *Server) {
	t.Helper()
	if err := server.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func waitForState(t *testing.T, store *Store, paneID string, check func(State) bool) State {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		state, ok := store.Get(paneID)
		if ok && check(state) {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for state for %s; last state=%#v, exists=%v", paneID, state, ok)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServerReceivesPermissionRequest(t *testing.T) {
	socketPath := shortSocketPath(t)
	store := NewStore(3)
	server, err := Listen(ServerOptions{SocketPath: socketPath, Store: store})
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	t.Cleanup(func() { closeServer(t, server) })

	payload := []byte(`{"pane_id":"%1","hook_event_name":"PermissionRequest","session_id":"session-1"}`)
	if err := Send(config.HookConfig{SocketPath: socketPath}, payload); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	state := waitForState(t, store, "%1", func(state State) bool { return state.Waiting })
	if state.SessionID != "session-1" {
		t.Fatalf("SessionID = %q, want session-1", state.SessionID)
	}
}

func TestServerDebouncesPermissionRequestCallback(t *testing.T) {
	socketPath := shortSocketPath(t)
	store := NewStore(3)
	var calls atomic.Int32
	server, err := Listen(ServerOptions{
		SocketPath: socketPath,
		Store:      store,
		OnPermissionRequest: func() {
			calls.Add(1)
		},
		Debounce: 75 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	t.Cleanup(func() { closeServer(t, server) })

	for i := 0; i < 3; i++ {
		payload := []byte(fmt.Sprintf(`{"pane_id":"%%2","hook_event_name":"PermissionRequest","session_id":"session-%d"}`, i))
		if err := Send(config.HookConfig{SocketPath: socketPath}, payload); err != nil {
			t.Fatalf("Send failed: %v", err)
		}
	}

	waitForState(t, store, "%2", func(state State) bool { return state.Waiting })
	time.Sleep(225 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("callback calls = %d, want 1", got)
	}
}

func TestServerCanListenAgainAfterClose(t *testing.T) {
	socketPath := shortSocketPath(t)
	first, err := Listen(ServerOptions{SocketPath: socketPath, Store: NewStore(3)})
	if err != nil {
		t.Fatalf("first Listen failed: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	second, err := Listen(ServerOptions{SocketPath: socketPath, Store: NewStore(3)})
	if err != nil {
		t.Fatalf("second Listen failed: %v", err)
	}
	t.Cleanup(func() { closeServer(t, second) })
}

func TestServerRejectsSecondListenerWithoutStealingSocket(t *testing.T) {
	socketPath := shortSocketPath(t)
	store := NewStore(3)
	first, err := Listen(ServerOptions{SocketPath: socketPath, Store: store})
	if err != nil {
		t.Fatalf("first Listen failed: %v", err)
	}
	t.Cleanup(func() { closeServer(t, first) })

	second, err := Listen(ServerOptions{SocketPath: socketPath, Store: NewStore(3)})
	if second != nil {
		t.Cleanup(func() { closeServer(t, second) })
		t.Fatal("second Listen unexpectedly returned a server")
	}
	if !errors.Is(err, ErrAlreadyListening) {
		t.Fatalf("second Listen error = %v, want ErrAlreadyListening", err)
	}

	payload := []byte(`{"pane_id":"%3","hook_event_name":"PermissionRequest"}`)
	if err := Send(config.HookConfig{SocketPath: socketPath}, payload); err != nil {
		t.Fatalf("Send to first server failed: %v", err)
	}
	waitForState(t, store, "%3", func(state State) bool { return state.Waiting })
}

func TestListenCreatesSocketParentDirectory(t *testing.T) {
	root, err := os.MkdirTemp("", "bh")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(root); err != nil {
			t.Errorf("RemoveAll failed: %v", err)
		}
	})
	socketDir := filepath.Join(root, "nested")
	socketPath := filepath.Join(socketDir, "s.sock")

	server, err := Listen(ServerOptions{SocketPath: socketPath})
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	t.Cleanup(func() { closeServer(t, server) })

	fi, err := os.Stat(socketDir)
	if err != nil {
		t.Fatalf("Stat socket directory failed: %v", err)
	}
	if got := fi.Mode().Perm(); got != 0o700 {
		t.Fatalf("socket directory mode = %o, want 700", got)
	}
}

func TestListenRejectsSymlinkWithoutRemovingIt(t *testing.T) {
	socketPath := shortSocketPath(t)
	targetPath := filepath.Join(filepath.Dir(socketPath), "target")
	if err := os.WriteFile(targetPath, []byte("target"), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	if err := os.Symlink(targetPath, socketPath); err != nil {
		t.Fatalf("Symlink failed: %v", err)
	}

	server, err := Listen(ServerOptions{SocketPath: socketPath})
	if server != nil {
		t.Cleanup(func() { closeServer(t, server) })
		t.Fatal("Listen unexpectedly returned a server")
	}
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Listen error = %v, want symlink refusal", err)
	}
	fi, lstatErr := os.Lstat(socketPath)
	if lstatErr != nil {
		t.Fatalf("Lstat symlink failed: %v", lstatErr)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("Listen removed or replaced the symlink")
	}
}

func TestListenRejectsNonSocketWithoutRemovingIt(t *testing.T) {
	socketPath := shortSocketPath(t)
	if err := os.WriteFile(socketPath, []byte("keep"), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	server, err := Listen(ServerOptions{SocketPath: socketPath})
	if server != nil {
		t.Cleanup(func() { closeServer(t, server) })
		t.Fatal("Listen unexpectedly returned a server")
	}
	if err == nil || !strings.Contains(err.Error(), "not a socket") {
		t.Fatalf("Listen error = %v, want non-socket refusal", err)
	}
	content, readErr := os.ReadFile(socketPath)
	if readErr != nil {
		t.Fatalf("ReadFile failed: %v", readErr)
	}
	if string(content) != "keep" {
		t.Fatalf("file content = %q, want keep", content)
	}
}

func TestListenAppliesConnectionAndDeadlineOptions(t *testing.T) {
	defaultServer, err := Listen(ServerOptions{SocketPath: shortSocketPath(t)})
	if err != nil {
		t.Fatalf("default Listen failed: %v", err)
	}
	if got := cap(defaultServer.sem); got != defaultMaxConnections {
		t.Errorf("default semaphore capacity = %d, want %d", got, defaultMaxConnections)
	}
	if got := defaultServer.readDeadline; got != 5*time.Second {
		t.Errorf("default read deadline = %v, want 5s", got)
	}
	closeServer(t, defaultServer)

	customServer, err := Listen(ServerOptions{
		SocketPath:     shortSocketPath(t),
		MaxConnections: 2,
		ReadDeadline:   250 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("custom Listen failed: %v", err)
	}
	t.Cleanup(func() { closeServer(t, customServer) })
	if got := cap(customServer.sem); got != 2 {
		t.Errorf("custom semaphore capacity = %d, want 2", got)
	}
	if got := customServer.readDeadline; got != 250*time.Millisecond {
		t.Errorf("custom read deadline = %v, want 250ms", got)
	}
}

func TestServerRejectsConnectionsAboveLimit(t *testing.T) {
	socketPath := shortSocketPath(t)
	server, err := Listen(ServerOptions{
		SocketPath:     socketPath,
		MaxConnections: 1,
		ReadDeadline:   2 * time.Second,
	})
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	t.Cleanup(func() { closeServer(t, server) })

	first, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("first Dial failed: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	deadline := time.Now().Add(time.Second)
	for len(server.sem) != 1 {
		if time.Now().After(deadline) {
			t.Fatal("first connection did not acquire semaphore")
		}
		time.Sleep(10 * time.Millisecond)
	}

	second, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("second Dial failed: %v", err)
	}
	defer second.Close()
	if err := second.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatalf("SetReadDeadline failed: %v", err)
	}
	buf := make([]byte, 1)
	if _, err := second.Read(buf); err == nil {
		t.Fatal("connection above the limit was not closed")
	}
}
