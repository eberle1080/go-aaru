package flac

// CRC-8 (polynomial x^8 + x^2 + x^1 + x^0, initial value 0) protects frame
// headers; CRC-16 (polynomial x^16 + x^15 + x^2 + x^0, initial value 0)
// protects whole frames. Both are MSB-first, unreflected, with no final XOR.

var (
	crc8Table  [256]uint8
	crc16Table [256]uint16
)

func init() {
	for i := range 256 {
		c8 := uint8(i)
		for range 8 {
			if c8&0x80 != 0 {
				c8 = c8<<1 ^ 0x07
			} else {
				c8 <<= 1
			}
		}
		crc8Table[i] = c8

		c16 := uint16(i) << 8
		for range 8 {
			if c16&0x8000 != 0 {
				c16 = c16<<1 ^ 0x8005
			} else {
				c16 <<= 1
			}
		}
		crc16Table[i] = c16
	}
}

func crc8(data []byte) uint8 {
	var c uint8
	for _, b := range data {
		c = crc8Table[c^b]
	}
	return c
}

func crc16(data []byte) uint16 {
	var c uint16
	for _, b := range data {
		c = c<<8 ^ crc16Table[byte(c>>8)^b]
	}
	return c
}
