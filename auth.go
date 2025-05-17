package hoist

import (
	"context"
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
	"net/http"
	"sync"
	"time"
)

var (
	ErrNoToken             = errors.New("could not find access token")
	ErrUnexpectedType      = errors.New("expected context value to be string")
	ErrExpiredRefreshToken = errors.New("refresh token expired")
)

const defaultUsername = "default"

type Store interface {
	// Set stores an authenticated user's access and refresh tokens
	Set(username string, auth AuthResponse)

	// Get retrieves an authenticated user's access and refresh token
	// This MUST return nil, nil if a stored auth does not exist
	Get(username string) (*AuthResponse, error)
}

// AuthManagerOption configures AuthManager for usage
type AuthManagerOption func(*authManager)

// WithAuthClient specifies the http client to use for authentication requests
func WithAuthClient(client *http.Client) AuthManagerOption {
	return func(manager *authManager) {
		manager.client = client
	}
}

// WithAuthStore sets the auth store for caching/storage of auth tokens
func WithAuthStore(store Store) AuthManagerOption {
	return func(manager *authManager) {
		manager.store = store
	}
}

type AuthManager interface {
	Authenticate(ctx context.Context, username, password, twoFactorCode string) error
	RefreshToken(ctx context.Context) error
	GetToken(ctx context.Context) (string, error)
}

// AuthManager manages the authentication token.
type authManager struct {
	mu           sync.Mutex
	client       *http.Client
	apiURL       string
	lastResponse *AuthResponse
	store        Store
}

// NewAuthManager initializes the AuthManager.
func NewAuthManager(apiURL string, opts ...AuthManagerOption) AuthManager {
	a := &authManager{
		client: http.DefaultClient,
		apiURL: apiURL,
	}

	for _, opt := range opts {
		opt(a)
	}

	return a
}

type authRequest struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	TwoFactorCode string `json:"twoFactorCode"`
}

type AuthResponse struct {
	Username               string    `json:"username"`
	Token                  string    `json:"accessToken"`
	TokenExpiration        time.Time `json:"accessTokenExpiration"` // Token expiration datetime
	RefreshToken           string    `json:"refreshToken"`
	RefreshTokenExpiration time.Time `json:"refreshTokenExpiration"`
}

// Authenticate obtains a new token.
func (am *authManager) Authenticate(ctx context.Context, username, password, twoFactorCode string) error {
	log.WithFields(log.Fields{
		"username": username,
	}).Debug("Trying to authenticate user")

	am.mu.Lock()
	defer am.mu.Unlock()

	// Construct the API URL for authentication
	url := fmt.Sprintf("%s/api/v1/auth/authenticate-user", am.apiURL)

	res, err := doHttpRequest(ctx, am.client, http.MethodPost, url, authRequest{
		Username:      username,
		Password:      password,
		TwoFactorCode: twoFactorCode,
	})

	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d", res.StatusCode)
	}

	// Parse the response
	var response AuthResponse

	if err := res.Decode(&response); err != nil {
		return fmt.Errorf("failed to decode authenteication response: %w", err)
	}

	// Store the token and expiration time
	if am.store != nil {
		am.store.Set(username, response)
	} else {
		am.lastResponse = &response
	}

	return nil
}

type refreshRequest struct {
	Token string `json:"token"`
}

func (am *authManager) RefreshToken(ctx context.Context) error {
	am.mu.Lock()
	defer am.mu.Unlock()

	url := fmt.Sprintf("%s/api/v1/auth/refresh-token", am.apiURL)

	res, err := doHttpRequest(ctx, am.client, http.MethodPost, url, refreshRequest{
		Token: am.lastResponse.RefreshToken,
	})

	if err != nil {
		return err
	}

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code %d", res.StatusCode)
	}

	var response AuthResponse

	if err := res.Decode(&response); err != nil {
		return fmt.Errorf("failed to decode refresh response: %w", err)
	}

	if am.store != nil {
		am.store.Set(response.Username, response)
	} else {
		am.lastResponse = &response
	}

	return nil
}

// GetToken ensures the token is valid and returns it.
func (am *authManager) GetToken(ctx context.Context) (string, error) {
	response := am.lastResponse

	if am.store != nil {
		username := ctx.Value("username")

		var usernameStr string

		if username == nil {
			usernameStr = defaultUsername
		} else {
			var ok bool
			usernameStr, ok = username.(string)

			if !ok {
				return "", ErrUnexpectedType
			}
		}

		var err error
		response, err = am.store.Get(usernameStr)

		if err != nil {
			return "", err
		}
	}

	if response == nil || response.Token == "" {
		log.Debug("No token set in AuthManager")
		return "", ErrNoToken
	}

	// Handle if we can't use our refresh token
	if response.RefreshTokenExpiration.Before(time.Now()) {
		log.Debug(am, "Refresh token expired")
		return "", ErrExpiredRefreshToken
	}

	// Give us a 5 minute grace period to prevent race conditions/issues
	if response.TokenExpiration.Before(time.Now().Add(5 * time.Minute)) {
		log.Debug("Access token expires soon, need to refresh")

		// Refresh token
		if err := am.RefreshToken(ctx); err != nil {
			return "", fmt.Errorf("failed to refresh token: %w", err)
		}
	}

	log.Debug("Using existing token")

	return response.Token, nil
}

func (am *authManager) String() string {
	return "Hoist Auth Manager"
}
