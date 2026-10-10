package sfo

import "encoding/binary"

// testEntry describes one parameter for buildSFO. String formats use str,
// FormatInt32 uses ival; raw, when non-nil, overrides the encoded value.
type testEntry struct {
	key    string
	format uint16
	str    string
	ival   int32
	raw    []byte
	maxLen int // reserved data length; 0 means len(value) rounded up to 4
}

// buildSFO builds a PARAM.SFO image:
//
//	header (20): magic, version, key table offset, data table offset, count
//	index (16 each): key offset u16, format u16, length u32, max length u32, data offset u32
//	key table: NUL-terminated keys, padded to 4 bytes
//	data table: values, each reserved maxLen bytes
func buildSFO(entries []testEntry) []byte {
	var keys, values []byte

	index := make([]byte, 16*len(entries))

	for i, e := range entries {
		v := e.raw
		if v == nil {
			if e.format == FormatInt32 {
				v = binary.LittleEndian.AppendUint32(nil, uint32(e.ival))
			} else {
				v = []byte(e.str)
			}
		}

		maxLen := e.maxLen
		if maxLen == 0 {
			maxLen = (len(v) + 3) &^ 3
		}

		maxLen = max(maxLen, len(v))

		idx := index[16*i:]
		binary.LittleEndian.PutUint16(idx, uint16(len(keys)))
		binary.LittleEndian.PutUint16(idx[2:], e.format)
		binary.LittleEndian.PutUint32(idx[4:], uint32(len(v)))
		binary.LittleEndian.PutUint32(idx[8:], uint32(maxLen))
		binary.LittleEndian.PutUint32(idx[12:], uint32(len(values)))

		keys = append(append(keys, e.key...), 0)
		values = append(values, v...)
		values = append(values, make([]byte, maxLen-len(v))...)
	}

	for len(keys)%4 != 0 {
		keys = append(keys, 0)
	}

	keyTable := headerSize + len(index)
	dataTable := keyTable + len(keys)

	out := make([]byte, headerSize, dataTable+len(values))
	binary.LittleEndian.PutUint32(out, Magic)
	binary.LittleEndian.PutUint32(out[4:], 0x101)
	binary.LittleEndian.PutUint32(out[8:], uint32(keyTable))
	binary.LittleEndian.PutUint32(out[12:], uint32(dataTable))
	binary.LittleEndian.PutUint32(out[16:], uint32(len(entries)))

	out = append(out, index...)
	out = append(out, keys...)

	return append(out, values...)
}
