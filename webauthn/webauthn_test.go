package webauthn_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zcecc22/loom/webauthn"
	"github.com/zcecc22/loom/webauthn/webauthntest"
)

const (
	rpID   = "vault.example.com"
	origin = "https://vault.example.com"
)

var rp = webauthn.RelyingParty{ID: rpID, Origin: origin, RequireUserVerification: true}

func challenge(t *testing.T) []byte {
	t.Helper()
	c, err := webauthn.NewChallenge()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func register(t *testing.T, a *webauthntest.Authenticator) *webauthn.Credential {
	t.Helper()
	ch := challenge(t)
	reg, err := a.Create(ch)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := rp.VerifyRegistration(ch, reg.ClientDataJSON, reg.AttestationObject)
	if err != nil {
		t.Fatalf("VerifyRegistration: %v", err)
	}
	return cred
}

func stored(c *webauthn.Credential) webauthn.StoredCredential {
	return webauthn.StoredCredential{ID: c.ID, PublicKey: c.PublicKey, SignCount: c.SignCount}
}

func TestRegisterThenAssert(t *testing.T) {
	a := webauthntest.New(rpID, origin)
	cred := register(t, a)
	if cred.Alg != webauthn.AlgES256 || !cred.UserVerified || len(cred.ID) != 32 {
		t.Fatalf("credential %+v", cred)
	}

	sc := stored(cred)
	for i := 1; i <= 3; i++ {
		ch := challenge(t)
		as, err := a.Get(ch, cred.ID)
		if err != nil {
			t.Fatal(err)
		}
		res, err := rp.VerifyAssertion(ch, sc, as.ClientDataJSON, as.AuthenticatorData, as.Signature)
		if err != nil {
			t.Fatalf("assertion %d: %v", i, err)
		}
		if res.SignCount != uint32(i) {
			t.Fatalf("assertion %d: counter %d", i, res.SignCount)
		}
		sc.SignCount = res.SignCount
	}
}

func TestPasskeyZeroCounterAccepted(t *testing.T) {
	a := webauthntest.New(rpID, origin)
	a.FixedCounter = true
	cred := register(t, a)
	for i := 0; i < 2; i++ {
		ch := challenge(t)
		as, _ := a.Get(ch, cred.ID)
		if _, err := rp.VerifyAssertion(ch, stored(cred), as.ClientDataJSON, as.AuthenticatorData, as.Signature); err != nil {
			t.Fatalf("zero-counter assertion rejected: %v", err)
		}
	}
}

func TestCounterRegressionRejected(t *testing.T) {
	a := webauthntest.New(rpID, origin)
	cred := register(t, a)
	ch := challenge(t)
	as, _ := a.Get(ch, cred.ID) // counter 1
	sc := stored(cred)
	sc.SignCount = 5
	_, err := rp.VerifyAssertion(ch, sc, as.ClientDataJSON, as.AuthenticatorData, as.Signature)
	if !errors.Is(err, webauthn.ErrSignCount) {
		t.Fatalf("got %v, want ErrSignCount", err)
	}
}

func TestRegistrationRejections(t *testing.T) {
	t.Run("challenge mismatch", func(t *testing.T) {
		a := webauthntest.New(rpID, origin)
		reg, _ := a.Create(challenge(t))
		if _, err := rp.VerifyRegistration(challenge(t), reg.ClientDataJSON, reg.AttestationObject); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("wrong origin", func(t *testing.T) {
		a := webauthntest.New(rpID, "https://evil.example.com")
		ch := challenge(t)
		reg, _ := a.Create(ch)
		if _, err := rp.VerifyRegistration(ch, reg.ClientDataJSON, reg.AttestationObject); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("wrong rp id", func(t *testing.T) {
		a := webauthntest.New("example.com", origin)
		ch := challenge(t)
		reg, _ := a.Create(ch)
		if _, err := rp.VerifyRegistration(ch, reg.ClientDataJSON, reg.AttestationObject); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("no user verification", func(t *testing.T) {
		a := webauthntest.New(rpID, origin)
		a.UserVerified = false
		ch := challenge(t)
		reg, _ := a.Create(ch)
		if _, err := rp.VerifyRegistration(ch, reg.ClientDataJSON, reg.AttestationObject); err == nil {
			t.Fatal("accepted")
		}
		lax := rp
		lax.RequireUserVerification = false
		if _, err := lax.VerifyRegistration(ch, reg.ClientDataJSON, reg.AttestationObject); err != nil {
			t.Fatalf("UV not required, still rejected: %v", err)
		}
	})
	t.Run("no user presence", func(t *testing.T) {
		a := webauthntest.New(rpID, origin)
		a.NoUserPresence = true
		ch := challenge(t)
		reg, _ := a.Create(ch)
		if _, err := rp.VerifyRegistration(ch, reg.ClientDataJSON, reg.AttestationObject); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("get clientData used for create", func(t *testing.T) {
		a := webauthntest.New(rpID, origin)
		ch := challenge(t)
		reg, _ := a.Create(ch)
		as, _ := a.Get(ch, reg.ID)
		if _, err := rp.VerifyRegistration(ch, as.ClientDataJSON, reg.AttestationObject); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("cross origin", func(t *testing.T) {
		a := webauthntest.New(rpID, origin)
		ch := challenge(t)
		reg, _ := a.Create(ch)
		cd := map[string]any{}
		_ = json.Unmarshal(reg.ClientDataJSON, &cd)
		cd["crossOrigin"] = true
		raw, _ := json.Marshal(cd)
		if _, err := rp.VerifyRegistration(ch, raw, reg.AttestationObject); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("garbage attestation object", func(t *testing.T) {
		a := webauthntest.New(rpID, origin)
		ch := challenge(t)
		reg, _ := a.Create(ch)
		if _, err := rp.VerifyRegistration(ch, reg.ClientDataJSON, reg.AttestationObject[:len(reg.AttestationObject)-1]); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("short challenge", func(t *testing.T) {
		a := webauthntest.New(rpID, origin)
		ch := []byte("short")
		reg, _ := a.Create(ch)
		if _, err := rp.VerifyRegistration(ch, reg.ClientDataJSON, reg.AttestationObject); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("unconfigured relying party", func(t *testing.T) {
		a := webauthntest.New(rpID, origin)
		ch := challenge(t)
		reg, _ := a.Create(ch)
		if _, err := (webauthn.RelyingParty{}).VerifyRegistration(ch, reg.ClientDataJSON, reg.AttestationObject); err == nil {
			t.Fatal("accepted")
		}
	})
}

func TestAssertionRejections(t *testing.T) {
	a := webauthntest.New(rpID, origin)
	cred := register(t, a)
	other := webauthntest.New(rpID, origin)
	otherCred := register(t, other)

	t.Run("wrong challenge", func(t *testing.T) {
		as, _ := a.Get(challenge(t), cred.ID)
		if _, err := rp.VerifyAssertion(challenge(t), stored(cred), as.ClientDataJSON, as.AuthenticatorData, as.Signature); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("signed by another key", func(t *testing.T) {
		ch := challenge(t)
		as, _ := other.Get(ch, otherCred.ID)
		if _, err := rp.VerifyAssertion(ch, stored(cred), as.ClientDataJSON, as.AuthenticatorData, as.Signature); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("tampered authenticator data", func(t *testing.T) {
		ch := challenge(t)
		as, _ := a.Get(ch, cred.ID)
		as.AuthenticatorData[36] ^= 0x7f // counter byte: still well-formed, signature now wrong
		if _, err := rp.VerifyAssertion(ch, stored(cred), as.ClientDataJSON, as.AuthenticatorData, as.Signature); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("tampered client data", func(t *testing.T) {
		ch := challenge(t)
		as, _ := a.Get(ch, cred.ID)
		cd := map[string]any{}
		_ = json.Unmarshal(as.ClientDataJSON, &cd)
		cd["extra"] = "x"
		raw, _ := json.Marshal(cd)
		if _, err := rp.VerifyAssertion(ch, stored(cred), raw, as.AuthenticatorData, as.Signature); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("create clientData used for get", func(t *testing.T) {
		ch := challenge(t)
		as, _ := a.Get(ch, cred.ID)
		cd, _ := json.Marshal(map[string]any{"type": "webauthn.create", "challenge": base64.RawURLEncoding.EncodeToString(ch), "origin": origin})
		if _, err := rp.VerifyAssertion(ch, stored(cred), cd, as.AuthenticatorData, as.Signature); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("truncated authenticator data", func(t *testing.T) {
		ch := challenge(t)
		as, _ := a.Get(ch, cred.ID)
		if _, err := rp.VerifyAssertion(ch, stored(cred), as.ClientDataJSON, as.AuthenticatorData[:36], as.Signature); err == nil {
			t.Fatal("accepted")
		}
	})
	t.Run("no user verification", func(t *testing.T) {
		a.UserVerified = false
		defer func() { a.UserVerified = true }()
		ch := challenge(t)
		as, _ := a.Get(ch, cred.ID)
		if _, err := rp.VerifyAssertion(ch, stored(cred), as.ClientDataJSON, as.AuthenticatorData, as.Signature); err == nil {
			t.Fatal("accepted")
		}
	})
}

func TestPRFIsStablePerCredentialAndSalt(t *testing.T) {
	a := webauthntest.New(rpID, origin)
	c1 := register(t, a)
	c2 := register(t, a)
	salt := sha256.Sum256([]byte("salt"))
	p1, _ := a.PRF(c1.ID, salt[:])
	p1again, _ := a.PRF(c1.ID, salt[:])
	p2, _ := a.PRF(c2.ID, salt[:])
	if string(p1) != string(p1again) || string(p1) == string(p2) || len(p1) != 32 {
		t.Fatal("PRF output not stable per credential, or not distinct across credentials")
	}
}
