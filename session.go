package loom

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

// SessionConfig configures NewSessions.
type SessionConfig struct {
	// IdleTimeout ends a session that has not been used (Lookup) for this
	// long. Required: NewSessions panics if it is not positive.
	IdleTimeout time.Duration

	// MaxLifetime ends a session this long after Create, however active.
	// Required: NewSessions panics if it is not positive, or shorter than
	// IdleTimeout (which would make IdleTimeout meaningless).
	MaxLifetime time.Duration

	// Secure sets the cookie's Secure attribute. Gate it per deployment,
	// the same way CSRFConfig.Secure is gated -- local plain-HTTP
	// development never stores a Secure cookie. When Secure is set the
	// cookie is named with the "__Host-" prefix, which makes the browser
	// refuse it unless it is Secure, Path=/ and has no Domain: a sibling
	// subdomain can then neither set nor shadow it.
	Secure bool

	// CookieName overrides the cookie name. Default "__Host-session" when
	// Secure, "session" otherwise.
	CookieName string
}

// Sessions is an in-memory, server-side session store: the cookie
// carries only an opaque 256-bit random token, and the identity it maps
// to never leaves the server. Every session is lost when the process
// exits -- the trade this store makes for having nothing to persist,
// encrypt or rotate. It suits a single-instance service; a deployment
// with replicas needs a shared store instead.
//
// Tokens are kept only as SHA-256 hashes, so a memory or heap dump of the
// store doesn't hand out live cookies.
//
// The cookie is HttpOnly, SameSite=Strict, Path=/, with Max-Age equal to
// MaxLifetime. Strict means the browser omits it on every cross-site
// navigation, including a link followed from another site; an app that
// must survive such entry points should land them on a page that needs
// no session.
//
// Sessions is safe for concurrent use. Lookup has the signature of the
// authenticate function App.Authentication and
// AuthenticationRedirectConfig.Authenticate take, so it can be passed to
// either directly.
type Sessions struct {
	cfg  SessionConfig
	now  func() time.Time
	mu   sync.Mutex
	byID map[[32]byte]*session
}

type session struct {
	identity string
	created  time.Time
	lastUsed time.Time
}

// NewSessions returns an empty store. It panics on a misconfigured cfg,
// following loom's panic-at-setup convention (see App.CSRF).
func NewSessions(cfg SessionConfig) *Sessions {
	if cfg.IdleTimeout <= 0 || cfg.MaxLifetime <= 0 {
		panic("loom: SessionConfig.IdleTimeout and MaxLifetime must be positive")
	}
	if cfg.MaxLifetime < cfg.IdleTimeout {
		panic("loom: SessionConfig.MaxLifetime must not be shorter than IdleTimeout")
	}
	if cfg.CookieName == "" {
		cfg.CookieName = "session"
		if cfg.Secure {
			cfg.CookieName = "__Host-session"
		}
	}
	return &Sessions{cfg: cfg, now: time.Now, byID: map[[32]byte]*session{}}
}

// CookieName reports the name of the session cookie.
func (s *Sessions) CookieName() string { return s.cfg.CookieName }

// Create starts a session for identity and sets its cookie on w. Any
// session the request already carried should be destroyed first by the
// caller (Destroy) -- Create never reuses a token, which is what prevents
// session fixation.
func (s *Sessions) Create(w http.ResponseWriter, identity string) error {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()

	s.mu.Lock()
	s.sweepLocked(now)
	s.byID[sha256.Sum256([]byte(token))] = &session{identity: identity, created: now, lastUsed: now}
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.CookieName,
		Value:    token,
		Path:     "/",
		MaxAge:   int(s.cfg.MaxLifetime / time.Second),
		HttpOnly: true,
		Secure:   s.cfg.Secure,
		SameSite: http.SameSiteStrictMode,
	})
	return nil
}

// Lookup returns the identity of r's session, and whether it has one that
// is still live. A live lookup counts as use and restarts the idle timer.
func (s *Sessions) Lookup(r *http.Request) (identity string, ok bool) {
	c, err := r.Cookie(s.cfg.CookieName)
	if err != nil || c.Value == "" {
		return "", false
	}
	key := sha256.Sum256([]byte(c.Value))
	now := s.now()

	s.mu.Lock()
	defer s.mu.Unlock()
	sess, found := s.byID[key]
	if !found {
		return "", false
	}
	if s.expired(sess, now) {
		delete(s.byID, key)
		return "", false
	}
	sess.lastUsed = now
	return sess.identity, true
}

// Destroy ends r's session, if any, and clears its cookie on w.
func (s *Sessions) Destroy(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(s.cfg.CookieName); err == nil {
		key := sha256.Sum256([]byte(c.Value))
		s.mu.Lock()
		delete(s.byID, key)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.Secure,
		SameSite: http.SameSiteStrictMode,
	})
}

// DestroyIdentity ends every session belonging to identity, e.g. after
// one of its credentials is revoked. It reports how many it ended.
func (s *Sessions) DestroyIdentity(identity string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for k, sess := range s.byID {
		if sess.identity == identity {
			delete(s.byID, k)
			n++
		}
	}
	return n
}

// Len reports how many sessions are live.
func (s *Sessions) Len() int {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	return len(s.byID)
}

func (s *Sessions) expired(sess *session, now time.Time) bool {
	return now.Sub(sess.lastUsed) >= s.cfg.IdleTimeout || now.Sub(sess.created) >= s.cfg.MaxLifetime
}

// sweepLocked drops expired sessions. Run on every Create, so the map is
// bounded by the number of sessions started within one MaxLifetime
// without a background goroutine to start and stop.
func (s *Sessions) sweepLocked(now time.Time) {
	for k, sess := range s.byID {
		if s.expired(sess, now) {
			delete(s.byID, k)
		}
	}
}
