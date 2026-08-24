package main

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	cookieName    = "vr2_example_session"
	sessionTTL    = 2 * time.Hour
	sweepInterval = 5 * time.Minute
)

// session is everything the app knows about one browser: the credentials that
// were typed in, the tokens they bought, and the last answer each console card
// got. None of it is written anywhere. A restart loses the lot, which is the
// point.
type session struct {
	mu       sync.Mutex
	clientID string
	secret   string
	state    string
	tok      tokenSet
	results  map[string]cardResult
	expires  time.Time
}

type cardResult struct {
	Input    string
	Status   int
	Duration time.Duration
	Body     string
	Failure  string
}

type tokenSet struct {
	AccessToken  string
	RefreshToken string
	TokenType    string
	ExpiresAt    time.Time
	Revoked      bool
}

// The refresh token normally outlives the access token, so this is a reason to
// refresh rather than to start the flow again.
func (t tokenSet) Expired() bool {
	return t.AccessToken != "" && !t.ExpiresAt.IsZero() && !time.Now().Before(t.ExpiresAt)
}

func (s *session) setClient(id, secret string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clientID, s.secret = id, secret
}

func (s *session) client() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clientID, s.secret
}

func (s *session) newState() (string, error) {
	state, err := randomHex(16)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = state
	return state, nil
}

// consumeState compares the state handed back on the callback with the one we
// generated, and clears it either way so a code cannot be replayed against it.
//
// This check is the only CSRF defence in the flow. Without it anyone could
// point a logged-in browser at /callback carrying a code from their own
// authorization, and this app would happily bind the session to their account.
func (s *session) consumeState(got string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := s.state
	s.state = ""
	return want != "" && got == want
}

func (s *session) setToken(t tokenSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tok = t
}

func (s *session) token() tokenSet {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tok
}

func (s *session) markRevoked() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tok.Revoked = true
}

func (s *session) setResult(key string, r cardResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.results == nil {
		s.results = make(map[string]cardResult)
	}
	s.results[key] = r
}

func (s *session) result(key string) (cardResult, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.results[key]
	return r, ok
}

type store struct {
	mu       sync.Mutex
	sessions map[string]*session
	secure   bool
}

func newStore(secure bool) *store {
	return &store{sessions: make(map[string]*session), secure: secure}
}

// get returns the session named by the request cookie, minting a new one when
// the cookie is missing, unknown or expired.
func (st *store) get(w http.ResponseWriter, r *http.Request) *session {
	now := time.Now()

	if c, err := r.Cookie(cookieName); err == nil {
		st.mu.Lock()
		s, ok := st.sessions[c.Value]
		if ok && s.expires.After(now) {
			s.expires = now.Add(sessionTTL)
			st.mu.Unlock()
			return s
		}
		st.mu.Unlock()
	}

	id, err := randomHex(32)
	if err != nil {
		// crypto/rand failing means we cannot produce an unguessable session
		// id, and a guessable one is worse than none.
		panic("reading random bytes: " + err.Error())
	}
	s := &session{expires: now.Add(sessionTTL)}

	st.mu.Lock()
	st.sessions[id] = s
	st.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:  cookieName,
		Value: id,
		Path:  "/",
		// Lax, not Strict: the provider sends the browser back to /callback
		// as a top-level navigation from another origin, and Strict would
		// withhold the cookie there — losing the state we are about to check.
		SameSite: http.SameSiteLaxMode,
		HttpOnly: true,
		Secure:   st.secure,
		MaxAge:   int(sessionTTL / time.Second),
	})
	return s
}

func (st *store) sweep(now time.Time) {
	st.mu.Lock()
	defer st.mu.Unlock()
	for id, s := range st.sessions {
		if s.expires.Before(now) {
			delete(st.sessions, id)
		}
	}
}

func (st *store) janitor(every time.Duration) {
	for range time.Tick(every) {
		st.sweep(time.Now())
	}
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// fingerprint shortens an opaque token to something that can be compared by eye
// — enough to see that a refresh token rotated, without putting the whole thing
// on the page.
func fingerprint(s string) string {
	if len(s) <= 14 {
		return s
	}
	return s[:8] + "…" + s[len(s)-4:]
}

func secureCookies(redirect string) bool {
	return strings.HasPrefix(redirect, "https://")
}
