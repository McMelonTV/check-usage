package tui

import (
	"context"
	"path/filepath"
	"time"

	"github.com/McMelonTV/check-usage/internal/providers/codexapi"
	"github.com/McMelonTV/check-usage/internal/remote"
	"github.com/McMelonTV/check-usage/internal/storage"
	"github.com/McMelonTV/check-usage/internal/usage"
	tea "github.com/charmbracelet/bubbletea"
)

func NewRemoteModel(connectionPath string, client *remote.Client) Model {
	prefsPath := filepath.Join(filepath.Dir(connectionPath), "client-display", "preferences.json")
	cachePath := filepath.Join(filepath.Dir(connectionPath), "client-display", "usage-cache.json")
	settings, err := storage.LoadSettings(prefsPath)
	if err != nil {
		settings = storage.DefaultSettings()
	}
	m := Model{remote: client, accountsPath: prefsPath, loading: true, settings: settings, resetCache: map[string]*codexapi.ResetCreditsPayload{}}
	m.rows, m.accounts = remote.Rows(remote.LoadDisplayCache(cachePath, client.Config.ServerID))
	for i := range m.rows {
		m.rows[i].Stale = true
	}
	m.initialized = len(m.rows) > 0
	m.loader = func() ([]usage.Row, []storage.Account, storage.Settings, map[string]*codexapi.ResetCreditsPayload, error) {
		results, err := client.Usage(context.Background(), "", true)
		if err != nil {
			return nil, nil, storage.Settings{}, nil, err
		}
		if err := remote.SaveDisplayCache(cachePath, client.Config.ServerID, results); err != nil {
			return nil, nil, storage.Settings{}, nil, err
		}
		settings, err := storage.LoadSettings(prefsPath)
		rows, accounts := remote.Rows(results)
		return rows, accounts, settings, map[string]*codexapi.ResetCreditsPayload{}, err
	}
	m.resetLoader = func(accountID string) (*codexapi.ResetCreditsPayload, error) {
		result, err := client.Resets(context.Background(), accountID)
		if err != nil {
			return nil, err
		}
		return remote.LocalCredits(result.Credits), nil
	}
	return m
}
func RunRemote(connectionPath string, client *remote.Client) error {
	program := tea.NewProgram(NewRemoteModel(connectionPath, client), tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := program.Run()
	return err
}
func (m Model) renameAccount(id, name string) tea.Cmd {
	if m.remote == nil {
		return renameAccountCmd(m.accountsPath, id, name)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.remote.Call(ctx, "accounts.rename", map[string]string{"account": id, "new_name": name}, nil)
		return storeSavedMsg{action: "Shared account renamed", reload: true, err: err}
	}
}
func (m Model) removeAccount(id, name string) tea.Cmd {
	if m.remote == nil {
		return removeAccountCmd(m.accountsPath, id, name)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := m.remote.Call(ctx, "accounts.remove", map[string]string{"account": id}, nil)
		return storeSavedMsg{action: "Removed shared account " + name, reload: true, err: err}
	}
}
