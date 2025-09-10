package event

type Event interface {
	Data() map[any]any
	String() string
}

type event int

const (
	UKNOWN event = iota
	TOGGLE_PLAY
	NEXT
	PREV
	SONGCHANGE
)

var eventName = map[event]string{
	UKNOWN:      "unknown",
	TOGGLE_PLAY: "togglePlay",
	NEXT:        "next",
	PREV:        "prev",
	SONGCHANGE:  "songChange",
}

func (e event) String() string {
	return eventName[e]
}

type TogglePlay struct {
	e event
}

func (u TogglePlay) Data() map[any]any {
	return nil
}

func (u TogglePlay) String() string {
	return u.e.String()
}

type Next struct {
	e    event
	data map[any]any
}

func (n Next) Data() map[any]any {
	return n.data
}

func (n Next) String() string {
	return n.e.String()
}

type Prev struct {
	e    event
	data map[any]any
}

func (n Prev) Data() map[any]any {
	return n.data
}

func (n Prev) String() string {
	return n.e.String()
}

type Unknown struct {
	e event
}

func (u Unknown) Data() map[any]any {
	return nil
}

func (u Unknown) String() string {
	return u.e.String()
}

type SongChange struct {
	e    event
	data map[any]any
}

func (sc SongChange) Data() map[any]any {
	return sc.data
}

func (sc SongChange) String() string {
	return sc.e.String()
}

func New(e event, data map[any]any) Event {
	switch e {
	case TOGGLE_PLAY:
		return TogglePlay{TOGGLE_PLAY}
	case PREV:
		return Prev{PREV, data}
	case NEXT:
		return Next{NEXT, data}
	case SONGCHANGE:
		return SongChange{SONGCHANGE, data}
	default:
		return Unknown{UKNOWN}
	}
}
