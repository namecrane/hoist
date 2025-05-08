package hoist

// Cache will be a caching implementation, populated on startup and then updated via SignalR events
type Cache struct {
	event *Events
	root  Folder
}
