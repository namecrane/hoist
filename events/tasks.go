package events

type Task struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

func (r *Receiver) TasksModified(user string, tasks []Task) {

}

func (r *Receiver) TasksDeleted(user string, tasks []string) {}
