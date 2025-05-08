package hoist

import (
	"context"
	"errors"
	"github.com/namecrane/hoist/events"
	"github.com/philippseith/signalr"
	"time"
)

var ErrAuthFailed = errors.New("auth failed")

// Events is a helper for managing SignalR events from the server
type Events struct {
	r           *events.Receiver
	client      signalr.Client
	apiUrl      string
	authManager AuthManager
}

// NewEventsClient creates a new event client, with apiUrl and authManager similar to client.
// Note that you must call Events.Connect yourself.
func NewEventsClient(apiUrl string, authManager AuthManager) *Events {
	return &Events{
		r:           &events.Receiver{},
		apiUrl:      apiUrl,
		authManager: authManager,
	}
}

// Connect opens a SignalR client and authenticates via Authenticate call
func (c *Events) Connect() error {
	ctx := context.Background()

	creationCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	conn, err := signalr.NewHTTPConnection(creationCtx, c.apiUrl+"/hubs/mail")

	if err != nil {
		return err
	}
	// Create the client and set a receiver for callbacks from the server
	client, err := signalr.NewClient(ctx,
		signalr.WithConnection(conn),
		signalr.WithReceiver(c.r))

	if err != nil {
		return err
	}

	c.client = client

	client.Start()

	// Authenticate
	return c.Authenticate()
}

// Authenticate will send a `connect` method with the bearer token to the server
func (c *Events) Authenticate() error {
	token, err := c.authManager.GetToken(context.Background())

	if err != nil {
		return err
	}

	res := <-c.client.Invoke("connect", token)

	if b, ok := res.Value.(bool); !ok || !b {
		return ErrAuthFailed
	}

	return nil
}
