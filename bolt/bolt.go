package bolt

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

type Message struct {
	T    Type
	Data []byte
}

type Version struct {
	Major, Minor, Patch uint8
}

type Type string

const (
	ResetMsg    Type = "RESET"
	RunMsg           = "RUN"
	DiscardMsg       = "DISCARD"
	PullMsg          = "PULL"
	RecordMsg        = "RECORD"
	SuccessMsg       = "SUCCESS"
	IgnoreMsg        = "IGNORE"
	FailureMsg       = "FAILURE"
	HelloMsg         = "HELLO"
	GoodbyeMsg       = "GOODBYE"
	BeginMsg         = "BEGIN"
	CommitMsg        = "COMMIT"
	RollbackMsg      = "ROLLBACK"
	UnknownMsg       = "?UNKNOWN?"
	NopMsg           = "NOP"
	ChunkedMsg       = "CHUNKED" // not a true bolt message
)

// Parse a byte into the corresponding Bolt message Type
func TypeFromByte(b byte) Type {
	switch b {
	case 0x0f:
		return ResetMsg
	case 0x10:
		return RunMsg
	case 0x2f:
		return DiscardMsg
	case 0x3f:
		return PullMsg
	case 0x71:
		return RecordMsg
	case 0x70:
		return SuccessMsg
	case 0x7e:
		return IgnoreMsg
	case 0x7f:
		return FailureMsg
	case 0x01:
		return HelloMsg
	case 0x02:
		return GoodbyeMsg
	case 0x11:
		return BeginMsg
	case 0x12:
		return CommitMsg
	case 0x13:
		return RollbackMsg
	default:
		return UnknownMsg
	}
}

// Try to extract the BoltMsg from some given bytes.
func IdentifyType(buf []byte) Type {

	// If the byte array is too small, it could be an empty message
	if len(buf) < 4 {
		return NopMsg
	}

	return TypeFromByte(buf[3])
}

// Try parsing some bytes into a Packstream Map, returning it as a map
// of strings to their values as byte arrays.
//
// If not found or something horribly wrong, return nil and an error. Also,
// will panic on a nil input.
func ParseMap(buf []byte) (map[string]interface{}, int, error) {
	if buf == nil {
		panic("cannot parse nil byte array for structs")
	}

	if len(buf) < 1 {
		return nil, 0, errors.New("bytes empty, cannot parse struct")
	}

	if buf[0]>>4 != 0xa && (buf[0] < 0xd8 || buf[0] > 0xda) {
		return nil, 0, errors.New("expected a map")
	}

	numMembers := 0
	pos := 1

	if buf[0]>>4 == 0xa {
		// Tiny Map
		numMembers = int(buf[0] & 0xf)
	} else {
		switch buf[0] & 0x0f {
		case 0x08:
			numMembers = int(buf[pos])
			pos++
		case 0x09:
			numMembers = int(binary.BigEndian.Uint16(buf[pos : pos+2]))
			pos = pos + 2
		case 0x0a:
			numMembers = int(binary.BigEndian.Uint32(buf[pos : pos+4]))
			pos = pos + 4
		default:
			return nil, 0, errors.New("invalid map prefix")
		}
	}

	result := make(map[string]interface{}, numMembers)

	for i := 0; i < numMembers; i++ {
		// map keys are Strings
		name, n, err := ParseString(buf[pos:])
		if err != nil {
			panic(err)
		}
		pos = pos + n

		// now for the value
		switch buf[pos] >> 4 {
		case 0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7: // tiny-int
			val, err := ParseTinyInt(buf[pos])
			if err != nil {
				panic(err)
				return result, pos, err
			}
			result[name] = val
			pos++
		case 0x8: // tiny-string
			val, n, err := ParseTinyString(buf[pos:])
			if err != nil {
				panic(err)
				return result, pos, err
			}
			result[name] = val
			pos = pos + n
		case 0x9: // tiny-array
			val, n, err := ParseArray(buf[pos:])
			if err != nil {
				panic(err)
				return result, pos, err
			}
			result[name] = val
			pos = pos + n
		case 0xa: // tiny-map
			value, n, err := ParseMap(buf[pos:])
			if err != nil {
				panic(err)
				return result, pos, err
			}
			result[name] = value
			pos = pos + n
		case 0xc: // floats, nil, and bools
			nib := int(buf[pos] & 0xf)
			switch nib {
			case 0: // packed nil/null
				result[name] = nil
				pos++
			case 1: // packed float
				panic("can't do floats yet")
			case 2:
				result[name] = false
				pos++
			case 3:
				result[name] = true
				pos++
			case 0x8, 0x9, 0xa, 0xb:
				val, n, err := ParseInt(buf[pos:])
				if err != nil {
					return result, pos, err
				}
				result[name] = val
				pos = pos + n
			}
		case 0xd:
			nib := int(buf[pos] & 0xf)
			switch nib {
			case 0x0, 0x1, 0x2: // string
				val, n, err := ParseString(buf[pos:])
				if err != nil {
					return result, pos, err
				}
				result[name] = val
				pos = pos + n
			case 0x4, 0x5, 0x6: // array
				val, n, err := ParseArray(buf[pos:])
				if err != nil {
					return result, pos, err
				}
				result[name] = val
				pos = pos + n
			case 0x7:
				panic("invalid prefix 0xd7")
			case 0x8, 0x9, 0xa:
				// err
				panic("not ready")
			}

		default:
			errMsg := fmt.Sprintf("found unsupported encoding type: %#v\n", buf[pos])
			return result, pos, errors.New(errMsg)
		}
	}
	return result, pos, nil
}

// Parse a TinyInt...which is a simply 7-bit number.
func ParseTinyInt(b byte) (int, error) {
	if b > 0x7f {
		return 0, errors.New("expected tiny-int!")
	}
	return int(b), nil
}

// Parse a packed Int
func ParseInt(buf []byte) (int, int, error) {
	if len(buf) < 2 || buf[0]>>4 != 0xc {
		return 0, 0, errors.New("can't parse int, invalid byte buf")
	}

	var i, n int

	switch buf[0] {
	case 0xc8:
		i = int(int8(buf[1]))
		n = 2
	case 0xc9:
		i = int(int16(binary.BigEndian.Uint16(buf[1:3])))
		n = 3
	case 0xca:
		i = int(int32(binary.BigEndian.Uint32(buf[1:5])))
		n = 5
	case 0xcb:
		i = int(int64(binary.BigEndian.Uint64(buf[1:9])))
		n = 9
	}

	return i, n, nil
}

// Parse a TinyString from a byte slice, returning the string (if valid) and
// the number of bytes processed from the slice (including the 0x80 prefix).
//
// Otherwise, return an empty string, 0, and an error.
func ParseTinyString(buf []byte) (string, int, error) {
	if len(buf) == 0 || buf[0]>>4 != 0x8 {
		return "", 0, errors.New("expected tiny-string!")
	}

	size := int(buf[0] & 0xf)
	if size == 0 {
		return "", 1, nil
	}

	return fmt.Sprintf("%s", buf[1:size+1]), size + 1, nil
}

// Parse a byte slice into a string, returning the string value, the last
// position used in the byte slice, and optionally an error
func ParseString(buf []byte) (string, int, error) {
	if len(buf) < 1 {
		return "", 0, errors.New("empty byte slice")
	}
	pos := 0

	if buf[0]>>4 == 0x8 {
		// tiny string
		return ParseTinyString(buf)
	} else if buf[0] < 0xd0 || buf[0] > 0xd2 {
		return "", 0, errors.New("slice doesn't look like valid string")
	}

	// how many bytes is the encoding for the string length?
	readAhead := int(1 << int(buf[pos]&0xf))
	pos++

	// decode the amount of bytes to read to get the string length
	sizeBytes := buf[pos : pos+readAhead]
	sizeBytes = append(make([]byte, 8), sizeBytes...)
	pos = pos + readAhead

	// decode the actual string length
	size := int(binary.BigEndian.Uint64(sizeBytes[len(sizeBytes)-8:]))
	return fmt.Sprintf("%s", buf[pos:pos+size]), pos + size, nil
}

// Parse a byte slice into a TinyArray as an array of interface{} values,
// returning the array, the last position in the byte slice read, and
// optionally an error.
func ParseArray(buf []byte) ([]interface{}, int, error) {
	if buf[0]>>4 != 0x9 && (buf[0] < 0xd4 || buf[0] > 0xd6) {
		return nil, 0, errors.New("expected an array")

	}
	size := 0
	pos := 1

	if buf[0]>>4 == 0x9 {
		// Tiny Array
		size = int(buf[0] & 0xf)
	} else {
		switch buf[0] & 0x0f {
		case 0x04:
			size = int(buf[pos])
			pos++
		case 0x05:
			size = int(binary.BigEndian.Uint16(buf[pos : pos+2]))
			pos = pos + 2
		case 0x06:
			size = int(binary.BigEndian.Uint32(buf[pos : pos+4]))
			pos = pos + 4
		default:
			return nil, 0, errors.New("invalid array prefix")
		}
	}

	array := make([]interface{}, size)
	if size == 0 {
		// bail early
		return array, pos, nil
	}

	for i := 0; i < size; i++ {
		memberType := buf[pos] >> 4
		switch memberType {
		case 0x0, 0x1, 0x2, 0x3, 0x4, 0x5, 0x6, 0x7: // tiny-int
			val, err := ParseTinyInt(buf[pos])
			if err != nil {
				return array, pos, err
			}
			array[i] = val
			pos++
		case 0x8: // tiny-string
			val, n, err := ParseTinyString(buf[pos:])
			if err != nil {
				return array, pos, err
			}
			array[i] = val
			pos = pos + n
		case 0xc:
			nib := int(buf[pos] & 0xf)
			switch nib {
			case 0: // packed nil/null
				array[i] = nil
				pos++
			case 1: // packed float
				panic("can't do floats yet")
			case 2:
				array[i] = false
				pos++
			case 3:
				array[i] = true
				pos++
			case 0x8, 0x9, 0xa, 0xb:
				val, n, err := ParseInt(buf[pos:])
				if err != nil {
					return array, pos, err
				}
				array[i] = val
				pos = pos + n
			}
		case 0xd:
			nib := int(buf[pos] & 0xf)
			switch nib {
			case 0x0, 0x1, 0x2: // string
				val, n, err := ParseString(buf[pos:])
				if err != nil {
					return array, pos, err
				}
				array[i] = val
				pos = pos + n
			case 0x4, 0x5, 0x6: // array
				val, n, err := ParseArray(buf[pos:])
				if err != nil {
					return array, pos, err
				}
				array[i] = val
				pos = pos + n
			case 0xd7:
				panic("invalid prefix 0xd7")
			case 0x8, 0x9, 0xa:
				// err
				panic("not ready")
			}
		default:
			errMsg := fmt.Sprintf("found unsupported encoding type: %#v", memberType)
			return array, pos, errors.New(errMsg)
		}
	}

	return array, pos, nil
}

// Serialize a string to a byte slice
func StringToBytes(s string) ([]byte, error) {
	buf := []byte(s)
	size := len(buf)

	if len(buf) < 16 {
		// TinyString
		prefix := []byte{uint8(0x80 + len(buf))}
		return bytes.Join([][]byte{
			prefix,
			buf,
		}, []byte{}), nil
	}

	prefix := new(bytes.Buffer)
	var data interface{}
	if size < 0x1000 {
		data = 0xd000 + uint16(size)
	} else if size < 0x10000 {
		data = 0xd00000 + uint32(size)
	} else {
		data = 0xd0000000 + uint64(size)
	}
	err := binary.Write(prefix, binary.BigEndian, data)
	if err != nil {
		return []byte{}, err
	}

	return bytes.Join([][]byte{prefix.Bytes(), buf}, []byte{}), nil

}

// Serialize an int to a byte slice
func IntToBytes(i int) ([]byte, error) {

	// Tiny Int
	if i < 0x80 {
		return []byte{byte(uint(i))}, nil
	}

	// Regular Int
	buf := new(bytes.Buffer)
	if i < 0x100 {
		buf.Write([]byte{0xc8, byte(uint(i))})
	} else if i < 0x10000 {
		buf.WriteByte(byte(0xc9))
		binary.Write(buf, binary.BigEndian, uint16(i))
	} else if i < 0x100000000 {
		buf.WriteByte(byte(0xca))
		binary.Write(buf, binary.BigEndian, uint32(i))
	} else {
		buf.WriteByte(byte(0xcb))
		binary.Write(buf, binary.BigEndian, uint64(i))
	}
	return buf.Bytes(), nil
}

// Serialize a TinyMap to a byte slice
func TinyMapToBytes(tinymap map[string]interface{}) ([]byte, error) {
	buf := make([]byte, 1024*4)

	if len(tinymap) > 15 {
		return []byte{}, errors.New("too many keys for a tinymap!")
	}

	buf[0] = byte(0xa0 + uint8(len(tinymap)))
	pos := 1

	for key := range tinymap {
		// key first
		raw, err := StringToBytes(key)
		if err != nil {
			return buf[:pos], err
		}
		copy(buf[pos:], raw)
		pos = pos + len(raw)

		// now the value
		val := tinymap[key]
		switch val.(type) {
		case int:
			raw, err = IntToBytes(val.(int))
		case string:
			raw, err = StringToBytes(val.(string))
		case map[string]interface{}:
			raw, err = TinyMapToBytes(val.(map[string]interface{}))
		case nil:
			raw = []byte{0xc0}
		default:
			err = errors.New("unsupported type")
		}

		if err != nil {
			return buf[:pos], err
		}

		copy(buf[pos:], raw)
		pos = pos + len(raw)
	}
	return buf[:pos], nil
}

func ParseVersion(buf []byte) (Version, error) {
	if len(buf) < 4 {
		return Version{}, errors.New("buffer too short (< 4)")
	}

	version := Version{}
	version.Major = uint8(buf[3])
	version.Minor = uint8(buf[2])
	version.Patch = uint8(buf[1])
	return version, nil
}

func (v Version) String() string {
	return fmt.Sprintf("Bolt{major: %d, minor: %d, patch: %d}",
		v.Major,
		v.Minor,
		v.Patch)
}

func (v Version) Bytes() []byte {
	return []byte{
		0x00, 0x00,
		v.Minor, v.Major,
	}
}
