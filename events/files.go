package events

import (
	"fmt"
)

func (r *Receiver) FolderChange() {
	fmt.Println("Folder changed")
}

type FolderChange struct {
	Action       int    `json:"action"`
	ParentFolder string `json:"parentFolder"`
	Folder       string `json:"folder"`
}

func (r *Receiver) FsFolderChange(folder *FolderChange) {
	fmt.Println("Folder changed:", folder)

}

type File struct {
	ID     string `json:"id"`
	Source string `json:"source"`
}

func (r *Receiver) FilesAdded(files []File) {
	for _, file := range files {
		fmt.Println("File added:", file.ID)
	}
}

func (r *Receiver) FilesDeleted(files []File) {
	for _, file := range files {
		fmt.Println("File deleted:", file.ID)
	}
}

func (r *Receiver) FilesModified(files []File) {
	for _, file := range files {
		fmt.Println("File modified:", file.ID)
	}
}
