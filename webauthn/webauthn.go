// Package webauthn verifies WebAuthn (FIDO2) registration and
// authentication ceremonies on the relying-party side, using only the
// standard library.
//
// Scope is deliberately narrow: the server half of the two ceremonies, for
// a relying party that requests attestation "none". The browser half
// (navigator.credentials.create/get), challenge storage, user and
// credential bookkeeping all belong to the application. In particular:
//
//   - Challenges are minted by NewChallenge, but storing one and making it
//     single-use and short-lived is the caller's job -- this package
//     checks only that the challenge the client signed equals the one it
//     is given.
//   - Attestation statements are not verified. A relying party that asks
//     for attestation "none" has said it does not care which authenticator
//     model created the key; verifying a statement it never asked for
//     would add trust-anchor management for no gain. VerifyRegistration
//     accepts any attestation format and ignores attStmt.
//   - Client extension outputs (PRF, credProps, ...) are reported by the
//     browser in getClientExtensionResults(), outside anything the
//     authenticator signs, so they never reach this package.
//
// Typical flow:
//
//	rp := webauthn.RelyingParty{ID: "example.com", Origin: "https://example.com", RequireUserVerification: true}
//	ch, _ := webauthn.NewChallenge()          // store it server-side, single use
//	// ... browser runs navigator.credentials.create({publicKey: {challenge: ch, ...}})
//	cred, err := rp.VerifyRegistration(ch, clientDataJSON, attestationObject)
//	// persist cred.ID, cred.PublicKey, cred.SignCount
//
//	ch, _ = webauthn.NewChallenge()
//	// ... browser runs navigator.credentials.get({publicKey: {challenge: ch, allowCredentials: [...]}})
//	res, err := rp.VerifyAssertion(ch, stored, clientDataJSON, authenticatorData, signature)
//	// persist res.SignCount
package webauthn

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
)

// ChallengeSize is the length NewChallenge produces. The spec asks for at
// least 16 random bytes; 32 matches the hash size used everywhere else.
const ChallengeSize = 32

// maxCredentialIDLen is the WebAuthn Level 3 cap on credential IDs.
const maxCredentialIDLen = 1023

// Authenticator data flag bits (WebAuthn §6.1).
const (
	flagUP = 0x01 // user present
	flagUV = 0x04 // user verified
	flagBE = 0x08 // backup eligible
	flagBS = 0x10 // backed up
	flagAT = 0x40 // attested credential data included
	flagED = 0x80 // extension data included
)

// RelyingParty holds the values every ceremony is checked against.
type RelyingParty struct {
	// ID is the RP ID: a registrable domain suffix of the origin's host,
	// e.g. "example.com" or "vault.example.com" ("localhost" for local
	// development). Authenticator data must carry SHA-256(ID).
	ID string

	// Origin is the exact web origin the browser reports in
	// clientDataJSON, scheme and host (and port, if non-default), e.g.
	// "https://vault.example.com". Compared byte for byte.
	Origin string

	// RequireUserVerification rejects any ceremony whose authenticator
	// data lacks the UV flag (PIN, biometric). Set it when the browser
	// side requests userVerification "required"; the browser's request
	// alone is not a guarantee the server can rely on.
	RequireUserVerification bool
}

// Credential is what a successful registration yields: everything the
// relying party must persist to verify later assertions.
type Credential struct {
	// ID is the credential ID, as the browser reports it in rawId and as
	// it must be listed in allowCredentials.
	ID []byte
	// PublicKey is the credential public key in COSE_Key form; pass it to
	// ParsePublicKey (VerifyAssertion does).
	PublicKey []byte
	// Alg is the key's COSE algorithm.
	Alg int64
	// SignCount is the authenticator's signature counter at registration.
	// Many platform authenticators (passkeys) always report 0.
	SignCount uint32
	// AAGUID identifies the authenticator model (all zero under
	// attestation "none" on most platforms). Informational only.
	AAGUID []byte
	// UserVerified reports the UV flag of the registration ceremony.
	UserVerified bool
	// BackupEligible and BackedUp report the BE/BS flags: a synced passkey
	// is eligible and usually backed up, a hardware key is neither.
	BackupEligible bool
	BackedUp       bool
}

// StoredCredential is the subset of a persisted credential that
// VerifyAssertion needs.
type StoredCredential struct {
	ID        []byte
	PublicKey []byte
	SignCount uint32
}

// AssertionResult is what a successful assertion yields.
type AssertionResult struct {
	// SignCount is the authenticator's new counter; persist it so the
	// next assertion can be checked for regression.
	SignCount    uint32
	UserVerified bool
	BackedUp     bool
}

// ErrSignCount is returned (wrapped) by VerifyAssertion when the
// authenticator's signature counter did not advance past the stored one
// -- the spec's signal that the credential may have been cloned.
var ErrSignCount = errors.New("webauthn: signature counter did not increase")

// NewChallenge returns ChallengeSize fresh random bytes from crypto/rand.
func NewChallenge() ([]byte, error) {
	b := make([]byte, ChallengeSize)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, nil
}

// clientData is the subset of CollectedClientData the checks need.
type clientData struct {
	Type        string `json:"type"`
	Challenge   string `json:"challenge"`
	Origin      string `json:"origin"`
	CrossOrigin bool   `json:"crossOrigin"`
}

func (rp RelyingParty) checkClientData(raw []byte, wantType string, challenge []byte) error {
	if rp.ID == "" || rp.Origin == "" {
		return errors.New("webauthn: RelyingParty needs ID and Origin")
	}
	if len(challenge) < 16 {
		return errors.New("webauthn: challenge shorter than 16 bytes")
	}
	var cd clientData
	if err := json.Unmarshal(raw, &cd); err != nil {
		return fmt.Errorf("webauthn: clientDataJSON: %w", err)
	}
	if cd.Type != wantType {
		return fmt.Errorf("webauthn: clientData type %q, want %q", cd.Type, wantType)
	}
	got, err := base64.RawURLEncoding.DecodeString(cd.Challenge)
	if err != nil {
		return errors.New("webauthn: clientData challenge is not base64url")
	}
	if subtle.ConstantTimeCompare(got, challenge) != 1 {
		return errors.New("webauthn: challenge mismatch")
	}
	if cd.Origin != rp.Origin {
		return fmt.Errorf("webauthn: origin %q, want %q", cd.Origin, rp.Origin)
	}
	if cd.CrossOrigin {
		return errors.New("webauthn: cross-origin ceremony rejected")
	}
	return nil
}

// authData is parsed authenticator data (WebAuthn §6.1).
type authData struct {
	rpIDHash  []byte
	flags     byte
	signCount uint32
	aaguid    []byte
	credID    []byte
	credKey   []byte // COSE_Key bytes
}

func parseAuthData(b []byte) (*authData, error) {
	if len(b) < 37 {
		return nil, errors.New("webauthn: authenticator data too short")
	}
	ad := &authData{
		rpIDHash:  b[:32],
		flags:     b[32],
		signCount: binary.BigEndian.Uint32(b[33:37]),
	}
	rest := b[37:]
	if ad.flags&flagAT != 0 {
		if len(rest) < 18 {
			return nil, errors.New("webauthn: attested credential data truncated")
		}
		ad.aaguid = rest[:16]
		n := int(binary.BigEndian.Uint16(rest[16:18]))
		rest = rest[18:]
		if n == 0 || n > maxCredentialIDLen || n > len(rest) {
			return nil, errors.New("webauthn: bad credential ID length")
		}
		ad.credID = rest[:n]
		rest = rest[n:]
		_, after, err := cborDecodeFirst(rest)
		if err != nil {
			return nil, fmt.Errorf("webauthn: credential public key: %w", err)
		}
		ad.credKey = rest[:len(rest)-len(after)]
		rest = after
	}
	if ad.flags&flagED != 0 {
		if _, err := cborDecode(rest); err != nil {
			return nil, fmt.Errorf("webauthn: extensions: %w", err)
		}
		rest = nil
	}
	if len(rest) != 0 {
		return nil, errors.New("webauthn: trailing bytes in authenticator data")
	}
	return ad, nil
}

func (rp RelyingParty) checkAuthData(ad *authData) error {
	want := sha256.Sum256([]byte(rp.ID))
	if !bytes.Equal(ad.rpIDHash, want[:]) {
		return errors.New("webauthn: RP ID hash mismatch")
	}
	if ad.flags&flagUP == 0 {
		return errors.New("webauthn: user presence flag not set")
	}
	if rp.RequireUserVerification && ad.flags&flagUV == 0 {
		return errors.New("webauthn: user verification required but not performed")
	}
	if ad.flags&flagBE == 0 && ad.flags&flagBS != 0 {
		return errors.New("webauthn: backup state set without backup eligibility")
	}
	return nil
}

// VerifyRegistration checks a navigator.credentials.create() response --
// the raw clientDataJSON and attestationObject the browser returned --
// against challenge, and returns the new credential.
func (rp RelyingParty) VerifyRegistration(challenge, clientDataJSON, attestationObject []byte) (*Credential, error) {
	if err := rp.checkClientData(clientDataJSON, "webauthn.create", challenge); err != nil {
		return nil, err
	}
	v, err := cborDecode(attestationObject)
	if err != nil {
		return nil, fmt.Errorf("webauthn: attestationObject: %w", err)
	}
	att, ok := v.(map[any]any)
	if !ok {
		return nil, errors.New("webauthn: attestationObject is not a map")
	}
	if _, ok := att["fmt"].(string); !ok {
		return nil, errors.New("webauthn: attestationObject has no fmt")
	}
	if _, ok := att["attStmt"].(map[any]any); !ok {
		return nil, errors.New("webauthn: attestationObject has no attStmt")
	}
	raw, ok := att["authData"].([]byte)
	if !ok {
		return nil, errors.New("webauthn: attestationObject has no authData")
	}
	ad, err := parseAuthData(raw)
	if err != nil {
		return nil, err
	}
	if err := rp.checkAuthData(ad); err != nil {
		return nil, err
	}
	if ad.flags&flagAT == 0 {
		return nil, errors.New("webauthn: registration without attested credential data")
	}
	pub, err := ParsePublicKey(ad.credKey)
	if err != nil {
		return nil, err
	}
	return &Credential{
		ID:             append([]byte(nil), ad.credID...),
		PublicKey:      append([]byte(nil), ad.credKey...),
		Alg:            pub.Alg,
		SignCount:      ad.signCount,
		AAGUID:         append([]byte(nil), ad.aaguid...),
		UserVerified:   ad.flags&flagUV != 0,
		BackupEligible: ad.flags&flagBE != 0,
		BackedUp:       ad.flags&flagBS != 0,
	}, nil
}

// VerifyAssertion checks a navigator.credentials.get() response -- the
// raw clientDataJSON, authenticatorData and signature -- made with the
// stored credential, against challenge. The caller has already matched
// the response's rawId to cred (by looking cred up by that ID).
//
// The signature counter must advance unless both the stored and the new
// value are 0 (authenticators without a counter, i.e. most passkeys); a
// regression returns an error wrapping ErrSignCount.
func (rp RelyingParty) VerifyAssertion(challenge []byte, cred StoredCredential, clientDataJSON, authenticatorData, signature []byte) (*AssertionResult, error) {
	if err := rp.checkClientData(clientDataJSON, "webauthn.get", challenge); err != nil {
		return nil, err
	}
	ad, err := parseAuthData(authenticatorData)
	if err != nil {
		return nil, err
	}
	if ad.flags&flagAT != 0 {
		return nil, errors.New("webauthn: assertion must not carry attested credential data")
	}
	if err := rp.checkAuthData(ad); err != nil {
		return nil, err
	}
	pub, err := ParsePublicKey(cred.PublicKey)
	if err != nil {
		return nil, err
	}
	cdHash := sha256.Sum256(clientDataJSON)
	signed := make([]byte, 0, len(authenticatorData)+len(cdHash))
	signed = append(append(signed, authenticatorData...), cdHash[:]...)
	if err := pub.Verify(signed, signature); err != nil {
		return nil, err
	}
	if (ad.signCount != 0 || cred.SignCount != 0) && ad.signCount <= cred.SignCount {
		return nil, fmt.Errorf("%w (stored %d, got %d)", ErrSignCount, cred.SignCount, ad.signCount)
	}
	return &AssertionResult{
		SignCount:    ad.signCount,
		UserVerified: ad.flags&flagUV != 0,
		BackedUp:     ad.flags&flagBS != 0,
	}, nil
}
