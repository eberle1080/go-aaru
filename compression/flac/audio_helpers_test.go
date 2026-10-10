package flac

import (
	"encoding/binary"
	"math"
	"math/rand/v2"
)

// musicPCM returns samples stereo sample frames of deterministic synthetic
// "music" as interleaved 16-bit little-endian PCM. Every 4096 samples a new
// segment starts with its own pair of tonal sources (sums of sines), noise
// levels and stereo mixing, ranging from identical channels through
// correlated and independent ones to a silent channel, so the encoder meets
// a variety of predictors and stereo decorrelation choices.
func musicPCM(seed uint64, samples int) []byte {
	rng := rand.New(rand.NewPCG(seed, 0x666c6163))
	out := make([]byte, 0, 4*samples)
	type partial struct{ freq, amp, phase float64 }
	newSource := func() []partial {
		ps := make([]partial, 1+rng.IntN(4))
		for i := range ps {
			ps[i] = partial{
				freq:  2 * math.Pi * (50 + rng.Float64()*4000) / 44100,
				amp:   1000 + rng.Float64()*6000,
				phase: rng.Float64() * 2 * math.Pi,
			}
		}
		return ps
	}
	eval := func(ps []partial, n int) float64 {
		v := 0.0
		for _, p := range ps {
			v += p.amp * math.Sin(p.freq*float64(n)+p.phase)
		}
		return v
	}
	clamp := func(v float64) int16 { return int16(max(-32768, min(32767, math.Round(v)))) }
	for start := 0; start < samples; start += 4096 {
		a, b := newSource(), newSource()
		noiseL, noiseR := rng.Float64()*64, rng.Float64()*64
		var cl, cr [2]float64
		switch rng.IntN(6) {
		case 0: // identical channels
			cl, cr, noiseR = [2]float64{1, 0}, [2]float64{1, 0}, 0
		case 1: // correlated
			cl, cr = [2]float64{1, 0.2}, [2]float64{0.9, 0.3}
		case 2: // independent
			cl, cr = [2]float64{1, 0}, [2]float64{0, 1}
		case 3: // right is left plus a quiet extra source
			cl, cr = [2]float64{1, 0}, [2]float64{1, 0.05}
		case 4: // left is right plus a quiet extra source
			cl, cr = [2]float64{1, 0.05}, [2]float64{1, 0}
		default: // silent right channel
			cl, cr, noiseR = [2]float64{1, 1}, [2]float64{0, 0}, 0
		}
		for n := start; n < min(start+4096, samples); n++ {
			va, vb := eval(a, n), eval(b, n)
			l := cl[0]*va + cl[1]*vb + noiseL*(rng.Float64()-0.5)
			r := cr[0]*va + cr[1]*vb + noiseR*(rng.Float64()-0.5)
			out = binary.LittleEndian.AppendUint16(out, uint16(clamp(l)))
			out = binary.LittleEndian.AppendUint16(out, uint16(clamp(r)))
		}
	}
	return out
}
