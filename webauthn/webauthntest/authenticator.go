// Package webauthntest provides a software WebAuthn authenticator for
// tests, in the spirit of net/http/httptest: it produces the exact byte
// structures a browser hands back from navigator.credentials.create() and
// .get(), so a relying party's ceremony handlers can be exercised end to
// end without hardware.
//
// It also simulates the PRF extension (hmac-secret): PRF returns a
// per-credential HMAC-SHA256 of the salt, the same shape a real
// authenticator's output has, so client-side key-derivation code can be
// tested against stable values.
//
// Never use it outside tests: keys live in memory, unprotected.
package webauthntest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sort"
	"sync"
)

// Authenticator is a software authenticator holding any number of ES256
// credentials. The zero value is not usable; call New.
type Authenticator struct {
	// RPID and Origin are what the simulated browser reports. Change
	// Origin to test origin checks.
	RPID   string
	Origin string
	// UserVerified sets the UV flag (default true from New).
	UserVerified bool
	// NoUserPresence clears the UP flag, for negative tests.
	NoUserPresence bool
	// FixedCounter, when true, always reports a signature counter of 0
	// (as passkeys do) instead of incrementing.
	FixedCounter bool

	mu    sync.Mutex
	creds map[string]*credential
}

type credential struct {
	key     *ecdsa.PrivateKey
	prf     []byte
	counter uint32
}

// New returns an Authenticator for rpID/origin with user verification on.
func New(rpID, origin string) *Authenticator {
	return &Authenticator{RPID: rpID, Origin: origin, UserVerified: true, creds: map[string]*credential{}}
}

// Registration is what navigator.credentials.create() yields.
type Registration struct {
	ID                []byte
	ClientDataJSON    []byte
	AttestationObject []byte
}

// Assertion is what navigator.credentials.get() yields.
type Assertion struct {
	ID                []byte
	ClientDataJSON    []byte
	AuthenticatorData []byte
	Signature         []byte
}

// Create makes a new credential bound to challenge and returns the
// registration response (attestation format "none").
func (a *Authenticator) Create(challenge []byte) (*Registration, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	id := make([]byte, 32)
	prf := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		return nil, err
	}
	if _, err := rand.Read(prf); err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.creds[string(id)] = &credential{key: key, prf: prf}
	a.mu.Unlock()

	cd := a.clientData("webauthn.create", challenge)
	pub, err := key.PublicKey.Bytes() // 0x04 || X || Y
	if err != nil {
		return nil, err
	}
	coseKey := encMap([][2][]byte{
		{encInt(1), encInt(2)},            // kty: EC2
		{encInt(3), encInt(-7)},           // alg: ES256
		{encInt(-1), encInt(1)},           // crv: P-256
		{encInt(-2), encBytes(pub[1:33])}, // x
		{encInt(-3), encBytes(pub[33:])},  // y
	})
	ad := a.authData(0x40, 0)
	ad = append(ad, make([]byte, 16)...) // AAGUID: zero, as under attestation none
	ad = binary.BigEndian.AppendUint16(ad, uint16(len(id)))
	ad = append(ad, id...)
	ad = append(ad, coseKey...)

	att := encMap([][2][]byte{
		{encText("fmt"), encText("none")},
		{encText("attStmt"), encMap(nil)},
		{encText("authData"), encBytes(ad)},
	})
	return &Registration{ID: id, ClientDataJSON: cd, AttestationObject: att}, nil
}

// Get signs challenge with the credential id and returns the assertion
// response. Each call advances the credential's counter unless
// FixedCounter is set.
func (a *Authenticator) Get(challenge, id []byte) (*Assertion, error) {
	a.mu.Lock()
	c, ok := a.creds[string(id)]
	if ok && !a.FixedCounter {
		c.counter++
	}
	var counter uint32
	if ok {
		counter = c.counter
	}
	a.mu.Unlock()
	if !ok {
		return nil, errors.New("webauthntest: unknown credential")
	}
	cd := a.clientData("webauthn.get", challenge)
	ad := a.authData(0, counter)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte(nil), ad...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, c.key, digest[:])
	if err != nil {
		return nil, err
	}
	return &Assertion{ID: append([]byte(nil), id...), ClientDataJSON: cd, AuthenticatorData: ad, Signature: sig}, nil
}

// PRF returns the simulated PRF (hmac-secret) output of credential id
// for salt: HMAC-SHA256 under a per-credential secret, 32 bytes.
func (a *Authenticator) PRF(id, salt []byte) ([]byte, error) {
	a.mu.Lock()
	c, ok := a.creds[string(id)]
	a.mu.Unlock()
	if !ok {
		return nil, errors.New("webauthntest: unknown credential")
	}
	m := hmac.New(sha256.New, c.prf)
	m.Write(salt)
	return m.Sum(nil), nil
}

// IDs returns every credential ID the authenticator holds, sorted.
func (a *Authenticator) IDs() [][]byte {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([][]byte, 0, len(a.creds))
	for k := range a.creds {
		ids = append(ids, []byte(k))
	}
	sort.Slice(ids, func(i, j int) bool { return string(ids[i]) < string(ids[j]) })
	return ids
}

func (a *Authenticator) clientData(typ string, challenge []byte) []byte {
	b, _ := json.Marshal(map[string]any{
		"type":        typ,
		"challenge":   base64.RawURLEncoding.EncodeToString(challenge),
		"origin":      a.Origin,
		"crossOrigin": false,
	})
	return b
}

func (a *Authenticator) authData(extraFlags byte, counter uint32) []byte {
	h := sha256.Sum256([]byte(a.RPID))
	flags := extraFlags
	if !a.NoUserPresence {
		flags |= 0x01
	}
	if a.UserVerified {
		flags |= 0x04
	}
	b := append([]byte(nil), h[:]...)
	b = append(b, flags)
	return binary.BigEndian.AppendUint32(b, counter)
}

// --- minimal CBOR encoding: only what Create needs ---------------------

func encHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n <= 0xff:
		return []byte{major<<5 | 24, byte(n)}
	case n <= 0xffff:
		return binary.BigEndian.AppendUint16([]byte{major<<5 | 25}, uint16(n))
	default:
		return binary.BigEndian.AppendUint32([]byte{major<<5 | 26}, uint32(n))
	}
}

func encInt(v int64) []byte {
	if v >= 0 {
		return encHead(0, uint64(v))
	}
	return encHead(1, uint64(-1-v))
}

func encBytes(b []byte) []byte { return append(encHead(2, uint64(len(b))), b...) }
func encText(s string) []byte  { return append(encHead(3, uint64(len(s))), s...) }

func encMap(pairs [][2][]byte) []byte {
	b := encHead(5, uint64(len(pairs)))
	for _, p := range pairs {
		b = append(append(b, p[0]...), p[1]...)
	}
	return b
}
