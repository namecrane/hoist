package events

type Event struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

func (r *Receiver) EventModified(event []Event) {}

func (r *Receiver) EventDeleted(event []Event) {}
