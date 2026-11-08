package xfeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/screwys/igloo/internal/download"
	"github.com/screwys/igloo/internal/model"
)

const (
	// X rate limits reset in 15-minute windows.
	cookieRateLimitRest  = 15 * time.Minute
	cookieSessionRest    = time.Hour
	cookieSessionMaxRest = 24 * time.Hour
	// A file whose session fails this many times in a row is dropped until it
	// is replaced.
	cookieDropAfter = 7
)

// CookiesUnavailableError reports that every X cookie file is resting or
// dropped. It says nothing about the account or source being fetched.
type CookiesUnavailableError struct {
	// RetryAt is when the next file can be tried; zero when every file is dropped.
	RetryAt time.Time
}

func (e *CookiesUnavailableError) Error() string {
	if e.RetryAt.IsZero() {
		return "every X cookie file is dropped; replace them to resume"
	}
	return "no X cookie file is usable until " + e.RetryAt.Format(time.RFC3339)
}

// FetchUnavailable reports failures of Igloo's own X fetching setup, resting
// cookie files or a missing gallery-dl, that say nothing about the account or
// source being fetched.
func FetchUnavailable(err error) bool {
	var cookies *CookiesUnavailableError
	return errors.As(err, &cookies) || download.ExecutableNotFound(err)
}

// CookieStateStore keeps the rest state of cookie files across restarts.
type CookieStateStore interface {
	XCookieStates() ([]model.XCookieState, error)
	SaveXCookieState(model.XCookieState) error
	DeleteXCookieState(label string) error
}

// CookiePool rotates gallery-dl cookie files across requests. A file rests
// after a rate limit or a rejected session, and repeated session failures
// drop it until the file is replaced.
type CookiePool struct {
	paths []string
	next  atomic.Uint64
	store CookieStateStore

	mu     sync.Mutex
	loaded bool
	states map[string]model.XCookieState
}

func NewCookiePool(cookiesDir string, store CookieStateStore) *CookiePool {
	return &CookiePool{paths: DiscoverCookieFiles(cookiesDir), store: store}
}

func DiscoverCookieFiles(cookiesDir string) []string {
	candidates := download.DiscoverCookieFiles(strings.TrimSpace(cookiesDir), "twitter")
	out := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		out = append(out, candidate.Path)
	}
	return out
}

func (p *CookiePool) Len() int {
	if p == nil {
		return 0
	}
	return len(p.paths)
}

// NextBatch returns the usable files in rotation order.
func (p *CookiePool) NextBatch(now time.Time) []string {
	if p.Len() == 0 {
		return nil
	}
	start := int(p.next.Add(1)-1) % len(p.paths)
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.paths))
	for i := range p.paths {
		path := p.paths[(start+i)%len(p.paths)]
		if retryAt, ok := p.retryAtLocked(path); ok && !now.Before(retryAt) {
			out = append(out, path)
		}
	}
	return out
}

// ReadyAt returns when a file can next be tried; zero when every file is
// dropped.
func (p *CookiePool) ReadyAt(now time.Time) time.Time {
	if p.Len() == 0 {
		return time.Time{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var readyAt time.Time
	for _, path := range p.paths {
		retryAt, ok := p.retryAtLocked(path)
		if !ok {
			continue
		}
		if retryAt.Before(now) {
			retryAt = now
		}
		if readyAt.IsZero() || retryAt.Before(readyAt) {
			readyAt = retryAt
		}
	}
	return readyAt
}

// retryAtLocked returns when path can be tried next, or false when it is
// dropped. A file that now holds a different login session starts fresh.
func (p *CookiePool) retryAtLocked(path string) (time.Time, bool) {
	p.loadLocked()
	label := download.CookieLabel(path, "")
	state, resting := p.states[label]
	if !resting {
		return time.Time{}, true
	}
	if cookieSession(path) != state.Session {
		p.forgetLocked(label)
		return time.Time{}, true
	}
	if state.Dropped {
		return time.Time{}, false
	}
	return time.UnixMilli(state.RetryAtMs), true
}

// Succeeded clears the failures of a file that worked.
func (p *CookiePool) Succeeded(path string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loadLocked()
	label := download.CookieLabel(path, "")
	if _, ok := p.states[label]; ok {
		p.forgetLocked(label)
	}
}

// RateLimited rests a file until X's rate-limit window resets. It does not
// count toward dropping the file.
func (p *CookiePool) RateLimited(path string, now time.Time, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loadLocked()
	state := p.stateLocked(path)
	state.RetryAtMs = max(state.RetryAtMs, now.Add(cookieRateLimitRest).UnixMilli())
	state.LastError = reason
	p.saveLocked(state)
}

// SessionFailed rests a file whose session X rejected, doubling the rest with
// each failure in a row, and drops it after cookieDropAfter failures.
func (p *CookiePool) SessionFailed(path string, now time.Time, reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loadLocked()
	state := p.stateLocked(path)
	state.FailCount++
	rest := min(cookieSessionRest<<min(state.FailCount-1, 5), cookieSessionMaxRest)
	state.RetryAtMs = now.Add(rest).UnixMilli()
	state.Dropped = state.FailCount >= cookieDropAfter
	state.LastError = reason
	if state.Dropped {
		log.Printf("[x_cookies] dropped %s after %d failed sessions until it is replaced: %s", state.Label, state.FailCount, reason)
	} else {
		log.Printf("[x_cookies] resting %s for %s after %d failed sessions: %s", state.Label, rest, state.FailCount, reason)
	}
	p.saveLocked(state)
}

func (p *CookiePool) stateLocked(path string) model.XCookieState {
	label := download.CookieLabel(path, "")
	state := p.states[label]
	state.Label = label
	state.Session = cookieSession(path)
	return state
}

// cookieSession identifies the X login session in a Netscape cookie file by a
// hash of its auth_token, so replacing the file with a new login is noticed
// while tools rewriting other cookies are not. A logged-out file returns "".
func cookieSession(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(fields) == 7 && fields[5] == "auth_token" && fields[6] != "" {
			sum := sha256.Sum256([]byte(fields[6]))
			return hex.EncodeToString(sum[:8])
		}
	}
	return ""
}

func (p *CookiePool) loadLocked() {
	if p.loaded {
		return
	}
	p.loaded = true
	p.states = make(map[string]model.XCookieState)
	if p.store == nil {
		return
	}
	states, err := p.store.XCookieStates()
	if err != nil {
		log.Printf("[x_cookies] load cookie state: %v", err)
		return
	}
	for _, state := range states {
		p.states[state.Label] = state
	}
}

func (p *CookiePool) saveLocked(state model.XCookieState) {
	p.states[state.Label] = state
	if p.store == nil {
		return
	}
	if err := p.store.SaveXCookieState(state); err != nil {
		log.Printf("[x_cookies] save %s: %v", state.Label, err)
	}
}

func (p *CookiePool) forgetLocked(label string) {
	delete(p.states, label)
	if p.store == nil {
		return
	}
	if err := p.store.DeleteXCookieState(label); err != nil {
		log.Printf("[x_cookies] clear %s: %v", label, err)
	}
}

type cookieFailure int

const (
	// cookieOther failures belong to the account or source; another cookie
	// would not change them.
	cookieOther cookieFailure = iota
	cookieRateLimited
	// cookieSessionFailed means X rejected the file's session: it is logged
	// out, expired or locked.
	cookieSessionFailed
	// cookieAccessDenied means this cookie's account cannot see the target,
	// for example a protected account it does not follow. Another might.
	cookieAccessDenied
)

// classifyCookieFailure reads the error gallery-dl reported for one cookie
// file and returns it with a short reason.
func classifyCookieFailure(err error, output []byte) (cookieFailure, string) {
	if reported := download.GalleryDLSemanticError(output); reported != nil {
		reason := reported.Error()
		text := strings.ToLower(reason)
		switch {
		case strings.Contains(text, "rate limit exceeded"):
			return cookieRateLimited, reason
		case strings.Contains(text, "could not authenticate you"),
			strings.Contains(text, "account temporarily locked"),
			strings.Contains(text, "authrequired") && strings.Contains(text, "cookie"):
			return cookieSessionFailed, reason
		case strings.Contains(text, "authrequired"), strings.Contains(text, "authorizationerror"):
			return cookieAccessDenied, reason
		}
		return cookieOther, reason
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return cookieOther, ""
	}
	switch download.ClassifyError(err, output) {
	case download.ErrorKindRateLimit:
		return cookieRateLimited, "rate limited"
	case download.ErrorKindAuth:
		return cookieAccessDenied, "authorization failed"
	}
	return cookieOther, ""
}
