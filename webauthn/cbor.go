package webauthn

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf8"
)

// A minimal, bounded CBOR (RFC 8949) decoder: exactly the subset WebAuthn
// puts on the wire -- the attestation object (a text-keyed map holding
// byte strings and a map) and the COSE_Key inside authenticator data (an
// integer-keyed map of integers and byte strings). It is deliberately not
// a general-purpose CBOR library and is unexported for that reason.
//
// Decoded values are:
//
//	unsigned / negative integer -> int64 (a value outside int64 is an error)
//	byte string                 -> []byte (a copy, never aliasing input)
//	text string                 -> string (must be valid UTF-8)
//	array                       -> []any
//	map                         -> map[any]any, keys int64 or string only
//	false / true / null         -> bool / bool / nil
//
// Rejected: indefinite lengths, tags, floats, undefined, simple values
// other than false/true/null, duplicate map keys, and nesting deeper than
// cborMaxDepth. Every length is checked against the remaining input
// before anything is allocated, so a hostile length prefix can't make the
// decoder allocate more than the input it was given.

// cborMaxDepth bounds nesting. The deepest legitimate WebAuthn structure
// (attestation object -> attStmt -> x5c array) is three levels.
const cborMaxDepth = 8

var errCBORTruncated = errors.New("cbor: unexpected end of input")

// cborDecode decodes exactly one CBOR data item from b and rejects any
// trailing bytes.
func cborDecode(b []byte) (any, error) {
	v, rest, err := cborDecodeFirst(b)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("cbor: %d trailing bytes", len(rest))
	}
	return v, nil
}

// cborDecodeFirst decodes one data item from the front of b and returns
// the remainder. Authenticator data needs this form: its COSE public key
// is followed directly by an optional extensions map, with no length
// prefix telling where the key ends.
func cborDecodeFirst(b []byte) (any, []byte, error) {
	d := cborDecoder{buf: b}
	v, err := d.value(0)
	if err != nil {
		return nil, nil, err
	}
	return v, d.buf[d.off:], nil
}

type cborDecoder struct {
	buf []byte
	off int
}

func (d *cborDecoder) remaining() int { return len(d.buf) - d.off }

// head reads an item's initial byte and argument. It returns the major
// type and the argument value; indefinite length (additional info 31) and
// the reserved values 28-30 are errors.
func (d *cborDecoder) head() (major byte, arg uint64, err error) {
	if d.remaining() < 1 {
		return 0, 0, errCBORTruncated
	}
	ib := d.buf[d.off]
	d.off++
	major = ib >> 5
	info := ib & 0x1f
	switch {
	case info < 24:
		return major, uint64(info), nil
	case info == 24:
		if d.remaining() < 1 {
			return 0, 0, errCBORTruncated
		}
		arg = uint64(d.buf[d.off])
		d.off++
	case info == 25:
		if d.remaining() < 2 {
			return 0, 0, errCBORTruncated
		}
		arg = uint64(binary.BigEndian.Uint16(d.buf[d.off:]))
		d.off += 2
	case info == 26:
		if d.remaining() < 4 {
			return 0, 0, errCBORTruncated
		}
		arg = uint64(binary.BigEndian.Uint32(d.buf[d.off:]))
		d.off += 4
	case info == 27:
		if d.remaining() < 8 {
			return 0, 0, errCBORTruncated
		}
		arg = binary.BigEndian.Uint64(d.buf[d.off:])
		d.off += 8
	case info == 31:
		return 0, 0, errors.New("cbor: indefinite length not supported")
	default:
		return 0, 0, fmt.Errorf("cbor: reserved additional info %d", info)
	}
	return major, arg, nil
}

func (d *cborDecoder) value(depth int) (any, error) {
	if depth > cborMaxDepth {
		return nil, errors.New("cbor: nesting too deep")
	}
	major, arg, err := d.head()
	if err != nil {
		return nil, err
	}
	switch major {
	case 0: // unsigned integer
		if arg > math.MaxInt64 {
			return nil, errors.New("cbor: integer overflows int64")
		}
		return int64(arg), nil
	case 1: // negative integer: -1 - arg
		if arg > math.MaxInt64 {
			return nil, errors.New("cbor: integer overflows int64")
		}
		return -1 - int64(arg), nil
	case 2, 3: // byte string, text string
		if arg > uint64(d.remaining()) {
			return nil, errCBORTruncated
		}
		n := int(arg)
		raw := d.buf[d.off : d.off+n]
		d.off += n
		if major == 2 {
			return append(make([]byte, 0, n), raw...), nil
		}
		s := string(raw)
		if !utf8.ValidString(s) {
			return nil, errors.New("cbor: text string is not valid UTF-8")
		}
		return s, nil
	case 4: // array
		// Every element takes at least one byte, so a count beyond the
		// remaining input is necessarily truncated -- checked before the
		// slice is allocated.
		if arg > uint64(d.remaining()) {
			return nil, errCBORTruncated
		}
		arr := make([]any, 0, int(arg))
		for i := uint64(0); i < arg; i++ {
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	case 5: // map
		if arg > uint64(d.remaining())/2 {
			return nil, errCBORTruncated
		}
		m := make(map[any]any, int(arg))
		for i := uint64(0); i < arg; i++ {
			k, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			switch k.(type) {
			case int64, string:
			default:
				return nil, fmt.Errorf("cbor: unsupported map key type %T", k)
			}
			if _, dup := m[k]; dup {
				return nil, fmt.Errorf("cbor: duplicate map key %v", k)
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			m[k] = v
		}
		return m, nil
	case 6:
		return nil, errors.New("cbor: tags not supported")
	default: // 7: simple values and floats
		switch arg {
		case 20:
			return false, nil
		case 21:
			return true, nil
		case 22:
			return nil, nil
		default:
			return nil, fmt.Errorf("cbor: unsupported simple/float value %d", arg)
		}
	}
}
