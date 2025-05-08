package hoist

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
)

var (
	ErrUnknownType      = errors.New("unknown content type")
	ErrUnexpectedStatus = errors.New("unexpected status")
	ErrNoFolder         = errors.New("no folder found")
	ErrNoFile           = errors.New("no file found")
)

type ClientOption func(*client)

// WithHttpClient defines the http client to use for http requests
func WithHttpClient(httpClient *http.Client) ClientOption {
	return func(c *client) {
		c.client = httpClient
	}
}

type Client interface {
	FileClient
}

// client is the Hoist API client implementation
type client struct {
	apiURL      string
	authManager AuthManager
	client      *http.Client
}

// NewClient creates a new Hoist client with the specified URL and auth manager
func NewClient(apiURL string, authManager AuthManager, opts ...ClientOption) Client {
	c := &client{
		apiURL:      apiURL,
		authManager: authManager,
		client:      http.DefaultClient,
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// defaultResponse represents a default API response, containing Success and optionally Message
type defaultResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

func (c *client) String() string {
	return "Hoist API (Endpoint: " + c.apiURL + ")"
}

// apiUrl joins the base API URL with the path specified
func (c *client) apiUrl(subPath string) (string, error) {
	u, err := url.Parse(c.apiURL)

	if err != nil {
		return "", err
	}

	u.Path = path.Join(u.Path, subPath)

	return u.String(), nil
}

func (c *client) doRequest(ctx context.Context, method, path string, body any, opts ...RequestOpt) (*Response, error) {
	ctx = context.WithValue(ctx, "httpClient", c.client)

	token, err := c.authManager.GetToken(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve token: %w", err)
	}

	opts = append(opts, WithHeader("Authorization", "Bearer "+token))

	apiUrl, err := c.apiUrl(path)

	if err != nil {
		return nil, err
	}

	return doHttpRequest(ctx, c.client, method, apiUrl, body, opts...)
}

// ParsePath parses the last segment off the specified path, representing either a file or directory
func (c *client) ParsePath(path string) (basePath, lastSegment string) {
	trimmedPath := strings.Trim(path, "/")

	segments := strings.Split(trimmedPath, "/")

	if len(segments) > 1 {
		basePath = "/" + strings.Join(segments[:len(segments)-1], "/")

		lastSegment = segments[len(segments)-1]
	} else {
		basePath = "/"
		lastSegment = segments[0]
	}

	return
}
