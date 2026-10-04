package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-runewidth"

	"mirr/internal/config"
	"mirr/internal/engine"
)

func TestDashboardFitsTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	row := engine.Result{
		Repository: "example", Mirror: "github", Mode: "continuous", Status: "pending",
		Changes: []engine.Change{{Ref: "refs/heads/main", Before: "1234567890", After: "abcdef1234", Action: "update"}},
	}
	m := model{
		rows: []engine.Result{row}, width: 80, height: 24,
		cfg: &config.Config{Repositories: map[string]config.Repository{
			"example": {Source: config.Endpoint{URL: "https://source.example/team/repo.git"}, Mirrors: map[string]config.Mirror{"github": {Endpoint: config.Endpoint{URL: "git@github.com:team/repo.git"}}}},
		}},
	}
	assertScreen(t, render(m), 80, 24)
	for _, expected := range []string{"PENDING", "refs/heads/main", "SOURCE", "TARGET", "s sync"} {
		if !strings.Contains(render(m), expected) {
			t.Fatalf("missing %q", expected)
		}
	}
}

func TestBubbleTeaInputConfirmsSync(t *testing.T) {
	m := &model{rows: []engine.Result{{Repository: "example", Mirror: "github", ReviewID: "review"}}}
	press := func(value rune) tea.Cmd {
		t.Helper()
		return m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}})
	}
	press('s')
	if !m.confirming || m.busy {
		t.Fatal("sync should wait for confirmation")
	}
	press('n')
	if m.confirming || m.busy {
		t.Fatal("cancel should not start sync")
	}
	press('s')
	if cmd := press('y'); cmd == nil || !m.busy || m.confirming {
		t.Fatal("confirmed sync should start a background command")
	}
	m.Update(tea.WindowSizeMsg{Width: 72, Height: 20})
	if m.width != 72 || m.height != 20 {
		t.Fatal("terminal resize was not applied")
	}
}

func TestTokenActionHandlesRemovedMirror(t *testing.T) {
	m := &model{
		rows: []engine.Result{{Repository: "example", Mirror: "github"}},
		load: func() (*config.Config, error) {
			return &config.Config{Repositories: map[string]config.Repository{}}, nil
		},
	}
	m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if !strings.Contains(m.message, "removed") || m.tokenAccount != "" {
		t.Fatal("removed mirror should be reported without opening token input")
	}
}

func TestErrorAndHelpFitSmallTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := model{rows: []engine.Result{{Repository: "example", Mirror: "github", Status: "error", Error: "Could not connect to the configured mirror host"}}, width: 50, height: 14}
	screen := render(m)
	assertScreen(t, screen, 50, 14)
	if !strings.Contains(screen, "ERROR") || !strings.Contains(screen, "Could not connect") || !strings.Contains(screen, "q quit") {
		t.Fatal("error is hidden in a small terminal")
	}
	m.help = true
	screen = render(m)
	assertScreen(t, screen, 50, 14)
	if !strings.Contains(screen, "Esc to return") {
		t.Fatal("help footer is hidden")
	}
}

func TestDashboardHighlightsSelectionAndConfirmation(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	m := model{width: 80, height: 24, rows: []engine.Result{{Repository: "项目", Mirror: "镜像", Status: "pending"}}}
	m.confirm, m.confirming = syncSelected, true
	screen := render(m)
	if !strings.Contains(screen, cHeader) || !strings.Contains(screen, cSelect) || !strings.Contains(screen, cPrompt) {
		t.Fatal("dashboard should distinguish header, selection and confirmation")
	}
	t.Setenv("NO_COLOR", "1")
	assertScreen(t, render(m), 80, 24)
}

func TestWideDashboardUsesTwoPanels(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := model{
		width: 120, height: 24,
		rows: []engine.Result{{
			Repository: "project", Mirror: "github", Status: "pending",
			Changes: []engine.Change{{Ref: "refs/heads/main", Action: "create"}},
		}},
	}
	screen := render(m)
	assertScreen(t, screen, 120, 24)
	if !strings.Contains(screen, "MIRRORS  1/1") || !strings.Contains(screen, "SELECTED PLAN") {
		t.Fatal("wide dashboard should show list and plan")
	}
	line := strings.Split(screen, "\n")[3]
	if strings.Count(line, "╭") != 2 {
		t.Fatal("wide dashboard should place both panels side by side")
	}
}

func TestBranchViewShowsSourceAndMirror(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := model{
		width: 120, height: 24,
		rows: []engine.Result{{Repository: "project", Mirror: "github", Mode: "release", Status: "current", Branches: []engine.RefSnapshot{
			{Name: "main", Source: "abcdef123456", Mirror: "abcdef123456", State: "same"},
			{Name: "feature", Source: "123456789abc", State: "source_only"},
		}}},
	}
	m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if m.detail != branchDetail {
		t.Fatal("branch view did not open")
	}
	screen := render(m)
	assertScreen(t, screen, 120, 24)
	for _, value := range []string{"BRANCHES", "main", "feature", "abcdef12", "root only", "branches are not synchronized"} {
		if !strings.Contains(screen, value) {
			t.Fatalf("branch view is missing %q", value)
		}
	}
}

func TestBranchViewKeepsUnavailableStateVisible(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := model{width: 80, height: 24, detail: branchDetail, rows: []engine.Result{{
		Repository: "amber", Mirror: "origin", Status: "error",
		Branches: []engine.RefSnapshot{{Name: "master", Source: "fc813dc21234", State: "unavailable"}},
	}}}
	screen := render(m)
	assertScreen(t, screen, 80, 24)
	if !strings.Contains(screen, "master") || !strings.Contains(screen, "unavailable") {
		t.Fatal("branch or unavailable state is clipped")
	}
}

func TestTagViewShowsFilteredReleaseInventory(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	m := model{width: 80, height: 24, rows: []engine.Result{{
		Repository: "showcase", Mirror: "releases", Mode: "release", Status: "pending",
		Tags: []engine.RefSnapshot{
			{Name: "v1.2.0", Source: "abcdef123456", State: "source_only"},
			{Name: "v2.0.0-rc1", Source: "123456789abc", State: "source_only"},
		},
	}}}
	press := func(value rune) { m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{value}}) }
	press('t')
	if m.detail != tagDetail {
		t.Fatal("tag view did not open")
	}
	screen := render(m)
	assertScreen(t, screen, 80, 24)
	for _, value := range []string{"TAGS", "v1.2.0", "v2.0.0-rc1", "filters apply"} {
		if !strings.Contains(screen, value) {
			t.Fatalf("tag view is missing %q", value)
		}
	}
	press('b')
	if m.detail != branchDetail {
		t.Fatal("branch key did not switch from tag view")
	}
	press('b')
	if m.detail != planDetail {
		t.Fatal("branch key did not return to plan")
	}
}

func assertScreen(t *testing.T, value string, width, height int) {
	t.Helper()
	lines := strings.Split(value, "\n")
	if len(lines) != height {
		t.Fatalf("got %d lines, want %d", len(lines), height)
	}
	for index, line := range lines {
		if n := runewidth.StringWidth(line); n > width {
			t.Fatalf("line %d has width %d, limit %d", index, n, width)
		}
	}
}
