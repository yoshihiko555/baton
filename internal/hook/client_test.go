package hook

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeHookConfig(t *testing.T, socketPath string, enabled bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := fmt.Sprintf("hook:\n  enabled: %t\n  socket_path: %s\n", enabled, socketPath)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}
	return path
}

func tmuxPane(paneID string) func(string) string {
	return func(key string) string {
		if key == "TMUX_PANE" {
			return paneID
		}
		return ""
	}
}

func TestRunHookCommandDisabled(t *testing.T) {
	configPath := writeHookConfig(t, filepath.Join(t.TempDir(), "missing.sock"), false)
	getenvCalled := false
	exitCode := RunHookCommand([]string{"--config", configPath}, strings.NewReader(`{"hook_event_name":"PermissionRequest"}`), func(string) string {
		getenvCalled = true
		return "%1"
	})

	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if getenvCalled {
		t.Fatal("disabled hook should return before reading TMUX_PANE")
	}
}

func TestRunHookCommandWithoutTMUXPane(t *testing.T) {
	socketPath := shortSocketPath(t)
	store := NewStore(3)
	server, err := Listen(ServerOptions{SocketPath: socketPath, Store: store})
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	t.Cleanup(func() { closeServer(t, server) })
	configPath := writeHookConfig(t, socketPath, true)

	exitCode := RunHookCommand([]string{"--config", configPath}, strings.NewReader(`{"hook_event_name":"PermissionRequest"}`), tmuxPane(""))
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
	if paneIDs := store.PaneIDs(); len(paneIDs) != 0 {
		t.Fatalf("unexpected states: %v", paneIDs)
	}
}

func TestRunHookCommandInvalidJSON(t *testing.T) {
	configPath := writeHookConfig(t, filepath.Join(t.TempDir(), "missing.sock"), true)
	exitCode := RunHookCommand([]string{"--config", configPath}, strings.NewReader(`{"hook_event_name":`), tmuxPane("%2"))
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
}

func TestRunHookCommandWithoutServer(t *testing.T) {
	configPath := writeHookConfig(t, filepath.Join(t.TempDir(), "missing.sock"), true)
	exitCode := RunHookCommand([]string{"--config", configPath}, strings.NewReader(`{"hook_event_name":"PermissionRequest"}`), tmuxPane("%3"))
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
}

func TestRunHookCommandDropsUnknownFieldsAndInjectsPaneID(t *testing.T) {
	socketPath := shortSocketPath(t)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
			t.Errorf("listener.Close failed: %v", err)
		}
	})

	type receiveResult struct {
		line string
		err  error
	}
	received := make(chan receiveResult, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			received <- receiveResult{err: err}
			return
		}
		defer conn.Close()
		scanner := bufio.NewScanner(conn)
		if !scanner.Scan() {
			err := scanner.Err()
			if err == nil {
				err = fmt.Errorf("connection closed without a line")
			}
			received <- receiveResult{err: err}
			return
		}
		received <- receiveResult{line: scanner.Text()}
	}()
	configPath := writeHookConfig(t, socketPath, true)

	exitCode := RunHookCommand(
		[]string{"--config", configPath},
		strings.NewReader(`{"hook_event_name":"PermissionRequest","session_id":"abc","unknown":"preserved"}`),
		tmuxPane("%3"),
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	var result receiveResult
	select {
	case result = <-received:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for hook payload")
	}
	if result.err != nil {
		t.Fatalf("receive payload failed: %v", result.err)
	}
	if strings.Contains(result.line, "unknown") {
		t.Fatalf("payload retained unknown field: %s", result.line)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(result.line), &payload); err != nil {
		t.Fatalf("decode payload failed: %v", err)
	}
	if got := payload["pane_id"]; got != "%3" {
		t.Fatalf("pane_id = %#v, want %%3", got)
	}
	if got := payload["session_id"]; got != "abc" {
		t.Fatalf("session_id = %#v, want abc", got)
	}
	if len(payload) != 3 {
		t.Fatalf("payload fields = %#v, want only pane_id, hook_event_name, and session_id", payload)
	}
}
