package webauthn

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
)

// COSE algorithm identifiers (IANA "COSE Algorithms" registry) this
// package verifies. They are also what a relying party lists, in order of
// preference, in PublicKeyCredentialCreationOptions.pubKeyCredParams;
// SupportedAlgorithms is that list.
const (
	AlgES256 int64 = -7   // ECDSA P-256 with SHA-256
	AlgEdDSA int64 = -8   // Ed25519
	AlgRS256 int64 = -257 // RSASSA-PKCS1-v1_5 with SHA-256
)

// SupportedAlgorithms is every COSE algorithm ParsePublicKey accepts, in
// the order a relying party should offer them: ES256 first (every FIDO2
// authenticator and every platform passkey supports it), EdDSA next
// (YubiKey 5.2.3+), RS256 last (Windows Hello).
var SupportedAlgorithms = []int64{AlgES256, AlgEdDSA, AlgRS256}

// COSE_Key labels (RFC 9052 / RFC 9053).
const (
	coseKty    int64 = 1
	coseAlg    int64 = 3
	coseCrv    int64 = -1 // EC2/OKP curve
	coseX      int64 = -2 // EC2/OKP x
	coseY      int64 = -3 // EC2 y
	coseRSAN   int64 = -1 // RSA modulus
	coseRSAE   int64 = -2 // RSA exponent
	ktyOKP     int64 = 1
	ktyEC2     int64 = 2
	ktyRSA     int64 = 3
	crvP256    int64 = 1
	crvEd25519 int64 = 6
)

// minRSABits rejects toy RSA keys; every real authenticator uses 2048+.
const minRSABits = 2048

// PublicKey is a credential public key parsed from its COSE_Key encoding.
// Store the COSE bytes (Credential.PublicKey), not this value, and parse
// again when verifying: the COSE form is what the authenticator signed
// over and is stable across library versions.
type PublicKey struct {
	// Alg is the COSE algorithm the key is bound to.
	Alg int64
	key crypto.PublicKey
}

// ParsePublicKey parses a COSE_Key (as found in authenticator data's
// attested credential data) into a PublicKey. Only the algorithms in
// SupportedAlgorithms are accepted, and the key type and curve must match
// the algorithm: a key that claims ES256 but carries an Ed25519 point is
// rejected rather than guessed at.
func ParsePublicKey(cose []byte) (*PublicKey, error) {
	v, err := cborDecode(cose)
	if err != nil {
		return nil, fmt.Errorf("webauthn: COSE key: %w", err)
	}
	m, ok := v.(map[any]any)
	if !ok {
		return nil, errors.New("webauthn: COSE key is not a map")
	}
	kty, ok := mapInt(m, coseKty)
	if !ok {
		return nil, errors.New("webauthn: COSE key has no kty")
	}
	alg, ok := mapInt(m, coseAlg)
	if !ok {
		return nil, errors.New("webauthn: COSE key has no alg")
	}

	switch alg {
	case AlgES256:
		if kty != ktyEC2 {
			return nil, errors.New("webauthn: ES256 key is not EC2")
		}
		if crv, _ := mapInt(m, coseCrv); crv != crvP256 {
			return nil, errors.New("webauthn: ES256 key is not on P-256")
		}
		x, _ := mapBytes(m, coseX)
		y, _ := mapBytes(m, coseY)
		if len(x) != 32 || len(y) != 32 {
			return nil, errors.New("webauthn: ES256 coordinates must be 32 bytes")
		}
		// ParseUncompressedPublicKey checks the point is on the curve,
		// which constructing an ecdsa.PublicKey from big.Ints would not.
		raw := append(append([]byte{0x04}, x...), y...)
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
		if err != nil {
			return nil, fmt.Errorf("webauthn: ES256 key: %w", err)
		}
		return &PublicKey{Alg: alg, key: pub}, nil

	case AlgEdDSA:
		if kty != ktyOKP {
			return nil, errors.New("webauthn: EdDSA key is not OKP")
		}
		if crv, _ := mapInt(m, coseCrv); crv != crvEd25519 {
			return nil, errors.New("webauthn: EdDSA key is not Ed25519")
		}
		x, _ := mapBytes(m, coseX)
		if len(x) != ed25519.PublicKeySize {
			return nil, errors.New("webauthn: Ed25519 key must be 32 bytes")
		}
		return &PublicKey{Alg: alg, key: ed25519.PublicKey(x)}, nil

	case AlgRS256:
		if kty != ktyRSA {
			return nil, errors.New("webauthn: RS256 key is not RSA")
		}
		n, _ := mapBytes(m, coseRSAN)
		e, _ := mapBytes(m, coseRSAE)
		if len(n)*8 < minRSABits {
			return nil, fmt.Errorf("webauthn: RSA modulus under %d bits", minRSABits)
		}
		if len(e) == 0 || len(e) > 4 {
			return nil, errors.New("webauthn: RSA exponent out of range")
		}
		exp := 0
		for _, b := range e {
			exp = exp<<8 | int(b)
		}
		if exp < 3 || exp%2 == 0 {
			return nil, errors.New("webauthn: RSA exponent invalid")
		}
		return &PublicKey{Alg: alg, key: &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}}, nil

	default:
		return nil, fmt.Errorf("webauthn: unsupported COSE algorithm %d", alg)
	}
}

// Verify checks sig over data with the key's algorithm. ES256 signatures
// are ASN.1 DER, as WebAuthn specifies (not the raw r||s of JOSE).
func (k *PublicKey) Verify(data, sig []byte) error {
	switch pub := k.key.(type) {
	case *ecdsa.PublicKey:
		d := sha256.Sum256(data)
		if !ecdsa.VerifyASN1(pub, d[:], sig) {
			return errors.New("webauthn: invalid ES256 signature")
		}
	case ed25519.PublicKey:
		if !ed25519.Verify(pub, data, sig) {
			return errors.New("webauthn: invalid EdDSA signature")
		}
	case *rsa.PublicKey:
		d := sha256.Sum256(data)
		if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, d[:], sig); err != nil {
			return errors.New("webauthn: invalid RS256 signature")
		}
	default:
		return errors.New("webauthn: no key")
	}
	return nil
}

func mapInt(m map[any]any, k int64) (int64, bool) {
	v, ok := m[k].(int64)
	return v, ok
}

func mapBytes(m map[any]any, k int64) ([]byte, bool) {
	v, ok := m[k].([]byte)
	return v, ok
}
