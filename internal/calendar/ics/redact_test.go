package ics

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Sentinels standing in for the credential a real feed URL carries: Google's
// "secret address in iCal format" puts it in the path, share links put it in
// the query.
const (
	pathSecret  = "private-s3cr3tpath"
	querySecret = "s3cr3tquery"
)

// captured runs Events against a source whose URL embeds both sentinels and
// returns the error text plus everything the provider logged.
func captured(t *testing.T, base string) (errText, logText string) {
	t.Helper()

	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	feed := base + "/calendar/ical/" + pathSecret + "/basic.ics?key=" + querySecret
	p := New([]Source{{ID: "feed", URL: feed}}, log)

	from := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 11, 0, 0, 0, 0, time.UTC)

	_, err := p.Events(context.Background(), from, to)
	if err == nil {
		t.Fatal("expected an error from the failing feed")
	}
	return err.Error(), buf.String()
}

func TestFeedSecretsAreNotLogged(t *testing.T) {
	// Every failure mode that used to interpolate the raw URL.
	cases := map[string]http.HandlerFunc{
		"http error status": func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		},
		"malformed body": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/calendar")
			_, _ = w.Write([]byte("not a calendar\r\n"))
		},
		"oversized body": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/calendar")
			chunk := bytes.Repeat([]byte("X"), 1<<20)
			for range 17 {
				if _, err := w.Write(chunk); err != nil {
					return
				}
			}
		},
	}

	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			srv := httptest.NewServer(handler)
			defer srv.Close()

			errText, logText := captured(t, srv.URL)
			assertRedacted(t, errText, logText, srv.URL)
		})
	}

	t.Run("transport failure", func(t *testing.T) {
		// A closed port: net/http reports this as *url.Error, whose own
		// message quotes the whole request URL.
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		base := srv.URL
		srv.Close()

		errText, logText := captured(t, base)
		assertRedacted(t, errText, logText, base)
	})
}

func assertRedacted(t *testing.T, errText, logText, base string) {
	t.Helper()

	for label, text := range map[string]string{"error": errText, "log": logText} {
		for _, secret := range []string{pathSecret, querySecret} {
			if strings.Contains(text, secret) {
				t.Errorf("%s leaks the feed credential %q:\n%s", label, secret, text)
			}
		}
	}

	// Redaction must not make a failure undiagnosable: the host stays.
	host := strings.TrimPrefix(base, "http://")
	if !strings.Contains(errText, host) {
		t.Errorf("error no longer names the host %q, so the failure cannot be traced:\n%s", host, errText)
	}
}

func TestLogURLKeepsLocalPaths(t *testing.T) {
	// A filesystem path is not a bearer credential, and naming it is the
	// only way to find the file.
	if got := logURL("/home/me/calendars/work.ics"); got != "/home/me/calendars/work.ics" {
		t.Errorf("logURL redacted a local path: %q", got)
	}
	if got := logURL("https://host/secret/basic.ics?key=abc"); got != "https://host" {
		t.Errorf("logURL(remote) = %q, want scheme and host only", got)
	}
}
