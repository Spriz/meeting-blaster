// Package tokens persists OAuth tokens in the operating system's credential
// store: Secret Service on Linux, Keychain on macOS, Credential Manager on
// Windows. Tokens are never written to the config file.
package tokens

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
	"golang.org/x/oauth2"

	"github.com/spriz/meeting-blaster/internal/config"
)

// ErrNotFound reports that no token is stored for an account.
var ErrNotFound = errors.New("no stored token")

// Store reads and writes OAuth tokens for named accounts.
type Store struct {
	// Service is the keyring service name. Empty uses the app default.
	Service string
}

func (s Store) service() string {
	if s.Service != "" {
		return s.Service
	}
	return config.AppName
}

// Save stores the token for account, replacing any existing entry.
func (s Store) Save(account string, tok *oauth2.Token) error {
	data, err := json.Marshal(tok)
	if err != nil {
		return fmt.Errorf("encode token: %w", err)
	}
	if err := keyring.Set(s.service(), account, string(data)); err != nil {
		return fmt.Errorf("store token in keyring: %w", err)
	}
	return nil
}

// Load retrieves the token for account, returning ErrNotFound if absent.
func (s Store) Load(account string) (*oauth2.Token, error) {
	data, err := keyring.Get(s.service(), account)
	if errors.Is(err, keyring.ErrNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("read token from keyring: %w", err)
	}

	var tok oauth2.Token
	if err := json.Unmarshal([]byte(data), &tok); err != nil {
		return nil, fmt.Errorf("decode stored token: %w", err)
	}
	return &tok, nil
}

// Delete removes the stored token, signing the account out. Deleting an
// account with no token is not an error.
func (s Store) Delete(account string) error {
	err := keyring.Delete(s.service(), account)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("delete token from keyring: %w", err)
	}
	return nil
}
