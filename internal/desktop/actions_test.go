package desktop

import (
	"bytes"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

// steps records what the actions did, in order.
type steps struct{ done []string }

func (s *steps) actions(openErr error, log *slog.Logger) Actions {
	return Actions{
		Open: func(url string) error {
			s.done = append(s.done, "open "+url)
			return openErr
		},
		Stop: func() { s.done = append(s.done, "stop") },
		Quit: func() { s.done = append(s.done, "quit") },
		Log:  log,
	}
}

func TestPickOpensThePage(t *testing.T) {
	for _, item := range Menu(running("en")) {
		if item.URL == "" {
			continue
		}
		var s steps
		s.actions(nil, nil).Pick(item)
		if want := []string{"open " + item.URL}; !reflect.DeepEqual(s.done, want) {
			t.Errorf("%q did %v, want %v", item.Title, s.done, want)
		}
	}
}

// Quit stops the server first (the graceful stop of Ctrl+C, which writes the
// waiting answers), then removes the icon.
func TestPickQuitStopsTheServerThenRemovesTheIcon(t *testing.T) {
	var s steps
	var log bytes.Buffer
	s.actions(nil, slog.New(slog.NewTextHandler(&log, nil))).Pick(Item{Title: "Quit", Action: ActionQuit})
	if want := []string{"stop", "quit"}; !reflect.DeepEqual(s.done, want) {
		t.Errorf("Quit did %v, want %v", s.done, want)
	}
	if !strings.Contains(log.String(), "Quit from the tray menu") {
		t.Errorf("log: %s", log.String())
	}
}

func TestPickAnInformationLineDoesNothing(t *testing.T) {
	var s steps
	actions := s.actions(nil, nil)
	for _, item := range Menu(State{Version: "2.0", Port: 8080, Problem: "broken", DataFolder: "/x"}) {
		if item.Action == ActionInfo {
			actions.Pick(item)
		}
	}
	if len(s.done) != 0 {
		t.Errorf("information lines did %v", s.done)
	}
}

// A browser that does not open is written to the log; nothing else happens.
func TestPickWritesABrowserErrorToTheLog(t *testing.T) {
	var s steps
	var log bytes.Buffer
	s.actions(errors.New("no browser"), slog.New(slog.NewTextHandler(&log, nil))).Pick(Item{Action: ActionOpenAdmin, URL: "http://127.0.0.1:8080/admin/"})
	if !strings.Contains(log.String(), "Could not open the browser: no browser") || !strings.Contains(log.String(), "level=WARN") {
		t.Errorf("log: %s", log.String())
	}
	// No logger: still no panic.
	s.actions(errors.New("no browser"), nil).Pick(Item{Action: ActionOpenFiles, URL: "http://127.0.0.1:8080/files/"})
}
