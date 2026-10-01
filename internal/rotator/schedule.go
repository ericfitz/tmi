package rotator

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ericfitz/tmi/internal/slogging"
)

// Annotation keys (suffix = Rotation.Name()).
const (
	AnnRotatedAt  = "tmi.dev/rotated-at."
	AnnEvery      = "tmi.dev/rotate-every."
	AnnPhase      = "tmi.dev/rotation-phase."
	AnnGeneration = "tmi.dev/rotation-generation."

	DefaultRotateEvery = 90 * 24 * time.Hour
)

// ParseRotateEvery accepts "<n>d" or any Go duration; zero or negative is an error.
// SEM@3b682947: parse a rotation interval written as days or a Go duration (pure)
// SEM@3b682947: parse a rotation interval like 90d or a duration (pure)
func ParseRotateEvery(v string) (time.Duration, error) {
	v = strings.TrimSpace(v)
	if strings.HasSuffix(v, "d") {
		n, err := strconv.Atoi(strings.TrimSuffix(v, "d"))
		// 36500d (100y) cap keeps n*24h inside time.Duration.
		if err != nil || n <= 0 || n > 36500 {
			return 0, fmt.Errorf("invalid rotate-every %q", v)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("invalid rotate-every %q", v)
	}
	return d, nil
}

// IsDue reports whether the named rotation should start now: never rotated, an
// unreadable rotated-at, or older than rotate-every (default 90d; an unparsable
// interval is logged and falls back to the default, never to "now").
// SEM@3b682947: decide whether a secret's scheduled rotation is due from its annotations (pure)
// SEM@3b682947: determine whether a secret's rotation is due (pure)
func IsDue(s *Secret, name string, now time.Time) bool {
	last, ok := s.Annotations[AnnRotatedAt+name]
	if !ok {
		return true
	}
	at, err := time.Parse(time.RFC3339, last)
	if err != nil {
		slogging.Get().Warn("Unreadable rotated-at annotation for %s; treating as due", name)
		return true
	}
	every := DefaultRotateEvery
	if v, ok := s.Annotations[AnnEvery+name]; ok {
		if d, err := ParseRotateEvery(v); err == nil {
			every = d
		} else {
			slogging.Get().Warn("Unreadable rotate-every annotation for %s; using default %s", name, DefaultRotateEvery)
		}
	}
	return now.Sub(at) >= every
}

// AgeDays returns whole days since the last rotation, or -1 if never rotated.
// SEM@3b682947: compute days since a secret's last recorded rotation (pure)
// SEM@3b682947: compute a secret's age in days since last rotation (pure)
func AgeDays(s *Secret, name string, now time.Time) int {
	at, err := time.Parse(time.RFC3339, s.Annotations[AnnRotatedAt+name])
	if err != nil {
		return -1
	}
	return int(now.Sub(at).Hours() / 24)
}
