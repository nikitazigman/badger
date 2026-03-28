package tui

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikitazigman/badger/internal/sqlite"
)

func Run(path string, out io.Writer) error {
	inspector, err := sqlite.Open(path)
	if err != nil {
		return err
	}
	defer inspector.Close()

	metadata, err := inspector.InspectDatabaseMetadata()
	if err != nil {
		return err
	}

	appModel, err := newModel(inspector, metadata)
	if err != nil {
		return err
	}

	program := tea.NewProgram(
		appModel,
		tea.WithAltScreen(),
		tea.WithOutput(out),
		tea.WithMouseCellMotion(),
	)

	finalModel, err := program.Run()
	if err != nil {
		return err
	}

	if result, ok := finalModel.(model); ok && result.err != nil {
		return result.err
	}

	return nil
}

type errMsg struct {
	err error
}

func loadPageCmd(inspector *sqlite.Inspector, pageNumber uint32) tea.Cmd {
	return func() tea.Msg {
		page, err := inspector.InspectPage(pageNumber)
		if err != nil {
			return errMsg{err: fmt.Errorf("load page %d: %w", pageNumber, err)}
		}
		return pageLoadedMsg{page: page}
	}
}

type pageLoadedMsg struct {
	page *sqlite.PageInspection
}
