# NameCrane Hoist

**NameCrane Hoist** is a Go library for NameCrane's file storage API.

## Features

- File access via direct API calls
- [afero](https://github.com/spf13/afero) driver for "filesystem" mocking

Planned:

- Cache implementation using SignalR events (Request full tree once, update as-needed)
- Calendar, Task, etc API implementations?

## Installation

```bash
go install github.com/namecrane/hoist
```

## Example

### Single user mode (recommended, default)
```go
// Define our API URL (note that you can use custom domains as well)
apiUrl := "https://us1.workspace.org"

// Create an auth manager
auth := hoist.NewAuthManager(apiUrl)

// Authenticate a user
if err := auth.Authenticate(context.Background(), "username", "password", ""); err != nil {
	log.Fatal(err)
}

// Create a client
client := hoist.NewClient(apiUrl, auth)

// Do things with the client, like get the root folder(s)
folders, err := client.GetFolders(context.Background())

if err != nil {
	log.Fatal(err)
}

// folders[0] will always be the root folder
log.Println(folders[0].Name)
```

### Multi user mode

If you wish to use multi-user mode, all Client functions can be called with a value on the context named "username"
which will specify which user to use. Users MUST be authenticated with `auth.Authenticate` first.