package ui

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"mirr/internal/auth"
	"mirr/internal/config"
	"mirr/internal/engine"
)

type action uint8

type detailView uint8

const (
	planDetail detailView = iota
	branchDetail
	tagDetail
)

const (
	inspect action = iota
	syncSelected
	syncAll
	changeMode
)

type target struct {
	project, mirror, reviewID string
}

type completion struct {
	rows    []engine.Result
	cfg     *config.Config
	single  bool
	err     error
	message string
}

type tickMsg struct{}

type model struct {
	ctx               context.Context
	cancel            context.CancelFunc
	load              func() (*config.Config, error)
	readHistory       func() ([]engine.Result, error)
	save              func([]engine.Result) error
	setMode           func(string, string, string) error
	rows, history     []engine.Result
	cfg               *config.Config
	cursor, scroll    int
	width, height     int
	tick              int
	busy              bool
	confirm           action
	confirming        bool
	help, historyOpen bool
	detail            detailView
	tokenAccount      string
	token             []byte
	message           string
}

func (m *model) displayedRows() []engine.Result {
	if m.historyOpen {
		return m.history
	}
	return m.rows
}

func (m *model) selected() (engine.Result, bool) {
	rows := m.displayedRows()
	if len(rows) == 0 || m.cursor >= len(rows) {
		return engine.Result{}, false
	}
	return rows[m.cursor], true
}

// Run owns terminal input, output and resize handling through Bubble Tea.
func Run(
	ctx context.Context,
	load func() (*config.Config, error),
	readHistory func() ([]engine.Result, error),
	save func([]engine.Result) error,
	setMode func(string, string, string) error,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	m := &model{
		ctx: ctx, cancel: cancel, load: load, readHistory: readHistory,
		save: save, setMode: setMode, width: 80, height: 24,
		message: "Reading configured mirrors…",
	}
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	clear(m.token)
	if errors.Is(err, tea.ErrProgramKilled) && ctx.Err() != nil {
		return nil
	}
	return err
}

func (m *model) Init() tea.Cmd { return m.start(inspect) }

func (m *model) View() string { return render(*m) }

func (m *model) start(kind action) tea.Cmd {
	m.busy = true
	m.confirming = false
	m.historyOpen = false
	m.message = "Checking Git repositories…  q cancels"
	if kind != inspect {
		m.message = "Synchronizing mirrors…  q cancels"
	}
	selected := make([]target, 0, len(m.rows))
	switch kind {
	case syncSelected:
		if row, ok := m.selected(); ok {
			selected = append(selected, target{row.Repository, row.Mirror, row.ReviewID})
		}
	case syncAll:
		for _, row := range m.rows {
			selected = append(selected, target{row.Repository, row.Mirror, row.ReviewID})
		}
	}
	ctx, load, readHistory, save := m.ctx, m.load, m.readHistory, m.save
	worker := func() tea.Msg {
		cfg, err := load()
		if err != nil {
			return completion{err: err}
		}
		if kind != inspect {
			if _, err := readHistory(); err != nil {
				return completion{err: err}
			}
		}
		core := &engine.Engine{Config: cfg}
		if kind == inspect {
			return completion{rows: core.Run(ctx, "", "", false), cfg: cfg}
		}
		results := make([]engine.Result, 0, len(selected))
		for _, item := range selected {
			if err := ctx.Err(); err != nil {
				return completion{rows: results, cfg: cfg, err: err}
			}
			rows := core.RunReviewed(ctx, item.project, item.mirror, item.reviewID)
			if len(rows) == 0 {
				rows = []engine.Result{{Repository: item.project, Mirror: item.mirror, Status: "stale", Error: "mirror was removed from configuration; refresh the plan"}}
			}
			results = append(results, rows...)
		}
		err = save(results)
		return completion{rows: results, cfg: cfg, single: kind == syncSelected, err: err}
	}
	return tea.Batch(worker, nextTick())
}

func nextTick() tea.Cmd {
	return tea.Tick(140*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tickMsg:
		if m.busy {
			m.tick++
			return m, nextTick()
		}
	case completion:
		m.completed(msg)
	case tea.KeyMsg:
		return m, m.key(msg)
	}
	return m, nil
}

func (m *model) completed(done completion) {
	m.busy = false
	if done.cfg != nil {
		m.cfg = done.cfg
	}
	if len(done.rows) > 0 {
		if done.single && len(m.rows) > 1 {
			for i, row := range m.rows {
				if row.Repository == done.rows[0].Repository && row.Mirror == done.rows[0].Mirror {
					m.rows[i] = done.rows[0]
				}
			}
		} else {
			m.rows = done.rows
		}
		m.cursor = min(m.cursor, max(0, len(m.rows)-1))
		m.scroll = 0
	}
	m.message = "Updated."
	if engine.Failed(done.rows) {
		m.message = "Review the highlighted errors or changed plans."
	}
	if done.message != "" {
		m.message = done.message
	}
	if done.err != nil {
		m.message = done.err.Error()
		if len(done.rows) > 0 {
			m.message = "Sync finished; history could not be saved: " + done.err.Error()
		}
	}
}

func (m *model) key(key tea.KeyMsg) tea.Cmd {
	name := key.String()
	if m.tokenAccount != "" {
		return m.tokenKey(key)
	}
	if name == "q" || name == "ctrl+c" {
		m.cancel()
		return tea.Quit
	}
	if m.busy {
		return nil
	}
	if m.help {
		if name == "?" || name == "esc" {
			m.help = false
		}
		return nil
	}
	if m.confirming {
		confirmed := name == "y" || name == "Y"
		m.confirming = false
		if !confirmed {
			m.message = "Cancelled."
			return nil
		}
		if m.confirm == changeMode {
			if row, ok := m.selected(); ok {
				mode := "release"
				if row.Mode == "release" {
					mode = "continuous"
				}
				if err := m.setMode(row.Repository, row.Mirror, mode); err != nil {
					m.message = err.Error()
					return nil
				}
				return m.start(inspect)
			}
			return nil
		}
		return m.start(m.confirm)
	}
	if m.historyOpen {
		switch name {
		case "esc", "h":
			m.historyOpen = false
			m.cursor = min(m.cursor, max(0, len(m.rows)-1))
		case "j", "down":
			m.cursor = min(m.cursor+1, max(0, len(m.history)-1))
		case "k", "up":
			m.cursor = max(0, m.cursor-1)
		case "r":
			var err error
			m.history, err = m.readHistory()
			if err != nil {
				m.message = err.Error()
			}
		case "?":
			m.help = true
		}
		return nil
	}
	switch name {
	case "j", "down":
		m.cursor = min(m.cursor+1, max(0, len(m.rows)-1))
		m.scroll = 0
	case "k", "up":
		m.cursor = max(0, m.cursor-1)
		m.scroll = 0
	case "]":
		m.scroll++
	case "[":
		m.scroll = max(0, m.scroll-1)
	case "b":
		if m.detail == branchDetail {
			m.detail = planDetail
		} else {
			m.detail = branchDetail
		}
		m.scroll = 0
	case "t":
		if m.detail == tagDetail {
			m.detail = planDetail
		} else {
			m.detail = tagDetail
		}
		m.scroll = 0
	case "r":
		return m.start(inspect)
	case "s":
		if len(m.rows) > 0 {
			m.confirm, m.confirming = syncSelected, true
		}
	case "S":
		if len(m.rows) > 0 {
			m.confirm, m.confirming = syncAll, true
		}
	case "m":
		if len(m.rows) > 0 {
			m.confirm, m.confirming = changeMode, true
		}
	case "h":
		var err error
		m.history, err = m.readHistory()
		if err != nil {
			m.message = err.Error()
		} else {
			m.historyOpen = true
			m.cursor = min(m.cursor, max(0, len(m.history)-1))
			m.scroll = 0
		}
	case "?":
		m.help = true
	case "a":
		m.beginToken()
	}
	return nil
}

func (m *model) beginToken() {
	row, ok := m.selected()
	if !ok {
		return
	}
	cfg, err := m.load()
	if err != nil {
		m.message = err.Error()
		return
	}
	repository, ok := cfg.Repositories[row.Repository]
	if !ok {
		m.message = "Mirror was removed from configuration. Press r to refresh."
		return
	}
	mirror, ok := repository.Mirrors[row.Mirror]
	if !ok {
		m.message = "Mirror was removed from configuration. Press r to refresh."
		return
	}
	credential, ok := cfg.Credentials[mirror.Auth]
	if !ok || credential.Provider != "keyring" {
		m.message = "Configure a keyring auth profile for this mirror in YAML first."
		return
	}
	m.tokenAccount = credential.Account
	m.token = nil
}

func (m *model) tokenKey(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "esc", "ctrl+c":
		m.tokenAccount = ""
		clear(m.token)
		m.token = nil
		m.message = "Token input cancelled."
	case "backspace", "ctrl+h":
		if len(m.token) > 0 {
			_, size := utf8.DecodeLastRune(m.token)
			clear(m.token[len(m.token)-size:])
			m.token = m.token[:len(m.token)-size]
		}
	case "enter":
		account, secret := m.tokenAccount, string(m.token)
		m.tokenAccount = ""
		clear(m.token)
		m.token = nil
		m.busy = true
		m.message = "Saving token to keyring…"
		ctx := m.ctx
		return tea.Batch(func() tea.Msg {
			c, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			err := auth.Store(c, account, secret)
			return completion{err: err, message: "Token stored. Press r to check access."}
		}, nextTick())
	default:
		if len(key.Runes) > 0 {
			m.token = append(m.token, []byte(string(key.Runes))...)
		}
	}
	return nil
}

var _ tea.Model = (*model)(nil)
