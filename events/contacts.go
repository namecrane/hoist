package events

type Contact struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

func (r *Receiver) ContactsModified(contacts []Contact) {

}

func (r *Receiver) ContactsDeleted(source string, contacts []string) {}
