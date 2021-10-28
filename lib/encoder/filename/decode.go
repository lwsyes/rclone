package filename

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"github.com/rclone/rclone/lib/sync"

	"github.com/dop251/scsu"
	"github.com/klauspost/compress/huff0"
)

// ErrCorrupted is returned if a provided encoded filename cannot be decoded.
var ErrCorrupted = errors.New("file name corrupt")

// ErrUnsupported is returned if a provided encoding may come from a future version or the file name is corrupt.
var ErrUnsupported = errors.New("file name possibly generated by future version of rclone")

// Custom decoder for tableCustom types. Stateful, so must have lock.
var customDec huff0.Scratch
var customDecMu sync.Mutex

// Decode an encoded string.
func Decode(s string) (string, error) {
	initCoders()
	if len(s) < 1 {
		return "", ErrCorrupted
	}
	table := decodeMap[s[0]]
	if table == 0 {
		return "", ErrCorrupted
	}
	table--
	s = s[1:]
	data := make([]byte, base64.URLEncoding.DecodedLen(len(s)))
	n, err := base64.URLEncoding.Decode(data, ([]byte)(s))
	if err != nil || n < 0 {
		return "", ErrCorrupted
	}
	data = data[:n]
	return DecodeBytes(table, data)
}

// DecodeBytes will decode raw id and data values.
func DecodeBytes(table byte, data []byte) (string, error) {
	initCoders()
	switch table {
	case tableUncompressed:
		return string(data), nil
	case tableReserved:
		return "", ErrUnsupported
	case tableSCSUPlain:
		return scsu.Decode(data)
	case tableRLE:
		if len(data) < 2 {
			return "", ErrCorrupted
		}
		n, used := binary.Uvarint(data[:len(data)-1])
		if used <= 0 || n > maxLength {
			return "", ErrCorrupted
		}
		return string(bytes.Repeat(data[len(data)-1:], int(n))), nil
	case tableCustom:
		customDecMu.Lock()
		defer customDecMu.Unlock()
		_, data, err := huff0.ReadTable(data, &customDec)
		if err != nil {
			return "", ErrCorrupted
		}
		customDec.MaxDecodedSize = maxLength
		decoded, err := customDec.Decompress1X(data)
		if err != nil {
			return "", ErrCorrupted
		}
		return string(decoded), nil
	default:
		if table >= byte(len(decTables)) {
			return "", ErrCorrupted
		}
		dec := decTables[table]
		if dec == nil {
			return "", ErrUnsupported
		}
		var dst [maxLength]byte
		name, err := dec.Decompress1X(dst[:0], data)
		if err != nil {
			return "", ErrCorrupted
		}
		if table == tableSCSU {
			return scsu.Decode(name)
		}
		return string(name), nil
	}
}
