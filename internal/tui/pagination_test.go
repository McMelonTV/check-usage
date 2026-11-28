package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/McMelonTV/check-usage/internal/providers"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestUsageListFillsTerminalBeforeScrolling(t *testing.T) {
	for _, tt := range []struct {
		name     string
		compact  bool
		accounts int
		visible  int
	}{
		{"compact exact fit", true, 14, 14},
		{"compact scroll hint", true, 15, 13},
		{"spaced exact fit", false, 7, 7},
		{"spaced scroll hint", false, 8, 6},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := Model{width: 110, height: 24, settings: storage.DefaultSettings()}
			m.settings.CompactMode = tt.compact
			for i := 0; i < tt.accounts; i++ {
				m.rows = append(m.rows, usage.Row{Name: fmt.Sprintf("Account %02d", i)})
			}
			start, end := m.usageVisibleRange(tuiContentWidth(m.width), m.height-2)
			if start != 0 || end != tt.visible {
				t.Fatalf("visible range = (%d, %d), want (0, %d)", start, end, tt.visible)
			}
			view := ansi.Strip(m.View())
			if got := lipgloss.Height(view); got > m.height {
				t.Fatalf("view height = %d, terminal height = %d", got, m.height)
			}
			if got := strings.Contains(view, "showing "); got != (tt.visible < tt.accounts) {
				t.Fatalf("scroll hint present = %v:\n%s", got, view)
			}
			// Growing the terminal should reveal every account immediately.
			updated, _ := m.Update(tea.WindowSizeMsg{Width: 110, Height: 40})
			view = ansi.Strip(updated.(Model).View())
			if strings.Contains(view, "showing ") || !strings.Contains(view, m.rows[len(m.rows)-1].Name) {
				t.Fatalf("larger terminal still hides accounts:\n%s", view)
			}
		})
	}
}

func TestUsagePaginationFitsChromeAndMapsMouseRows(t *testing.T) {
	for _, compact := range []bool{false, true} {
		for _, state := range []string{"normal", "notice", "warning", "rename", "rename notice"} {
			t.Run(fmt.Sprintf("compact=%v/%s", compact, state), func(t *testing.T) {
				m := Model{width: 110, height: 30, initialized: true, settings: storage.DefaultSettings()}
				m.settings.CompactMode = compact
				for i := 0; i < 30; i++ {
					row := usage.Row{Name: fmt.Sprintf("Account %02d", i)}
					if i%3 == 0 {
						row = usage.BaseRow(storage.Account{Name: row.Name, Provider: providers.Antigravity})
					}
					m.rows = append(m.rows, row)
				}
				switch state {
				case "notice":
					m.notice = "Account refreshed"
				case "warning":
					m.err = errors.New("offline")
				case "rename", "rename notice":
					m.editingName, m.nameInput = true, "Personal"
					if state == "rename notice" {
						m.notice = "Choose a name"
					}
				}
				for _, cursor := range []int{0, 15, 29} {
					m.cursor = cursor
					view := ansi.Strip(m.View())
					if got := lipgloss.Height(view); got > m.height {
						t.Fatalf("cursor %d: view height = %d, terminal height = %d:\n%s", cursor, got, m.height, view)
					}
					if !strings.Contains(view, m.rows[cursor].Name) {
						t.Fatalf("selected account %d hidden:\n%s", cursor, view)
					}
					for y, line := range strings.Split(view, "\n") {
						for i, row := range m.rows {
							if !strings.Contains(line, row.Name) {
								continue
							}
							if index, ok := m.usageMouseRowIndex(y); !ok || index != i {
								t.Fatalf("account %d at y=%d maps to (%d, %v)", i, y, index, ok)
							}
						}
					}
				}
			})
		}
	}
}
