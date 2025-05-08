package hoist

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Response wraps an *http.Response and provides extra functionality
type Response struct {
	*http.Response
}

// Data is a quick and dirty "read this data" for debugging
func (r *Response) Data() []byte {
	b, _ := io.ReadAll(r.Body)

	return b
}

// Decode only supports JSON.
// The API is weird and returns text/plain for JSON sometimes, but it's almost always JSON.
func (r *Response) Decode(data any) error {
	// Close by default on decode
	defer r.Close()

	return json.NewDecoder(r.Body).Decode(data)
}

// Close is a redirect to r.Body.Close for shorthand
func (r *Response) Close() error {
	return r.Body.Close()
}

// RequestOpt is a quick helper for changing request options
type RequestOpt func(r *http.Request)

// WithContentType overrides specified content types
func WithContentType(contentType string) RequestOpt {
	return func(r *http.Request) {
		r.Header.Set("Content-Type", contentType)
	}
}

// WithHeader sets header values on the request
func WithHeader(key, value string) RequestOpt {
	return func(r *http.Request) {
		r.Header.Set(key, value)
	}
}

// WithURLParameter replaces a URL parameter encased in {} with the value
func WithURLParameter(key string, value any) RequestOpt {
	return func(r *http.Request) {
		var valStr string
		switch v := value.(type) {
		case string:
			valStr = v
		case int:
			valStr = strconv.Itoa(v)
		default:
			valStr = fmt.Sprintf("%v", v)
		}

		r.URL.Path = strings.Replace(r.URL.Path, "{"+key+"}", valStr, -1)
	}
}

func doHttpRequest(ctx context.Context, client *http.Client, method, u string, body any, opts ...RequestOpt) (*Response, error) {
	var bodyReader io.Reader
	var jsonBody bool

	if body != nil {
		switch method {
		case http.MethodPost:
			switch v := body.(type) {
			case io.Reader:
				bodyReader = v
			case []byte:
				bodyReader = bytes.NewReader(v)
			case string:
				bodyReader = strings.NewReader(v)
			default:
				b, err := json.Marshal(body)

				if err != nil {
					return nil, err
				}

				bodyReader = bytes.NewReader(b)

				jsonBody = true
			}
		case http.MethodGet:
			switch v := body.(type) {
			case *url.Values:
				u += "?" + v.Encode()
			}
		}
	}

	// Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)

	if err != nil {
		return nil, fmt.Errorf("failed to create rmdir request: %w", err)
	}

	if jsonBody {
		req.Header.Set("Content-Type", "application/json")
	}

	// Apply extra options like overriding content types
	for _, opt := range opts {
		opt(req)
	}

	// Execute the HTTP request
	resp, err := client.Do(req)

	if err != nil {
		return nil, fmt.Errorf("failed to execute rmdir request: %w", err)
	}

	return &Response{
		Response: resp,
	}, err
}
