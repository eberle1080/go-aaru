package aaru

import "github.com/eberle1080/go-aaru/console/ps3"

// ps3DecryptForWrite returns a decrypted copy of a PlayStation 3 sector the
// caller marked SectorStatusUnencrypted (stored decrypted), when the disc key
// and encryption map are known (media tags) and the sector lies outside the
// plaintext regions. ok is false when
// the data must be stored as given.
func (img *Image) ps3DecryptForWrite(addr uint64, data []byte) (dec []byte, ok bool) {
	img.ps3LazyInit()
	key, ok := img.ps3CryptKey(addr)
	if !ok {
		return nil, false
	}
	dec = append([]byte(nil), data...)
	// Only whole AES blocks are decrypted.
	n := len(dec) &^ 15
	if n > 0 {
		_ = ps3.DecryptSector(key, addr, dec[:n])
	}
	return dec, true
}
