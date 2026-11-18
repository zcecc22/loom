package webauthn

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

// Test-only CBOR encoding for building COSE keys.
func tHead(major byte, n uint64) []byte {
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

func tInt(v int64) []byte {
	if v >= 0 {
		return tHead(0, uint64(v))
	}
	return tHead(1, uint64(-1-v))
}

func tBytes(b []byte) []byte { return append(tHead(2, uint64(len(b))), b...) }

func tMap(kv ...[]byte) []byte {
	b := tHead(5, uint64(len(kv)/2))
	for _, x := range kv {
		b = append(b, x...)
	}
	return b
}

func coseES256(t *testing.T, k *ecdsa.PrivateKey) []byte {
	t.Helper()
	pub, err := k.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return tMap(tInt(1), tInt(2), tInt(3), tInt(-7), tInt(-1), tInt(1), tInt(-2), tBytes(pub[1:33]), tInt(-3), tBytes(pub[33:]))
}

func TestParseAndVerifyES256(t *testing.T) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub, err := ParsePublicKey(coseES256(t, k))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("signed data")
	d := sha256.Sum256(msg)
	sig, _ := ecdsa.SignASN1(rand.Reader, k, d[:])
	if err := pub.Verify(msg, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := pub.Verify([]byte("other data"), sig); err == nil {
		t.Fatal("signature over other data accepted")
	}
}

func TestParseAndVerifyEdDSA(t *testing.T) {
	pk, sk, _ := ed25519.GenerateKey(rand.Reader)
	pub, err := ParsePublicKey(tMap(tInt(1), tInt(1), tInt(3), tInt(-8), tInt(-1), tInt(6), tInt(-2), tBytes(pk)))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("signed data")
	if err := pub.Verify(msg, ed25519.Sign(sk, msg)); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := pub.Verify(msg, make([]byte, 64)); err == nil {
		t.Fatal("zero signature accepted")
	}
}

func TestParseAndVerifyRS256(t *testing.T) {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	e := []byte{0x01, 0x00, 0x01}
	pub, err := ParsePublicKey(tMap(tInt(1), tInt(3), tInt(3), tInt(-257), tInt(-1), tBytes(k.N.Bytes()), tInt(-2), tBytes(e)))
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte("signed data")
	d := sha256.Sum256(msg)
	sig, _ := rsa.SignPKCS1v15(rand.Reader, k, crypto.SHA256, d[:])
	if err := pub.Verify(msg, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	sig[0] ^= 1
	if err := pub.Verify(msg, sig); err == nil {
		t.Fatal("corrupted signature accepted")
	}
}

func TestParsePublicKeyRejects(t *testing.T) {
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	pub, _ := k.PublicKey.Bytes()
	x, y := pub[1:33], pub[33:]
	offCurve := append([]byte(nil), y...)
	offCurve[31] ^= 1
	pk, _, _ := ed25519.GenerateKey(rand.Reader)
	small, _ := rsa.GenerateKey(rand.Reader, 1024)

	cases := map[string][]byte{
		"not cbor":              {0xff},
		"not a map":             tInt(1),
		"no kty":                tMap(tInt(3), tInt(-7)),
		"no alg":                tMap(tInt(1), tInt(2)),
		"unsupported alg":       tMap(tInt(1), tInt(2), tInt(3), tInt(-35)),
		"ES256 with OKP kty":    tMap(tInt(1), tInt(1), tInt(3), tInt(-7), tInt(-1), tInt(1), tInt(-2), tBytes(x), tInt(-3), tBytes(y)),
		"ES256 on P-384 crv":    tMap(tInt(1), tInt(2), tInt(3), tInt(-7), tInt(-1), tInt(2), tInt(-2), tBytes(x), tInt(-3), tBytes(y)),
		"ES256 short x":         tMap(tInt(1), tInt(2), tInt(3), tInt(-7), tInt(-1), tInt(1), tInt(-2), tBytes(x[:31]), tInt(-3), tBytes(y)),
		"ES256 point off curve": tMap(tInt(1), tInt(2), tInt(3), tInt(-7), tInt(-1), tInt(1), tInt(-2), tBytes(x), tInt(-3), tBytes(offCurve)),
		"EdDSA on X25519 crv":   tMap(tInt(1), tInt(1), tInt(3), tInt(-8), tInt(-1), tInt(4), tInt(-2), tBytes(pk)),
		"EdDSA short key":       tMap(tInt(1), tInt(1), tInt(3), tInt(-8), tInt(-1), tInt(6), tInt(-2), tBytes(pk[:31])),
		"RS256 1024-bit":        tMap(tInt(1), tInt(3), tInt(3), tInt(-257), tInt(-1), tBytes(small.N.Bytes()), tInt(-2), tBytes([]byte{1, 0, 1})),
		"RS256 even exponent":   tMap(tInt(1), tInt(3), tInt(3), tInt(-257), tInt(-1), tBytes(make([]byte, 256)), tInt(-2), tBytes([]byte{2})),
	}
	for name, cose := range cases {
		if _, err := ParsePublicKey(cose); err == nil {
			t.Errorf("%s: accepted, want error", name)
		}
	}
}
