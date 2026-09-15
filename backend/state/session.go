package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// SessionFileName marks a session that has started and not yet ended cleanly.
const SessionFileName = "session.open"

// Session is a marker file that exists for as long as the backend is running.
//
// It exists to answer one question on the next launch: did the last session end
// properly? The backend can be killed outright — the OOM killer on a tablet
// with ~2 GB shared with xochitl is the realistic case — and when it is,
// AppLoad tears the frontend down and Quire vanishes from under the user with
// no explanation. SIGKILL cannot be caught, so nothing can be said at the time.
// Saying it quietly on the *next* launch is the next best thing, and is far
// better than behaving as though nothing happened.
//
// A marker file is deliberately the whole mechanism. It costs one create and
// one remove per session, it needs no signal handling, and it is correct for a
// power cut as well — which is also a session that did not end properly, and
// also worth telling the user about.
type Session struct {
	path string
}

// OpenSession marks a session as started and reports whether the *previous* one
// ended abnormally.
//
// A failure to write the marker is not fatal and is returned for logging only:
// losing the ability to notice a crash next time is not a reason to refuse to
// start.
func OpenSession(dir string) (*Session, bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, fmt.Errorf("state: %w", err)
	}
	s := &Session{path: filepath.Join(dir, SessionFileName)}

	_, err := os.Stat(s.path)
	previousCrashed := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return s, false, fmt.Errorf("state: %w", err)
	}

	if err := os.WriteFile(s.path, []byte("open\n"), 0o600); err != nil {
		return s, previousCrashed, fmt.Errorf("state: %w", err)
	}
	return s, previousCrashed, nil
}

// Path is the marker file.
func (s *Session) Path() string { return s.path }

// Close records that this session ended properly. It is safe to call twice.
func (s *Session) Close() error {
	if s == nil {
		return nil
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("state: %w", err)
	}
	return nil
}
