package events

import (
	"fmt"
)

type MailboxSizeUpdate struct {
	Size    int64 `json:"size"`
	MaxSize int64 `json:"maxSize"`
}

func (r *Receiver) MailboxSizeUpdate(update []MailboxSizeUpdate) {
	fmt.Println("Size updated:", update)
}

type Mail struct {
	UID               int64  `json:"uid"`
	MailID            int64  `json:"mid"`
	OwnerEmailAddress string `json:"ownerEmailAddress"`
	Folder            string `json:"folder"`
	New               bool   `json:"isNew"`
}

func (r *Receiver) MailAdded(mail []Mail) {
	fmt.Println("Added mail:", mail)
}

func (r *Receiver) MailModified(mail []Mail) {
	fmt.Println("Modified mail:", mail)
}

func (r *Receiver) MailRemoved(mail []Mail) {
	fmt.Println("Deleted mail:", mail)
}
