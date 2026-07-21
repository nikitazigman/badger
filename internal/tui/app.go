package tui

import (
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nikitazigman/badger/internal/storage"
)

func Run(path string, out io.Writer) error {
	db, err := storage.Open(path)
	if err != nil {
		return err
	}
	defer db.Close()

	overview, err := db.Overview()
	if err != nil {
		return err
	}

	appModel, err := newModel(db, overview)
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

func loadPageCmd(db storage.Database, pageNumber uint64) tea.Cmd {
	return func() tea.Msg {
		page, err := db.InspectPage(storage.PageRef{ID: pageNumber})
		if err != nil {
			return errMsg{err: fmt.Errorf("load page %d: %w", pageNumber, err)}
		}
		return pageLoadedMsg{page: page}
	}
}

type pageLoadedMsg struct {
	page *storage.PageInspection
}

type btreeIndexedMsg struct {
	id    storage.BTreeID
	pages []storage.PageRef
	err   error // hard failure from PagesForBTree (unreadable/invalid root)
}

func indexBTreeCmd(db storage.Database, id storage.BTreeID) tea.Cmd {
	return func() tea.Msg {
		pages, err := db.PagesForBTree(id)
		return btreeIndexedMsg{id: id, pages: pages, err: err}
	}
}
