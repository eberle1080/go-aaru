package aaru

import (
	"fmt"

	"github.com/eberle1080/go-aaru/console/ngcw"
	"github.com/eberle1080/go-aaru/console/ps3"
	"github.com/eberle1080/go-aaru/console/wii"
	"github.com/eberle1080/go-aaru/console/wiiu"
	"github.com/eberle1080/go-aaru/media"
)

// consoleCrypto is the lazily initialized console disc crypto state. It is
// embedded in consoleState (Image.console).
//
// Every group is initialized at most once per Image by its lazy init function
// (ps3LazyInit, wiiuLazyInit, wiiLazyInit, ngcwJunkLazyInit), from the media
// tags present at that moment.
//
// The write path uses the PS3 group: it calls img.ps3LazyInit() and then
// img.ps3CryptKey(sector) (or reads ps3Key / ps3Map directly) to decide
// whether, and with which key, a sector marked SectorStatusUnencrypted must be
// decrypted before being stored.
type consoleCrypto struct {
	// ---- PS3 ----
	ps3Initialized bool              // ps3LazyInit already ran
	ps3Key         *[16]byte         // disc key (MediaTagPs3DiscKey, or derived from MediaTagPs3Data1); nil if unavailable
	ps3Map         ps3.EncryptionMap // plaintext regions (MediaTagPs3EncryptionMap); nil if absent or empty

	// ---- Wii U ----
	wiiuInitialized bool                 // wiiuLazyInit already ran
	wiiuDiscKey     *[16]byte            // MediaTagWiiUDiscKey; loaded but not needed for reading
	wiiuMap         wiiu.PartitionKeyMap // MediaTagWiiUPartitionKeyMap; nil if absent or empty
	wiiuCache       []byte               // re-encrypted 0x8000-byte physical sector
	wiiuCacheValid  bool
	wiiuCachedPhys  uint64 // physical sector held in wiiuCache
	wiiuBuilding    bool   // recursion guard while assembling a physical sector

	// ---- Wii ----
	wiiInitialized bool                // wiiLazyInit already ran
	wiiMap         wii.PartitionKeyMap // MediaTagWiiPartitionKeyMap; nil if absent or empty
	wiiCache       []byte              // re-encrypted 0x8000-byte group
	wiiCacheValid  bool
	wiiCachedGroup uint64 // physical group held in wiiCache
	wiiBuilding    bool   // recursion guard while assembling a group

	// ---- GameCube / Wii junk ----
	ngcwJunkInitialized bool         // ngcwJunkLazyInit already ran
	ngcwJunk            ngcw.JunkMap // MediaTagNgcwJunkMap; nil if absent or empty
}

// reset drops all console crypto state, zeroing key material.
func (c *consoleState) reset() {
	if c.ps3Key != nil {
		*c.ps3Key = [16]byte{}
	}
	if c.wiiuDiscKey != nil {
		*c.wiiuDiscKey = [16]byte{}
	}
	for i := range c.wiiuMap {
		c.wiiuMap[i].Key = [16]byte{}
	}
	for i := range c.wiiMap {
		c.wiiMap[i].Key = [16]byte{}
	}
	c.consoleCrypto = consoleCrypto{}
}

// mediaTag16 returns the media tag t if it is exactly 16 bytes long.
func (img *Image) mediaTag16(t MediaTagType) (*[16]byte, bool) {
	d, ok := img.mediaTags[t]
	if !ok || len(d) != 16 {
		return nil, false
	}
	var k [16]byte
	copy(k[:], d)
	return &k, true
}

// ps3LazyInit loads the PS3 disc key (MediaTagPs3DiscKey, or derived from
// MediaTagPs3Data1) and the plaintext region map (MediaTagPs3EncryptionMap)
// from the media tags into img.console. Only the first call
// has an effect; it is therefore safe to call repeatedly.
func (img *Image) ps3LazyInit() {
	c := &img.console
	if c.ps3Initialized {
		return
	}
	c.ps3Initialized = true

	if c.ps3Key == nil {
		if k, ok := img.mediaTag16(MediaTagPs3DiscKey); ok {
			c.ps3Key = k
		} else if d1, ok := img.mediaTag16(MediaTagPs3Data1); ok {
			k := ps3.DeriveDiscKey(*d1)
			c.ps3Key = &k
		}
	}

	if c.ps3Map == nil {
		if d, ok := img.mediaTags[MediaTagPs3EncryptionMap]; ok && len(d) >= 4 {
			var m ps3.EncryptionMap
			if m.UnmarshalBinary(d) == nil {
				c.ps3Map = m
			}
		}
	}
}

// ps3CryptKey reports whether PS3 sector must be encrypted on disc and
// returns the disc key to use. It requires a disc key and a non-empty
// encryption map, on both the read and the write path. Call ps3LazyInit
// first.
func (img *Image) ps3CryptKey(sector uint64) ([16]byte, bool) {
	c := &img.console
	if c.ps3Key == nil || c.ps3Map == nil || !c.ps3Map.IsSectorEncrypted(sector) {
		return [16]byte{}, false
	}
	return *c.ps3Key, true
}

// wiiuLazyInit loads the Wii U disc key and partition key map from the media
// tags and allocates the re-encryption cache.
func (img *Image) wiiuLazyInit() {
	c := &img.console
	if c.wiiuInitialized {
		return
	}
	c.wiiuInitialized = true

	if c.wiiuDiscKey == nil {
		if k, ok := img.mediaTag16(MediaTagWiiUDiscKey); ok {
			c.wiiuDiscKey = k
		}
	}
	if c.wiiuMap == nil {
		if d, ok := img.mediaTags[MediaTagWiiUPartitionKeyMap]; ok && len(d) >= 4 {
			var m wiiu.PartitionKeyMap
			if m.UnmarshalBinary(d) == nil {
				c.wiiuMap = m
			}
		}
	}
	if c.wiiuCache == nil {
		c.wiiuCache = make([]byte, wiiu.PhysicalSectorSize)
		c.wiiuCacheValid = false
	}
}

// wiiLazyInit loads the Wii partition key map from the media tags and
// allocates the group re-encryption cache.
func (img *Image) wiiLazyInit() {
	c := &img.console
	if c.wiiInitialized {
		return
	}
	c.wiiInitialized = true

	if c.wiiMap == nil {
		if d, ok := img.mediaTags[MediaTagWiiPartitionKeyMap]; ok && len(d) >= 4 {
			var m wii.PartitionKeyMap
			if m.UnmarshalBinary(d) == nil {
				c.wiiMap = m
			}
		}
	}
	if c.wiiCache == nil {
		c.wiiCache = make([]byte, wii.GroupSize)
		c.wiiCacheValid = false
	}
}

// ngcwJunkHeaderSize is the size of the junk map header.
const ngcwJunkHeaderSize = 8

// ngcwJunkLazyInit loads the GameCube/Wii junk map from the media tags.
func (img *Image) ngcwJunkLazyInit() {
	c := &img.console
	if c.ngcwJunkInitialized {
		return
	}
	c.ngcwJunkInitialized = true

	if c.ngcwJunk != nil {
		return
	}
	d, ok := img.mediaTags[MediaTagNgcwJunkMap]
	if !ok || len(d) < ngcwJunkHeaderSize {
		return
	}
	var m ngcw.JunkMap
	if m.UnmarshalBinary(d) == nil {
		c.ngcwJunk = m
	}
}

// readGenerable serves a SectorStatusGenerable sector of a GameCube or Wii
// disc: the junk is regenerated from the junk map and, for Wii discs inside
// an encrypted partition, re-encrypted as part of its group. Sectors not
// covered by the junk map (or images without one) read as zeros. The sector
// is ImageInfo.SectorSize bytes long and its status becomes
// SectorStatusDumped.
func (img *Image) readGenerable(addr uint64) ([]byte, error) {
	size := uint64(img.info.SectorSize)
	if size == 0 || size > maxDataBlockSectorSize {
		return nil, fmt.Errorf("generable sector of %d bytes: %w", size, ErrIncorrectDataSize)
	}
	img.ngcwJunkLazyInit()
	c := &img.console
	data := make([]byte, size)

	if c.ngcwJunk == nil {
		return data, nil // no junk map loaded: zero-fill
	}
	if c.ngcwJunk.Regenerate(addr*size, data) != nil {
		clear(data) // not found in junk map: zero-fill
		return data, nil
	}
	if media.Type(img.header.MediaType) == media.GOD {
		return data, nil
	}

	// WOD: regenerated junk needs Wii re-encryption.
	img.wiiLazyInit()
	if c.wiiMap != nil && c.wiiCache != nil && !c.wiiBuilding {
		if key, ok := c.wiiMap.SectorKey(addr); ok {
			img.wiiEncryptedSlice(addr, data, key)
		}
	}
	return data, nil
}

// consoleReencrypt applies the console re-encryption steps that follow a
// successful sector read: sectors stored decrypted
// (SectorStatusUnencrypted) of PS3, Wii U and Wii discs are encrypted again so
// that callers get the original disc contents. data is modified in place and
// the resulting status is returned.
func (img *Image) consoleReencrypt(addr uint64, data []byte, status SectorStatus) SectorStatus {
	if status != SectorStatusUnencrypted {
		return status
	}
	c := &img.console
	mt := media.Type(img.header.MediaType)

	// PS3 re-encryption.
	if mt == media.PS3DVD || mt == media.PS3BD {
		img.ps3LazyInit()
		if key, ok := img.ps3CryptKey(addr); ok && len(data)%16 == 0 {
			_ = ps3.EncryptSector(key, addr, data)
		}
		status = SectorStatusDumped
	}

	// Wii U re-encryption.
	if status == SectorStatusUnencrypted && mt == media.WUOD && !c.wiiuBuilding {
		img.wiiuLazyInit()
		if c.wiiuMap != nil && c.wiiuCache != nil {
			if key, ok := c.wiiuMap.SectorKey(addr); ok {
				img.wiiuEncryptedSlice(addr, data, key)
			}
		}
		status = SectorStatusDumped
	}

	// Wii re-encryption.
	if status == SectorStatusUnencrypted && mt == media.WOD && !c.wiiBuilding {
		img.wiiLazyInit()
		img.ngcwJunkLazyInit()
		if c.wiiMap != nil && c.wiiCache != nil {
			if key, ok := c.wiiMap.SectorKey(addr); ok {
				img.wiiEncryptedSlice(addr, data, key)
			}
		}
		status = SectorStatusDumped
	}
	return status
}

// wiiuEncryptedSlice replaces data (logical sector addr, already read) with
// its slice of the re-encrypted Wii U physical sector, building and caching
// the physical sector from its 16 logical sectors if needed.
func (img *Image) wiiuEncryptedSlice(addr uint64, data []byte, key [16]byte) {
	c := &img.console
	const per = wiiu.LogicalPerPhysical
	if len(data) != wiiu.LogicalSectorSize {
		return // cannot assemble a physical sector from odd-sized logical sectors
	}
	phys := addr / per
	slice := addr % per
	if !c.wiiuCacheValid || c.wiiuCachedPhys != phys {
		img.assembleCryptoBlock(c.wiiuCache, phys*per, per, addr, data, &c.wiiuBuilding)
		_ = wiiu.EncryptPhysicalSector(key, c.wiiuCache)
		c.wiiuCachedPhys = phys
		c.wiiuCacheValid = true
	}
	n := uint64(len(data))
	copy(data, c.wiiuCache[slice*n:(slice+1)*n])
}

// wiiEncryptedSlice replaces data (logical sector addr, already read or
// regenerated) with its slice of the re-encrypted Wii group, building the
// group from its 16 logical sectors, reconstructing its junk and encrypting
// it if the cached group is not the right one.
func (img *Image) wiiEncryptedSlice(addr uint64, data []byte, key [16]byte) {
	c := &img.console
	const per = wii.LogicalPerGroup
	if len(data) != wii.SectorSize {
		return // cannot assemble a group from odd-sized logical sectors
	}
	group := addr / per
	slice := addr % per
	if !c.wiiCacheValid || c.wiiCachedGroup != group {
		img.assembleCryptoBlock(c.wiiCache, group*per, per, addr, data, &c.wiiBuilding)
		if c.ngcwJunk != nil && c.wiiMap != nil {
			_ = wii.ReconstructGroupJunk(c.wiiCache, group, c.wiiMap, c.ngcwJunk)
		}
		_ = wii.EncryptGroup(key, c.wiiCache)
		c.wiiCachedGroup = group
		c.wiiCacheValid = true
	}
	n := uint64(len(data))
	copy(data, c.wiiCache[slice*n:(slice+1)*n])
}

// assembleCryptoBlock fills buf with count logical sectors starting at first,
// using data for sector addr and reading the others (with the recursion
// guard *building set, so they are returned as stored). Sectors that cannot
// be read, or whose size differs, are zero-filled.
func (img *Image) assembleCryptoBlock(buf []byte, first, count, addr uint64, data []byte, building *bool) {
	n := uint64(len(data))
	*building = true
	defer func() { *building = false }()
	for s := uint64(0); s < count; s++ {
		dst := buf[s*n : (s+1)*n]
		logical := first + s
		if logical == addr {
			copy(dst, data)
			continue
		}
		sec, _, err := img.readSector(logical, false)
		if err != nil || uint64(len(sec)) != n {
			clear(dst)
			continue
		}
		copy(dst, sec)
	}
}
