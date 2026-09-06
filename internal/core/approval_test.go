package core

import (
	"errors"
	"testing"

	"github.com/yoshihiko555/baton/internal/terminal"
)

func TestApprovalActionKey(t *testing.T) {
	if got := ApprovalApprove.Key(); got != "Enter" {
		t.Errorf("ApprovalApprove.Key() = %q, want %q", got, "Enter")
	}
	if got := ApprovalDeny.Key(); got != "Escape" {
		t.Errorf("ApprovalDeny.Key() = %q, want %q", got, "Escape")
	}
}

func TestCanRespondToApproval(t *testing.T) {
	tests := []struct {
		name string
		s    Session
		want bool
	}{
		{
			name: "waiting claude with pane id",
			s:    Session{State: Waiting, Tool: ToolClaude, PaneID: "%5"},
			want: true,
		},
		{
			name: "waiting codex with pane id",
			s:    Session{State: Waiting, Tool: ToolCodex, PaneID: "%5"},
			want: true,
		},
		{
			name: "waiting antigravity is not approvable",
			s:    Session{State: Waiting, Tool: ToolAntigravity, PaneID: "%5"},
			want: false,
		},
		{
			name: "idle claude is not approvable",
			s:    Session{State: Idle, Tool: ToolClaude, PaneID: "%5"},
			want: false,
		},
		{
			name: "waiting claude with empty pane id",
			s:    Session{State: Waiting, Tool: ToolClaude, PaneID: ""},
			want: false,
		},
		{
			name: "waiting claude ambiguous is not approvable",
			s:    Session{State: Waiting, Tool: ToolClaude, PaneID: "%5", Ambiguous: true},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanRespondToApproval(tt.s); got != tt.want {
				t.Errorf("CanRespondToApproval(%+v) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

// fakeApprovalTerminal is a minimal terminal.Terminal fake that records SendKeys calls.
type fakeApprovalTerminal struct {
	sendKeysCalls int
	lastPaneID    string
	lastKeys      []string
	sendKeysErr   error
}

func (f *fakeApprovalTerminal) ListPanes() ([]terminal.Pane, error) { return nil, nil }
func (f *fakeApprovalTerminal) FocusPane(paneID string) error       { return nil }
func (f *fakeApprovalTerminal) GetPaneText(paneID string) (string, error) {
	return "", nil
}
func (f *fakeApprovalTerminal) IsAvailable() bool { return true }
func (f *fakeApprovalTerminal) SendKeys(paneID string, keys ...string) error {
	f.sendKeysCalls++
	f.lastPaneID = paneID
	f.lastKeys = keys
	return f.sendKeysErr
}
func (f *fakeApprovalTerminal) Name() string { return "fake" }

func TestSendApprovalOnApprovableSession(t *testing.T) {
	term := &fakeApprovalTerminal{}
	s := Session{State: Waiting, Tool: ToolClaude, PaneID: "%5"}

	if err := SendApproval(term, s, ApprovalApprove); err != nil {
		t.Fatalf("SendApproval() error = %v, want nil", err)
	}
	if term.sendKeysCalls != 1 {
		t.Fatalf("SendKeys called %d times, want 1", term.sendKeysCalls)
	}
	if term.lastPaneID != "%5" {
		t.Errorf("SendKeys pane = %q, want %q", term.lastPaneID, "%5")
	}
	if len(term.lastKeys) != 1 || term.lastKeys[0] != "Enter" {
		t.Errorf("SendKeys keys = %v, want [Enter]", term.lastKeys)
	}
}

func TestSendApprovalOnDenyKey(t *testing.T) {
	term := &fakeApprovalTerminal{}
	s := Session{State: Waiting, Tool: ToolCodex, PaneID: "%7"}

	if err := SendApproval(term, s, ApprovalDeny); err != nil {
		t.Fatalf("SendApproval() error = %v, want nil", err)
	}
	if len(term.lastKeys) != 1 || term.lastKeys[0] != "Escape" {
		t.Errorf("SendKeys keys = %v, want [Escape]", term.lastKeys)
	}
}

func TestSendApprovalOnNonApprovableSession(t *testing.T) {
	actions := []struct {
		name   string
		action ApprovalAction
	}{
		{"approve", ApprovalApprove},
		{"deny", ApprovalDeny},
	}

	for _, tc := range actions {
		t.Run(tc.name, func(t *testing.T) {
			term := &fakeApprovalTerminal{}
			s := Session{State: Idle, Tool: ToolClaude, PaneID: "%5"}

			err := SendApproval(term, s, tc.action)
			if err == nil {
				t.Fatal("SendApproval() error = nil, want ErrNotApprovable")
			}
			if !errors.Is(err, ErrNotApprovable) {
				t.Errorf("SendApproval() error = %v, want wrapping ErrNotApprovable", err)
			}
			if term.sendKeysCalls != 0 {
				t.Errorf("SendKeys called %d times, want 0", term.sendKeysCalls)
			}
		})
	}
}
