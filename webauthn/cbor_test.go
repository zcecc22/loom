package webauthn

import (
	"bytes"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Vectors from RFC 8949 Appendix A, restricted to the supported subset.
func TestCBORDecodeRFC8949Vectors(t *testing.T) {
	cases := []struct {
		hex  string
		want any
	}{
		{"00", int64(0)},
		{"17", int64(23)},
		{"1818", int64(24)},
		{"1903e8", int64(1000)},
		{"1a000f4240", int64(1000000)},
		{"1b000000e8d4a51000", int64(1000000000000)},
		{"20", int64(-1)},
		{"3863", int64(-100)},
		{"3903e7", int64(-1000)},
		{"f4", false},
		{"f5", true},
		{"f6", nil},
		{"40", []byte{}},
		{"4401020304", []byte{1, 2, 3, 4}},
		{"60", ""},
		{"6161", "a"},
		{"6449455446", "IETF"},
		{"62c3bc", "ü"},
		{"80", []any{}},
		{"83010203", []any{int64(1), int64(2), int64(3)}},
		{"8301820203820405", []any{int64(1), []any{int64(2), int64(3)}, []any{int64(4), int64(5)}}},
		{"a0", map[any]any{}},
		{"a201020304", map[any]any{int64(1): int64(2), int64(3): int64(4)}},
		{"a26161016162820203", map[any]any{"a": int64(1), "b": []any{int64(2), int64(3)}}},
	}
	for _, c := range cases {
		got, err := cborDecode(mustHex(t, c.hex))
		if err != nil {
			t.Errorf("%s: %v", c.hex, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: got %#v, want %#v", c.hex, got, c.want)
		}
	}
}

func TestCBORDecodeRejects(t *testing.T) {
	cases := map[string]string{
		"empty":                  "",
		"trailing byte":          "0000",
		"truncated uint16":       "19 03",
		"truncated bytes":        "44 0102",
		"huge byte length":       "5b ffffffffffffffff",
		"huge array count":       "9b ffffffffffffffff",
		"huge map count":         "bb ffffffffffffffff",
		"uint overflows int64":   "1b ffffffffffffffff",
		"indefinite bytes":       "5f 41 01 ff",
		"indefinite array":       "9f ff",
		"reserved info 28":       "1c",
		"tag":                    "c1 1a 514b67b0",
		"float16":                "f9 3c00",
		"float64":                "fb 3ff199999999999a",
		"undefined":              "f7",
		"duplicate map key":      "a2 01 02 01 03",
		"bytes map key":          "a1 41 00 01",
		"array map key":          "a1 80 01",
		"invalid utf-8 text":     "62 c3 28",
		"map missing value":      "a1 01",
		"array missing elements": "83 01 02",
	}
	for name, h := range cases {
		if _, err := cborDecode(mustHex(t, h)); err == nil {
			t.Errorf("%s (%s): accepted, want error", name, h)
		}
	}
}

func TestCBORDepthLimit(t *testing.T) {
	ok := append(bytes.Repeat([]byte{0x81}, cborMaxDepth), 0x00)
	if _, err := cborDecode(ok); err != nil {
		t.Fatalf("depth %d rejected: %v", cborMaxDepth, err)
	}
	deep := append(bytes.Repeat([]byte{0x81}, cborMaxDepth+1), 0x00)
	if _, err := cborDecode(deep); err == nil {
		t.Fatal("over-deep nesting accepted")
	}
}

func TestCBORDecodeFirstReturnsRemainder(t *testing.T) {
	v, rest, err := cborDecodeFirst(mustHex(t, "a10102 ff ee"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v, map[any]any{int64(1): int64(2)}) {
		t.Fatalf("value %#v", v)
	}
	if !bytes.Equal(rest, []byte{0xff, 0xee}) {
		t.Fatalf("rest %x", rest)
	}
}

func TestCBORByteStringsDoNotAliasInput(t *testing.T) {
	in := mustHex(t, "4401020304")
	v, err := cborDecode(in)
	if err != nil {
		t.Fatal(err)
	}
	in[1] = 0xff
	if v.([]byte)[0] != 1 {
		t.Fatal("decoded byte string aliases the input buffer")
	}
}
