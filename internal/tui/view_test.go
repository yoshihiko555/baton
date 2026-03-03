package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/yoshihiko555/baton/internal/core"
)

func TestViewContainsProjectsTitle(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	// Set size to avoid zero-size rendering
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "Projects") {
		t.Error("view should contain 'Projects' title")
	}
	if !strings.Contains(view, "Sessions") {
		t.Error("view should contain 'Sessions' title")
	}
}

func TestViewContainsStatusBar(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	// Add a project with sessions
	projects := []core.Project{
		{
			Path:        "/project-a",
			DisplayName: "project-a",
			Sessions: []*core.Session{
				{ID: "s1", State: core.Thinking},
				{ID: "s2", State: core.Idle},
			},
			ActiveCount: 1,
		},
	}
	updated, _ = m.Update(StateUpdateMsg(projects))
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "Projects: 1") {
		t.Error("status bar should show project count")
	}
	if !strings.Contains(view, "Active: 1") {
		t.Error("status bar should show active count")
	}
}

func TestViewShowsErrorWhenSet(t *testing.T) {
	m, _, _, _, _ := newTestModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(Model)

	updated, _ = m.Update(ErrMsg(tea.ErrProgramKilled))
	m = updated.(Model)

	view := m.View()
	if !strings.Contains(view, "error") {
		t.Error("view should display error message")
	}
}
