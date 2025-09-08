package event

type Event int

const (
	UKNOWN Event = iota
	TOGGLE_PLAY
	NEXT
	PREV
)

var eventName = map[Event]string{
	UKNOWN:      "unknown",
	TOGGLE_PLAY: "togglePlay",
	NEXT:        "next",
	PREV:        "prev",
}

func (e Event) String() string {
	return eventName[e]
}
