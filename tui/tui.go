package tui

import (
	"fmt"
	"log"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/moritz-tiesler/spoli/event"

	"github.com/charmbracelet/bubbles/viewport"
)

type Broker interface {
	Source() chan event.Event
	Sink() chan event.Event
	FlushSource()
	FlushSink()
}

type model struct {
	choices  []string         // items on the to-do list
	cursor   int              // which to-do list item our cursor is pointing at
	selected map[int]struct{} // which to-do items are selected

	songInfo tea.Model

	broker Broker

	viewport viewport.Model
}

// TODO pub sub model: models sub to broker channel events
func InitialModel(b Broker) model {

	go func() {
		for e := range b.Source() {
			log.Printf("Dispatching %s\n", e)
			log.Printf("%+v\n", subs)
			cbs := subs[e.String()]
			for _, cb := range cbs {
				log.Printf("Dispatching %s to %T\n", e, cb)
				cb(e)
			}
		}
	}()
	m := model{
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
		songInfo: songInfo{},
		broker:   b,
		// viewport: viewport.New(30, 5),
	}

	sub(event.New(event.SONGCHANGE, nil), func(e event.Event) {
		if newSongEvent, ok := e.(event.SongChange); ok {
			d := newSongEvent.Data()
			song := d["songName"]
			m.songInfo, _ = m.songInfo.Update(song)
		} else {
			log.Printf("expected song change event, got %+v", e)
			m.songInfo, _ = m.songInfo.Update("ERROR")
		}
		log.Printf("Calling base model update")
		m.Update(m.songInfo)
	})

	return m
}

func (m model) Init() tea.Cmd {
	// Just return `nil`, which means "no I/O right now, please."
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	// Is it a key press?
	case songInfo:
		log.Printf("base model received songInfo=%v\n", msg)
		m.songInfo = msg
		m.View()
		// TODO: how to pass new songinfo into update cycle?
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
					event.New(event.TOGGLE_PLAY, nil),
					func() <-chan time.Time { return time.After(time.Second * 2) },
				)
			case 1:
				// m.broker.FlushSource()
				sendOrTimeout(
					m.broker.Sink(),
					event.New(event.PREV, nil),
					func() <-chan time.Time { return time.After(time.Second * 2) },
				)
				// e := <-m.broker.Source()
				// if newSongEvent, ok := e.(event.SongChange); ok {
				// 	d := newSongEvent.Data()
				// 	song := d["songName"]
				// 	m.songInfo, _ = m.songInfo.Update(song)
				// } else {
				// 	log.Printf("expected song change event, got %+v", e)
				// 	m.songInfo, _ = m.songInfo.Update("ERROR")
				// }
			case 2:
				// m.broker.FlushSource()
				sendOrTimeout(
					m.broker.Sink(),
					event.New(event.NEXT, nil),
					func() <-chan time.Time { return time.After(time.Second * 2) },
				)
				// songName = event.NEXT.String()
				// e := <-m.broker.Source()
				// if newSongEvent, ok := e.(event.SongChange); ok {
				// 	d := newSongEvent.Data()
				// 	song := d["songName"]
				// 	m.songInfo, _ = m.songInfo.Update(song)
				// } else {
				// 	log.Printf("expected song change event, got %+v", e)
				// 	m.songInfo, _ = m.songInfo.Update("ERROR")
				// }
			}

		}

	}

	// Return the updated model to the Bubble Tea runtime for processing.
	// Note that we're not returning a command.
	return m, nil
}

func (m model) View() string {
	log.Printf("model.View called with si=%+v\n", m)
	// The header
	s := fmt.Sprintf("%s\n\n", m.songInfo.View())

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

	gap := "\n"
	// Send the UI for rendering
	r := fmt.Sprintf(
		"%s%s%s",
		s,
		gap,
		m.songInfo.View(),
	)
	log.Println("base model render result: ", r)
	return r
}

func sendOrTimeout(ch chan<- event.Event, v event.Event, or func() <-chan time.Time) {
	select {
	case ch <- v:
	case <-or():
	}
}

type songInfo struct {
	text   string
	broker Broker
}

func (si songInfo) Init() tea.Cmd {
	return nil
}

func (si songInfo) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case string:
		si.text = msg
	default:

	}
	return si, nil
}

func (si songInfo) View() string {
	log.Printf("songInfo.View called with si.text=%s\n", si.text)
	return si.text
}

var subs map[string][]func(event.Event) = map[string][]func(event.Event){}
var mu sync.Mutex

func sub(e event.Event, cb func(event.Event)) {
	mu.Lock()
	defer mu.Unlock()
	key := e.String()
	subs[key] = append(subs[key], cb)
}
