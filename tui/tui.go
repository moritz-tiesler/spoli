package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/moritz-tiesler/spoli/event"
)

type Broker interface {
	Source() chan event.Event
	Sink() chan event.Event
}

type model struct {
	choices  []string         // items on the to-do list
	cursor   int              // which to-do list item our cursor is pointing at
	selected map[int]struct{} // which to-do items are selected
	broker   Broker
}

func InitialModel(b Broker) model {
	return model{
		// Our to-do list is a grocery list
		choices: []string{
			event.TOGGLE_PLAY.String(),
			event.PREV.String(),
			event.NEXT.String(),
		},

		// A map which indicates which choices are selected. We're using
		// the  map like a mathematical set. The keys refer to the indexes
		// of the `choices` slice, above.
		selected: make(map[int]struct{}),
		broker:   b,
	}
}

func (m model) Init() tea.Cmd {
	// Just return `nil`, which means "no I/O right now, please."
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// Is it a key press?
	case tea.KeyMsg:

		// Cool, what was the actual key pressed?
		switch msg.String() {

		// These keys should exit the program.
		case "ctrl+c", "q":
			return m, tea.Quit

		// The "up" and "k" keys move the cursor up
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		// The "down" and "j" keys move the cursor down
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}

		// The "enter" key and the spacebar (a literal space) toggle
		// the selected state for the item that the cursor is pointing at.
		case "enter", " ":
			// _, ok := m.selected[m.cursor]
			// if ok {
			// 	delete(m.selected, m.cursor)
			// } else {
			// }

			clear(m.selected)
			m.selected[m.cursor] = struct{}{}

			switch m.cursor {
			case 0:
				sendOrTimeout(
					m.broker.Sink(),
					event.TOGGLE_PLAY,
					func() <-chan time.Time { return time.After(time.Second * 2) },
				)
			case 1:
				sendOrTimeout(
					m.broker.Sink(),
					event.PREV,
					func() <-chan time.Time { return time.After(time.Second * 2) },
				)
			case 2:
				sendOrTimeout(
					m.broker.Sink(),
					event.NEXT,
					func() <-chan time.Time { return time.After(time.Second * 2) },
				)
			}

		}
	}

	// Return the updated model to the Bubble Tea runtime for processing.
	// Note that we're not returning a command.
	return m, nil
}

func (m model) View() string {
	// The header
	s := "What should we buy at the market?\n\n"

	// Iterate over our choices
	for i, choice := range m.choices {

		// Is the cursor pointing at this choice?
		cursor := " " // no cursor
		if m.cursor == i {
			cursor = ">" // cursor!
		}

		// Is this choice selected?
		checked := " " // not selected
		if _, ok := m.selected[i]; ok {
			checked = "x" // selected!
		}

		// Render the row
		s += fmt.Sprintf("%s [%s] %s\n", cursor, checked, choice)
	}

	// The footer
	s += "\nPress q to quit.\n"

	// Send the UI for rendering
	return s
}

func sendOrTimeout(ch chan<- event.Event, v event.Event, or func() <-chan time.Time) {
	select {
	case ch <- v:
	case <-or():
	}
}
