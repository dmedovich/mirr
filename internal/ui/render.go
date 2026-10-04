package ui

import (
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"

	"mirr/internal/engine"
	"mirr/internal/terminal"
)

type screenLine struct {
	text, color string
	parts       []screenPart
}

type screenPart struct {
	text, color string
	width       int
}

const (
	cCyan    = "\x1b[38;5;45m"
	cGreen   = "\x1b[38;5;78m"
	cYellow  = "\x1b[38;5;221m"
	cRed     = "\x1b[38;5;203m"
	cDim     = "\x1b[38;5;245m"
	cBorder  = "\x1b[38;5;39m"
	cHeader  = "\x1b[1;38;5;255;48;5;24m"
	cSummary = "\x1b[38;5;159;48;5;24m"
	cSelect  = "\x1b[1;38;5;255;48;5;31m"
	cFooter  = "\x1b[38;5;252;48;5;236m"
	cPrompt  = "\x1b[1;38;5;231;48;5;130m"
	cAlert   = "\x1b[1;38;5;231;48;5;88m"
)

func render(m model) string {
	w, h := max(24, m.width), max(12, m.height)
	lines := make([]screenLine, 0, h)
	add := func(value, color string) { lines = append(lines, screenLine{text: value, color: color}) }
	label := "DASHBOARD"
	if m.historyOpen {
		label = "HISTORY"
	}
	if m.help {
		label = "HELP"
	}
	if m.busy {
		frames := []string{"|", "/", "-", "\\"}
		label += "  " + frames[m.tick%len(frames)]
	}
	add("  MIRR  /  "+label, cHeader)
	add("  "+summary(m.rows, w), cSummary)
	add(strings.Repeat("━", w), cCyan)
	bodyHeight := h - 5
	if m.help {
		content := make([]screenLine, 0, len(helpLines()))
		for _, item := range helpLines() {
			content = append(content, screenLine{text: item})
		}
		lines = append(lines, panel("KEYBOARD SHORTCUTS", content, w, bodyHeight)...)
		add("  Press ? or Esc to return", cFooter)
		add("  q quit", cFooter)
		return encode(lines, w, h)
	}

	section := "MIRRORS"
	if m.historyOpen {
		section = "LAST SYNC RESULTS"
	}
	rows := m.displayedRows()
	wide := w >= 110
	listWidth, detailWidth := w, w
	listHeight, detailHeight := bodyHeight, bodyHeight
	if wide {
		listWidth = min(56, max(46, w*2/5))
		detailWidth = w - listWidth - 1
	} else {
		listHeight = min(max(5, len(rows)+3), min(10, bodyHeight-3))
		listHeight = max(4, listHeight)
		detailHeight = bodyHeight - listHeight
	}
	visibleRows := max(1, listHeight-3)
	first := max(0, m.cursor-visibleRows+1)
	list := []screenLine{{text: tableHeader(listWidth - 4), color: cDim}}
	if len(rows) > 0 {
		for i := first; i < min(len(rows), first+visibleRows); i++ {
			row := rows[i]
			color := statusColor(row.Status)
			if i == m.cursor {
				color = cSelect
			}
			list = append(list, screenLine{text: tableRow(row, i == m.cursor, listWidth-4), color: color})
		}
	} else {
		list = append(list, screenLine{text: "No results yet. Press r to refresh.", color: cDim})
	}
	listTitle := fmt.Sprintf("%s  %d/%d", section, min(m.cursor+1, len(rows)), len(rows))
	listPanel := panel(listTitle, list, listWidth, listHeight)
	selected, ok := m.selected()
	detailTitle := "SELECTED PLAN"
	if m.historyOpen {
		detailTitle = "LAST RESULT"
	} else if m.detail == branchDetail {
		detailTitle = "BRANCHES  /  SOURCE → MIRROR"
	} else if m.detail == tagDetail {
		detailTitle = "TAGS  /  SOURCE → MIRROR"
	}
	if ok {
		detailTitle += "  /  " + selected.Repository + " / " + selected.Mirror
	}
	details := detailLines(m, selected, ok, detailWidth-4, detailHeight-2)
	detailPanel := panel(detailTitle, details, detailWidth, detailHeight)
	if wide {
		lines = append(lines, joinPanels(listPanel, detailPanel, listWidth, detailWidth)...)
	} else {
		lines = append(lines, listPanel...)
		lines = append(lines, detailPanel...)
	}
	if m.tokenAccount != "" {
		add("  TOKEN  "+strings.Repeat("•", min(24, utf8.RuneCount(m.token)))+"  Enter save · Esc cancel", cPrompt)
	} else if m.confirming {
		add("  "+confirmation(m), cPrompt)
	} else {
		color := cFooter
		if messageColor(m.message) == cRed {
			color = cAlert
		}
		add("  "+m.message, color)
	}
	keys := "  j/k move  b branches  t tags  r refresh  s sync  ? help  q quit"
	if m.historyOpen {
		keys = "  j/k select    r reload    h/Esc back    ? help    q quit"
	}
	if w < 70 {
		keys = "  j/k  b branches  t tags  ? help  q quit"
		if m.historyOpen {
			keys = "  j/k move  r reload  h/Esc back  ? help  q quit"
		}
	}
	add(keys, cFooter)
	return encode(lines, w, h)
}

func panel(title string, content []screenLine, width, height int) []screenLine {
	title = clip(title, width-6)
	border := "╭─ " + title + " " + strings.Repeat("─", max(0, width-5-runewidth.StringWidth(title))) + "╮"
	lines := make([]screenLine, 0, height)
	lines = append(lines, screenLine{text: border, color: cBorder})
	for i := 0; i < height-2; i++ {
		line := screenLine{}
		if i < len(content) {
			line = content[i]
		}
		line.text = "│ " + pad(line.text, width-4) + " │"
		if line.color == "" {
			line.color = cDim
		}
		lines = append(lines, line)
	}
	lines = append(lines, screenLine{text: "╰" + strings.Repeat("─", width-2) + "╯", color: cBorder})
	return lines
}

func joinPanels(left, right []screenLine, leftWidth, rightWidth int) []screenLine {
	lines := make([]screenLine, min(len(left), len(right)))
	for i := range lines {
		lines[i].parts = []screenPart{
			{text: left[i].text, color: left[i].color, width: leftWidth},
			{text: " ", width: 1},
			{text: right[i].text, color: right[i].color, width: rightWidth},
		}
	}
	return lines
}

func summary(rows []engine.Result, width int) string {
	current, pending, issues := 0, 0, 0
	for _, row := range rows {
		switch row.Status {
		case "current", "synced":
			current++
		case "pending":
			pending++
		default:
			issues++
		}
	}
	if width < 70 {
		return fmt.Sprintf("%d mirrors  %d ok  %d pending  %d issues", len(rows), current, pending, issues)
	}
	return fmt.Sprintf("%d mirrors    %d current    %d pending    %d need attention", len(rows), current, pending, issues)
}

func tableHeader(width int) string {
	if width >= 96 {
		return fmt.Sprintf("  %-35s %-12s %-12s %-8s %s", "PROJECT / MIRROR", "MODE", "STATE", "CHANGES", "CHECKED")
	}
	if width >= 70 {
		return fmt.Sprintf("  %-31s %-12s %-12s %s", "PROJECT / MIRROR", "MODE", "STATE", "CHANGES")
	}
	return fmt.Sprintf("  %-*s %-12s %s", max(8, width-25), "MIRROR", "STATE", "CHG")
}

func tableRow(row engine.Result, selected bool, width int) string {
	marker := "  "
	if selected {
		marker = "> "
	}
	name := terminal.Safe(row.Repository + " / " + row.Mirror)
	status := statusLabel(row.Status)
	if width >= 96 {
		checked := "—"
		if !row.CheckedAt.IsZero() {
			checked = row.CheckedAt.Local().Format("15:04:05")
		}
		return marker + pad(name, 35) + " " + pad(row.Mode, 12) + " " + pad(status, 12) + " " + pad(fmt.Sprint(len(row.Changes)), 8) + " " + checked
	}
	if width >= 70 {
		return marker + pad(name, 31) + " " + pad(row.Mode, 12) + " " + pad(status, 12) + " " + fmt.Sprint(len(row.Changes))
	}
	return marker + pad(name, max(8, width-25)) + " " + pad(status, 12) + " " + fmt.Sprint(len(row.Changes))
}

func detailLines(m model, row engine.Result, ok bool, width, height int) []screenLine {
	if !ok {
		return []screenLine{{text: "  Waiting for repository data.", color: cDim}}
	}
	if !m.historyOpen {
		switch m.detail {
		case branchDetail:
			return refLines(row, row.Branches, "branch", width, m.scroll)
		case tagDetail:
			return refLines(row, row.Tags, "tag", width, m.scroll)
		}
	}
	var lines []screenLine
	add := func(value, color string) { lines = append(lines, screenLine{text: value, color: color}) }
	if row.Error != "" {
		wrapped := wrap("  ERROR   "+row.Error, width)
		for _, part := range wrapped[:min(2, len(wrapped))] {
			add(part, cRed)
		}
	}
	if height >= 4 {
		add(fmt.Sprintf("  STATUS  %s    REFS  %d", statusLabel(row.Status), len(row.Changes)), statusColor(row.Status))
	}
	if len(row.Changes) == 0 && row.Error == "" {
		add("  No changes in the selected scope.", cGreen)
	} else if len(row.Changes) > 0 {
		add(fmt.Sprintf("  %d REF CHANGE(S)  [ / ] scroll", len(row.Changes)), cDim)
	}
	if !m.historyOpen && m.cfg != nil && height >= 5 {
		if repository, found := m.cfg.Repositories[row.Repository]; found {
			add("  SOURCE  "+repository.Source.URL, cDim)
			if mirror, found := repository.Mirrors[row.Mirror]; found {
				add("  TARGET  "+mirror.URL, cDim)
			}
		}
	}
	if row.Mode == "release" && height >= 6 {
		add("  MODE    Release tags only; branches stay as published.", cDim)
	}
	if !row.CheckedAt.IsZero() && m.historyOpen {
		add("  CHECKED "+row.CheckedAt.Local().Format(time.RFC3339), cDim)
	}
	start := min(m.scroll, max(0, len(row.Changes)-1))
	for _, change := range row.Changes[start:] {
		symbol := "~"
		switch change.Action {
		case "create":
			symbol = "+"
		case "delete":
			symbol = "-"
		case "force", "conflict":
			symbol = "!"
		}
		text := fmt.Sprintf("  %s %-8s %s   %s → %s", symbol, change.Action, change.Ref, shortSHA(change.Before), shortSHA(change.After))
		add(text, statusColor(change.Action))
	}
	return lines
}

func refLines(row engine.Result, refs []engine.RefSnapshot, kind string, width, scroll int) []screenLine {
	lines := []screenLine{{text: fmt.Sprintf("  %d %s(s)    %s", len(refs), kind, statusLabel(row.Status)), color: statusColor(row.Status)}}
	if row.Error != "" {
		lines = append(lines, screenLine{text: "  ERROR  " + row.Error, color: cRed})
	}
	if row.Mode == "release" && kind == "branch" {
		lines = append(lines, screenLine{text: "  Release mode: branches are not synchronized.", color: cYellow})
	}
	if row.Mode == "release" && kind == "tag" {
		lines = append(lines, screenLine{text: "  Release filters apply; see plan for selected tags.", color: cDim})
	}
	if len(refs) == 0 {
		lines = append(lines, screenLine{text: "  No " + kind + "s on source or mirror.", color: cDim})
		return lines
	}
	lines = append(lines, screenLine{text: "  " + strings.ToUpper(kind) + "                    SOURCE     MIRROR     STATE", color: cDim})
	start := min(scroll, max(0, len(refs)-1))
	for _, ref := range refs[start:] {
		state := refState(ref.State)
		name := pad(ref.Name, max(8, width-25-len(state)))
		value := fmt.Sprintf("  %s  %-8s → %-8s  %s", name, shortSHA(ref.Source), shortSHA(ref.Mirror), state)
		color := cGreen
		if ref.State != "same" {
			color = cYellow
		}
		lines = append(lines, screenLine{text: value, color: color})
	}
	return lines
}

func refState(state string) string {
	switch state {
	case "same":
		return "same"
	case "source_only":
		return "root only"
	case "mirror_only":
		return "mirror only"
	case "unavailable":
		return "unavailable"
	default:
		return "different"
	}
}

func confirmation(m model) string {
	switch m.confirm {
	case syncAll:
		changes := 0
		for _, row := range m.rows {
			changes += len(row.Changes)
		}
		return fmt.Sprintf("SYNC ALL  %d mirrors · %d refs  |  y confirm · n cancel", len(m.rows), changes)
	case changeMode:
		if row, ok := m.selected(); ok {
			return "CHANGE MODE  " + row.Repository + " / " + row.Mirror + "  |  y confirm · n cancel"
		}
	case syncSelected:
		if row, ok := m.selected(); ok {
			return fmt.Sprintf("SYNC  %s / %s · %d refs  |  y confirm · n cancel", row.Repository, row.Mirror, len(row.Changes))
		}
	}
	return "y confirm · n cancel"
}

func helpLines() []string {
	return []string{
		"j / k, arrows    Select a mirror",
		"r                Refresh plans from source and destinations",
		"s                Sync selected mirror after confirmation",
		"S                Sync all displayed mirrors after confirmation",
		"m                Change selected mirror mode",
		"b                Show source and mirror branches",
		"t                Show source and mirror tags",
		"[ / ]            Scroll the selected ref list",
		"h                Show last saved sync results",
		"a                Store a token for the selected keyring profile",
		"?                Show or hide this help",
		"q                Quit; cancel an active Git operation",
		"",
		"A sync applies only the plan currently displayed. Changed refs or",
		"configuration cause a fresh plan and require another confirmation.",
	}
}

func statusLabel(status string) string {
	switch status {
	case "current":
		return "CURRENT"
	case "pending":
		return "PENDING"
	case "synced":
		return "SYNCED"
	case "conflict":
		return "CONFLICT"
	case "stale":
		return "REVIEW"
	case "error":
		return "ERROR"
	default:
		return strings.ToUpper(status)
	}
}

func statusColor(status string) string {
	switch status {
	case "current", "synced", "create":
		return cGreen
	case "pending", "update", "stale":
		return cYellow
	case "error", "conflict", "delete", "force":
		return cRed
	default:
		return ""
	}
}

func messageColor(message string) string {
	lower := strings.ToLower(message)
	if strings.Contains(lower, "error") || strings.Contains(lower, "could not") {
		return cRed
	}
	return cDim
}

func shortSHA(value string) string {
	if value == "" {
		return "—"
	}
	if len(value) > 8 {
		return value[:8]
	}
	return value
}

func pad(value string, width int) string {
	value = clip(value, width)
	return value + strings.Repeat(" ", max(0, width-runewidth.StringWidth(value)))
}

func clip(value string, width int) string {
	value = terminal.Safe(value)
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(value) <= width {
		return value
	}
	return runewidth.Truncate(value, width, "…")
}

func wrap(value string, width int) []string {
	value = terminal.Safe(value)
	if width <= 0 {
		return nil
	}
	var lines []string
	for runewidth.StringWidth(value) > width {
		used, end := 0, 0
		for offset, r := range value {
			cells := runewidth.RuneWidth(r)
			if used+cells > width {
				break
			}
			used += cells
			end = offset + utf8.RuneLen(r)
		}
		if end == 0 {
			_, end = utf8.DecodeRuneInString(value)
		}
		lines = append(lines, value[:end])
		value = value[end:]
		if width > 2 {
			value = "  " + value
		}
	}
	return append(lines, value)
}

func encode(lines []screenLine, width, height int) string {
	colors := os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	var out strings.Builder
	for i := 0; i < min(len(lines), height); i++ {
		line := lines[i]
		if len(line.parts) > 0 {
			for _, part := range line.parts {
				writeStyled(&out, pad(part.text, part.width), part.color, colors)
			}
		} else {
			writeStyled(&out, pad(line.text, width), line.color, colors)
		}
		if i < min(len(lines), height)-1 {
			out.WriteByte('\n')
		}
	}
	return out.String()
}

func writeStyled(out *strings.Builder, value, color string, colors bool) {
	if colors && color != "" {
		out.WriteString(color)
		out.WriteString(value)
		out.WriteString("\x1b[0m")
		return
	}
	out.WriteString(value)
}
