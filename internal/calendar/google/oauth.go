package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/oauth2"
	googleoauth "golang.org/x/oauth2/google"
	calendarapi "google.golang.org/api/calendar/v3"

	"github.com/spriz/meeting-blaster/internal/browser"
	"github.com/spriz/meeting-blaster/internal/config"
)

// Credentials identify this installation to Google. For an "installed
// application" client the secret is not confidential - it ships inside every
// copy of the binary and PKCE, not the secret, is what secures the exchange.
type Credentials struct {
	ClientID     string
	ClientSecret string
}

// ErrNoCredentials reports that no OAuth client has been configured yet.
var ErrNoCredentials = errors.New("no Google OAuth credentials configured")

// CredentialsPath is where a downloaded client_secret JSON is expected.
func CredentialsPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials.json"), nil
}

// LoadCredentials resolves the OAuth client from, in order: the
// MEETING_BLASTER_GOOGLE_CLIENT_ID / _SECRET environment variables, then
// credentials.json in the config directory, in the exact shape the Google
// Cloud console downloads.
func LoadCredentials() (Credentials, error) {
	if id := os.Getenv("MEETING_BLASTER_GOOGLE_CLIENT_ID"); id != "" {
		return Credentials{
			ClientID:     id,
			ClientSecret: os.Getenv("MEETING_BLASTER_GOOGLE_CLIENT_SECRET"),
		}, nil
	}

	path, err := CredentialsPath()
	if err != nil {
		return Credentials{}, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Credentials{}, ErrNoCredentials
	}
	if err != nil {
		return Credentials{}, fmt.Errorf("read %s: %w", path, err)
	}

	// The console wraps the client under "installed" for desktop apps and
	// "web" for web apps; accept either so a mis-picked type still works.
	var file struct {
		Installed *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
		Web *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"web"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return Credentials{}, fmt.Errorf("parse %s: %w", path, err)
	}

	switch {
	case file.Installed != nil && file.Installed.ClientID != "":
		return Credentials{file.Installed.ClientID, file.Installed.ClientSecret}, nil
	case file.Web != nil && file.Web.ClientID != "":
		return Credentials{file.Web.ClientID, file.Web.ClientSecret}, nil
	default:
		return Credentials{}, fmt.Errorf("%s has no client_id under \"installed\" or \"web\"", path)
	}
}

// Scopes requested at login. Read-only: the app never writes to a calendar.
var Scopes = []string{
	calendarapi.CalendarReadonlyScope,
	calendarapi.CalendarEventsReadonlyScope,
	"https://www.googleapis.com/auth/userinfo.email",
}

func oauthConfig(creds Credentials, redirectURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		Endpoint:     googleoauth.Endpoint,
		RedirectURL:  redirectURL,
		Scopes:       Scopes,
	}
}

// Authorize runs the interactive login: it starts a loopback HTTP server,
// opens the consent screen in the browser, and waits for Google to redirect
// back with an authorization code. It blocks until the user finishes, the
// context is cancelled, or authTimeout elapses.
//
// browserOverride, when set, names the executable used to show the consent
// screen; it mirrors the JoinBrowser setting.
func Authorize(ctx context.Context, creds Credentials, browserOverride string) (*oauth2.Token, error) {
	if creds.ClientID == "" {
		return nil, ErrNoCredentials
	}

	// Port 0 asks the kernel for a free port. Google permits any port on
	// the loopback interface for installed apps, so no fixed port needs
	// registering in the console.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("start loopback listener: %w", err)
	}
	defer listener.Close()

	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", listener.Addr().(*net.TCPAddr).Port)
	conf := oauthConfig(creds, redirectURL)

	verifier := oauth2.GenerateVerifier()
	state, err := randomState()
	if err != nil {
		return nil, err
	}

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		if errMsg := q.Get("error"); errMsg != "" {
			writePage(w, "Sign-in cancelled", "You can close this tab and try again from the tray menu.")
			results <- result{err: fmt.Errorf("google returned error: %s", errMsg)}
			return
		}
		// Comparing state defends against a cross-site request forging a
		// code into our loopback server.
		if q.Get("state") != state {
			writePage(w, "Sign-in failed", "The security check did not match. Please try again.")
			results <- result{err: errors.New("state mismatch in OAuth callback")}
			return
		}
		code := q.Get("code")
		if code == "" {
			writePage(w, "Sign-in failed", "Google did not return an authorization code.")
			results <- result{err: errors.New("no authorization code in callback")}
			return
		}

		writePage(w, "Signed in", "meeting-blaster is connected. You can close this tab.")
		results <- result{code: code}
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			results <- result{err: fmt.Errorf("loopback server: %w", err)}
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	// AccessTypeOffline is what makes Google issue a refresh token, so the
	// user signs in once rather than every hour.
	authURL := conf.AuthCodeURL(state,
		oauth2.AccessTypeOffline,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("prompt", "consent"),
	)
	if err := browser.Open(authURL, browserOverride); err != nil {
		return nil, fmt.Errorf("open consent screen (visit %s manually): %w", authURL, err)
	}

	ctx, cancel := context.WithTimeout(ctx, authTimeout)
	defer cancel()

	select {
	case res := <-results:
		if res.err != nil {
			return nil, res.err
		}
		tok, err := conf.Exchange(ctx, res.code, oauth2.VerifierOption(verifier))
		if err != nil {
			return nil, fmt.Errorf("exchange authorization code: %w", err)
		}
		return tok, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("waiting for Google sign-in: %w", ctx.Err())
	}
}

// authTimeout bounds how long the loopback server waits for the user to
// finish consenting before giving up.
const authTimeout = 5 * time.Minute

func randomState() (string, error) {
	// GenerateVerifier yields 32 bytes of URL-safe base64 randomness,
	// which is exactly the shape a state parameter needs.
	s := oauth2.GenerateVerifier()
	if s == "" {
		return "", errors.New("generate OAuth state")
	}
	return s, nil
}

func writePage(w http.ResponseWriter, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset="utf-8">
<title>%s</title>
<style>
 body{font:16px/1.6 system-ui,sans-serif;display:grid;place-items:center;
      height:100vh;margin:0;background:#101014;color:#e8e8ee}
 .card{text-align:center;padding:2rem 3rem}
 h1{font-size:1.4rem;margin:0 0 .5rem}
 p{margin:0;opacity:.7}
</style>
<div class="card"><h1>%s</h1><p>%s</p></div>`, title, title, message)
}
