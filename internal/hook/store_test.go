package hook

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

func TestStoreApplyPermissionRequestAndRelease(t *testing.T) {
	store := NewStore(3)
	store.Apply(Event{
		PaneID:         "%1",
		HookEventName:  "PermissionRequest",
		SessionID:      "session-1",
		TranscriptPath: "/tmp/session-1.jsonl",
		ToolName:       "Bash",
	})

	state, ok := store.Get("%1")
	if !ok {
		t.Fatal("PermissionRequest did not create state")
	}
	if !state.Waiting {
		t.Fatal("PermissionRequest did not set Waiting")
	}
	if state.SessionID != "session-1" {
		t.Fatalf("SessionID = %q, want session-1", state.SessionID)
	}
	if state.TranscriptPath != "/tmp/session-1.jsonl" {
		t.Fatalf("TranscriptPath = %q, want /tmp/session-1.jsonl", state.TranscriptPath)
	}
	if state.ToolName != "Bash" {
		t.Fatalf("ToolName = %q, want Bash", state.ToolName)
	}

	store.Apply(Event{PaneID: "%1", HookEventName: "PreToolUse"})
	state, ok = store.Get("%1")
	if !ok {
		t.Fatal("release event removed state")
	}
	if state.Waiting {
		t.Fatal("PreToolUse did not release Waiting")
	}
	if state.SessionID != "session-1" || state.TranscriptPath != "/tmp/session-1.jsonl" || state.ToolName != "Bash" {
		t.Fatalf("release event cleared retained fields: %#v", state)
	}
}

func TestStoreSessionStartRecordsCorrelationAndReleases(t *testing.T) {
	store := NewStore(3)
	store.Apply(Event{
		PaneID:         "%2",
		HookEventName:  "SessionStart",
		SessionID:      "session-2",
		TranscriptPath: "/tmp/session-2.jsonl",
	})

	state, ok := store.Get("%2")
	if !ok {
		t.Fatal("SessionStart did not create state")
	}
	if state.Waiting {
		t.Fatal("SessionStart should release Waiting")
	}
	if state.SessionID != "session-2" || state.TranscriptPath != "/tmp/session-2.jsonl" {
		t.Fatalf("SessionStart correlation fields = %#v", state)
	}
}

func TestStoreSessionEndRemovesPane(t *testing.T) {
	store := NewStore(3)
	store.Apply(Event{PaneID: "%3", HookEventName: "PermissionRequest"})
	store.Apply(Event{PaneID: "%3", HookEventName: "SessionEnd"})

	if _, ok := store.Get("%3"); ok {
		t.Fatal("SessionEnd did not remove pane")
	}
}

func TestStoreUnknownEventDoesNotReleaseWaiting(t *testing.T) {
	store := NewStore(3)
	store.Apply(Event{PaneID: "%4", HookEventName: "PermissionRequest"})
	store.Apply(Event{PaneID: "%4", HookEventName: "FutureEvent"})

	state, ok := store.Get("%4")
	if !ok || !state.Waiting {
		t.Fatalf("unknown event changed Waiting state: state=%#v, ok=%v", state, ok)
	}
}

func TestStoreNoteScanResult(t *testing.T) {
	store := NewStore(3)
	store.Apply(Event{PaneID: "%5", HookEventName: "PermissionRequest"})

	store.NoteScanResult("%5", true)
	store.NoteScanResult("%5", true)
	state, _ := store.Get("%5")
	if !state.Waiting || state.IdleScanStreak != 2 {
		t.Fatalf("below threshold state = %#v, want Waiting with streak 2", state)
	}

	store.NoteScanResult("%5", false)
	state, _ = store.Get("%5")
	if !state.Waiting || state.IdleScanStreak != 0 {
		t.Fatalf("non-idle scan state = %#v, want Waiting with reset streak", state)
	}

	for range 2 {
		store.NoteScanResult("%5", true)
	}
	state, _ = store.Get("%5")
	if !state.Waiting || state.IdleScanStreak != 2 {
		t.Fatalf("below threshold after reset = %#v, want Waiting with streak 2", state)
	}

	store.NoteScanResult("%5", true)
	state, _ = store.Get("%5")
	if state.Waiting || state.IdleScanStreak != 0 {
		t.Fatalf("threshold state = %#v, want released Waiting with reset streak", state)
	}
}

func TestStoreNoteScanResultDisabled(t *testing.T) {
	store := NewStore(0)
	store.Apply(Event{PaneID: "%6", HookEventName: "PermissionRequest"})
	for range 10 {
		store.NoteScanResult("%6", true)
	}

	state, _ := store.Get("%6")
	if !state.Waiting {
		t.Fatal("idle cancellation should be disabled when threshold is non-positive")
	}
}

func TestStoreNoteScanResultResetsStreakAcrossWaitingEpisodes(t *testing.T) {
	store := NewStore(3)
	store.Apply(Event{PaneID: "%7", HookEventName: "PermissionRequest"})
	store.NoteScanResult("%7", true)
	store.NoteScanResult("%7", true)

	state, _ := store.Get("%7")
	if !state.Waiting || state.IdleScanStreak != 2 {
		t.Fatalf("first episode state = %#v, want Waiting with streak 2", state)
	}

	store.Apply(Event{PaneID: "%7", HookEventName: "PreToolUse"})
	store.Apply(Event{PaneID: "%7", HookEventName: "PermissionRequest"})
	store.NoteScanResult("%7", true)

	state, _ = store.Get("%7")
	if !state.Waiting || state.IdleScanStreak != 1 {
		t.Fatalf("new episode first scan state = %#v, want Waiting with streak 1", state)
	}

	store.NoteScanResult("%7", true)
	store.NoteScanResult("%7", true)
	state, _ = store.Get("%7")
	if state.Waiting || state.IdleScanStreak != 0 {
		t.Fatalf("new episode threshold state = %#v, want released Waiting with reset streak", state)
	}
}

func TestStoreApplyCapsTrackedPanesButUpdatesKnownPane(t *testing.T) {
	store := NewStore(3)
	for i := 0; i < maxTrackedPanes; i++ {
		store.Apply(Event{PaneID: fmt.Sprintf("%%%d", i), HookEventName: "SessionStart"})
	}

	store.Apply(Event{PaneID: "%overflow", HookEventName: "PermissionRequest"})
	if _, ok := store.Get("%overflow"); ok {
		t.Fatal("event above tracked-pane limit created state")
	}

	store.Apply(Event{PaneID: "%0", HookEventName: "PermissionRequest"})
	state, ok := store.Get("%0")
	if !ok || !state.Waiting {
		t.Fatalf("known pane was not updated at limit: state=%#v, ok=%v", state, ok)
	}
}

func TestStoreRetainPanes(t *testing.T) {
	store := NewStore(3)
	for _, paneID := range []string{"%3", "%1", "%2"} {
		store.Apply(Event{PaneID: paneID, HookEventName: "SessionStart"})
	}

	store.RetainPanes(map[string]bool{"%1": true, "%2": false})
	if got, want := store.PaneIDs(), []string{"%1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PaneIDs = %v, want %v", got, want)
	}
}

func TestStoreConcurrentApplyAndGet(t *testing.T) {
	store := NewStore(3)
	const goroutines = 32
	const iterations = 100

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				store.Apply(Event{PaneID: "%race", HookEventName: "PermissionRequest", SessionID: "session-race"})
				_, _ = store.Get("%race")
				_ = store.PaneIDs()
			}
		}()
	}
	wg.Wait()

	state, ok := store.Get("%race")
	if !ok || !state.Waiting {
		t.Fatalf("concurrent state = %#v, ok=%v", state, ok)
	}
}
