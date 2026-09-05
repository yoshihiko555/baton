package hook

import (
	"log"
	"sort"
	"sync"
)

const maxTrackedPanes = 256

// State は 1 pane あたりの hook 由来の状態を表す。
type State struct {
	Waiting        bool
	SessionID      string
	TranscriptPath string
	ToolName       string
	IdleScanStreak int
}

// Store は pane ID ごとの hook 状態を並行安全に保持する。
type Store struct {
	mu              sync.RWMutex
	states          map[string]State
	idleCancelScans int
}

// NewStore は hook 状態ストアを生成する。
func NewStore(idleCancelScans int) *Store {
	return &Store{
		states:          make(map[string]State),
		idleCancelScans: idleCancelScans,
	}
}

// Apply は hook イベントを pane の状態へ反映する。
func (s *Store) Apply(ev Event) {
	if ev.PaneID == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if ev.HookEventName == "SessionEnd" {
		delete(s.states, ev.PaneID)
		return
	}
	_, known := s.states[ev.PaneID]
	if !known && len(s.states) >= maxTrackedPanes {
		log.Printf("hook store: pane %q は追跡上限に達したため無視します", ev.PaneID)
		return
	}

	state := s.states[ev.PaneID]
	switch ev.HookEventName {
	case "PermissionRequest":
		state.Waiting = true
		state.IdleScanStreak = 0
		if ev.SessionID != "" {
			state.SessionID = ev.SessionID
		}
		if ev.TranscriptPath != "" {
			state.TranscriptPath = ev.TranscriptPath
		}
		// 相関情報と同様、空の tool_name では直近の有効値を失わない。
		if ev.ToolName != "" {
			state.ToolName = ev.ToolName
		}
	case "PreToolUse", "PostToolUse", "Stop", "UserPromptSubmit":
		state.Waiting = false
	case "SessionStart":
		state.Waiting = false
		if ev.SessionID != "" {
			state.SessionID = ev.SessionID
		}
		if ev.TranscriptPath != "" {
			state.TranscriptPath = ev.TranscriptPath
		}
	}

	s.states[ev.PaneID] = state
}

// Get は pane ID に対応する状態のコピーを返す。
func (s *Store) Get(paneID string) (State, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	state, ok := s.states[paneID]
	return state, ok
}

// NoteScanResult は pane の Idle 連続スキャン数を更新する。
func (s *Store) NoteScanResult(paneID string, paneIdle bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.states[paneID]
	if !ok {
		return
	}
	if !paneIdle {
		state.IdleScanStreak = 0
		s.states[paneID] = state
		return
	}

	state.IdleScanStreak++
	if s.idleCancelScans > 0 && state.Waiting && state.IdleScanStreak >= s.idleCancelScans {
		state.Waiting = false
		state.IdleScanStreak = 0
	}
	s.states[paneID] = state
}

// RemovePane は pane ID に対応する状態を削除する。
func (s *Store) RemovePane(paneID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, paneID)
}

// RetainPanes は alive に含まれない pane の状態を削除する。
func (s *Store) RetainPanes(alive map[string]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for paneID := range s.states {
		if !alive[paneID] {
			delete(s.states, paneID)
		}
	}
}

// PaneIDs は保持中の pane ID をソート済みで返す。
func (s *Store) PaneIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	paneIDs := make([]string, 0, len(s.states))
	for paneID := range s.states {
		paneIDs = append(paneIDs, paneID)
	}
	sort.Strings(paneIDs)
	return paneIDs
}
