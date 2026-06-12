package selector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

var ErrCancelled = errors.New("selection cancelled")

type Service interface {
	Select(context.Context, io.Reader, io.Writer, string, []string, string) (string, error)
}

type Terminal struct{}

func NewTerminal() *Terminal {
	return &Terminal{}
}

func (Terminal) Select(
	ctx context.Context,
	input io.Reader,
	output io.Writer,
	title string,
	options []string,
	initial string,
) (string, error) {
	if len(options) == 0 {
		return "", errors.New("no options available")
	}

	result, err := tea.NewProgram(
		newModel(title, options, initial),
		tea.WithContext(ctx),
		tea.WithInput(input),
		tea.WithOutput(output),
	).Run()
	if err != nil {
		return "", fmt.Errorf("run selector: %w", err)
	}

	finalModel, ok := result.(model)
	if !ok {
		return "", errors.New("selector returned an unexpected model")
	}
	if finalModel.cancelled {
		return "", ErrCancelled
	}
	return finalModel.options[finalModel.cursor], nil
}

type model struct {
	title     string
	options   []string
	cursor    int
	selected  bool
	cancelled bool
}

func newModel(title string, options []string, initial string) model {
	cursor := 0
	for index, option := range options {
		if option == initial {
			cursor = index
			break
		}
	}
	return model{title: title, options: options, cursor: cursor}
}

func (model) Init() tea.Cmd {
	return nil
}

func (m model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.options)-1 {
			m.cursor++
		}
	case "enter":
		m.selected = true
		return m, tea.Quit
	case "esc", "q", "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

func (m model) View() string {
	if m.selected || m.cancelled {
		return ""
	}

	var view strings.Builder
	fmt.Fprintf(&view, "%s\n\n", m.title)
	for index, option := range m.options {
		cursor := "  "
		if index == m.cursor {
			cursor = "> "
		}
		fmt.Fprintf(&view, "%s%s\n", cursor, option)
	}
	view.WriteString("\nup/down navigate | enter select | esc cancel\n")
	return view.String()
}
