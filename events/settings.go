package events

import (
	"fmt"
)

func (r *Receiver) SettingsModified() {
	fmt.Println("Settings changed")
}
