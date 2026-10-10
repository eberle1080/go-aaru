package flac

import (
	"math"
	"math/bits"
)

// Linear prediction analysis, computed in float64 throughout.

// autocorrelation computes autoc[0..len(autoc)-1] of data.
func autocorrelation(data []float64, autoc []float64) {
	n := len(data)
	for lag := range autoc {
		if lag >= n {
			autoc[lag] = 0
			continue
		}
		a := data[lag:]
		b := data[:len(a)]
		// Four independent accumulators hide floating point add latency.
		var s0, s1, s2, s3 float64
		i := 0
		for ; i+4 <= len(a); i += 4 {
			s0 += a[i] * b[i]
			s1 += a[i+1] * b[i+1]
			s2 += a[i+2] * b[i+2]
			s3 += a[i+3] * b[i+3]
		}
		for ; i < len(a); i++ {
			s0 += a[i] * b[i]
		}
		autoc[lag] = (s0 + s1) + (s2 + s3)
	}
}

// computeLPCoefficients runs the Levinson-Durbin recursion on autoc for
// orders 1..maxOrder. lp[o-1][:o] receives the predictor coefficients of
// order o and lpErr[o-1] its prediction error. It returns the highest order
// actually computed (the recursion stops early on a zero error).
func computeLPCoefficients(autoc []float64, maxOrder int, lp *[maxLPCOrder][maxLPCOrder]float64, lpErr []float64) int {
	var lpc [maxLPCOrder]float64
	err := autoc[0]
	for i := range maxOrder {
		r := -autoc[i+1]
		for j := range i {
			r -= lpc[j] * autoc[i-j]
		}
		r /= err

		lpc[i] = r
		var j int
		for j = 0; j < i>>1; j++ {
			tmp := lpc[j]
			lpc[j] += r * lpc[i-1-j]
			lpc[i-1-j] += r * tmp
		}
		if i&1 != 0 {
			lpc[j] += lpc[j] * r
		}

		err *= 1 - r*r

		for j := 0; j <= i; j++ {
			lp[i][j] = -lpc[j] // negate FIR filter coefficients to get predictor coefficients
		}
		lpErr[i] = err

		if err == 0 {
			return i + 1
		}
	}
	return maxOrder
}

// quantizeCoefficients quantizes lp to precision-bit signed integers and
// returns the shift to apply to the prediction. ok is false when the
// coefficients cannot be represented (all zero, or a shift below -16 would
// be required).
func quantizeCoefficients(lp []float64, precision uint, q []int32) (shift int, ok bool) {
	precision--
	qmax := int32(1) << precision
	qmin := -qmax
	qmax--

	var cmax float64
	for _, c := range lp {
		cmax = max(cmax, math.Abs(c))
	}
	if !(cmax > 0) {
		return 0, false
	}
	const maxShift = 1<<(5-1) - 1
	const minShift = -maxShift - 1
	_, log2cmax := math.Frexp(cmax)
	log2cmax--
	shift = int(precision) - log2cmax - 1
	if shift > maxShift {
		shift = maxShift
	} else if shift < minShift {
		return 0, false
	}

	scale := math.Ldexp(1, shift) // negative shifts scale coefficients down
	var errAcc float64
	for i, c := range lp {
		errAcc += c * scale
		v := math.Round(errAcc)
		var qi int32
		switch {
		case v > float64(qmax):
			qi = qmax
		case v < float64(qmin):
			qi = qmin
		default:
			qi = int32(v)
		}
		errAcc -= float64(qi)
		q[i] = qi
	}
	if shift < 0 {
		// A negative shift is not representable; the coefficients were
		// scaled down instead.
		shift = 0
	}
	return shift, true
}

// expectedBitsPerResidualSample estimates the Rice-coded size of a residual
// with the given prediction error energy.
func expectedBitsPerResidualSample(lpcErr float64, errScale float64) float64 {
	switch {
	case lpcErr > 0:
		bps := 0.5 * math.Log(errScale*lpcErr) / math.Ln2
		if bps >= 0 {
			return bps
		}
		return 0
	case lpcErr < 0: // only possible through rounding
		return 1e32
	default:
		return 0
	}
}

// bestLPCOrder guesses the order (1..maxOrder) giving the smallest subframe.
func bestLPCOrder(lpErr []float64, maxOrder, totalSamples int, overheadBitsPerOrder uint) int {
	errScale := 0.5 / float64(totalSamples)
	best := 0
	bestBits := math.MaxUint32 * 1.0
	for i := range maxOrder {
		order := i + 1
		b := expectedBitsPerResidualSample(lpErr[i], errScale)*float64(totalSamples-order) + float64(order)*float64(overheadBitsPerOrder)
		if b < bestBits {
			best = i
			bestBits = b
		}
	}
	return best + 1
}

// fixedBestPredictor estimates which fixed predictor order (0..4) suits
// data[4:] best, using data[:4] as history. bitsPerSample receives the
// estimated residual bits per sample of each order.
func fixedBestPredictor(data []int32, bitsPerSample *[maxFixedOrder + 1]float64) int {
	last0 := int64(data[3])
	last1 := int64(data[3]) - int64(data[2])
	last2 := last1 - (int64(data[2]) - int64(data[1]))
	last3 := last2 - (int64(data[2]) - 2*int64(data[1]) + int64(data[0]))
	var t0, t1, t2, t3, t4 uint64
	for _, s := range data[4:] {
		e := int64(s)
		t0 += abs64(e)
		save := e
		e -= last0
		t1 += abs64(e)
		last0 = save
		save = e
		e -= last1
		t2 += abs64(e)
		last1 = save
		save = e
		e -= last2
		t3 += abs64(e)
		last2 = save
		save = e
		e -= last3
		t4 += abs64(e)
		last3 = save
	}

	var order int
	switch {
	case t0 < min(t1, t2, t3, t4):
		order = 0
	case t1 < min(t2, t3, t4):
		order = 1
	case t2 < min(t3, t4):
		order = 2
	case t3 < t4:
		order = 3
	default:
		order = 4
	}

	n := float64(len(data) - 4)
	for i, t := range [...]uint64{t0, t1, t2, t3, t4} {
		if t > 0 {
			bitsPerSample[i] = math.Log(math.Ln2*float64(t)/n) / math.Ln2
		} else {
			bitsPerSample[i] = 0
		}
	}
	return order
}

func abs64(v int64) uint64 {
	if v < 0 {
		return uint64(-v)
	}
	return uint64(v)
}

// fixedResidual computes the order-th fixed predictor residual of
// sig[order:] into res[:len(sig)-order].
func fixedResidual(sig []int32, order int, res []int32) {
	n := len(sig)
	res = res[:n-order]
	switch order {
	case 0:
		copy(res, sig)
	case 1:
		for i := range res {
			j := i + 1
			res[i] = sig[j] - sig[j-1]
		}
	case 2:
		for i := range res {
			j := i + 2
			res[i] = sig[j] - 2*sig[j-1] + sig[j-2]
		}
	case 3:
		for i := range res {
			j := i + 3
			res[i] = sig[j] - 3*sig[j-1] + 3*sig[j-2] - sig[j-3]
		}
	case 4:
		for i := range res {
			j := i + 4
			res[i] = sig[j] - 4*sig[j-1] + 6*sig[j-2] - 4*sig[j-3] + sig[j-4]
		}
	}
}

// lpcResidual computes the residual of sig[order:] for the quantized
// predictor coeffs/shift into res. It returns false if any residual does not
// fit in 32 bits (the candidate must then be discarded).
func lpcResidual(sig []int32, coeffs []int32, shift uint, res []int32) bool {
	order := len(coeffs)
	n := len(sig)
	res = res[:n-order]
	if order > 12 || n < 24 {
		return lpcResidualGeneric(sig, coeffs, shift, res)
	}
	// Orders up to 12 use an unrolled 12-tap kernel with zero-padded
	// coefficients once enough history is available.
	head := 12 - order
	if !lpcResidualGeneric(sig[:12], coeffs, shift, res[:head]) {
		return false
	}
	var c [12]int32
	copy(c[:], coeffs)
	return lpcResidual12(sig, &c, shift, res[head:])
}

func lpcResidualGeneric(sig []int32, coeffs []int32, shift uint, res []int32) bool {
	order := len(coeffs)
	for i := range res {
		hist := sig[i : i+order]
		var sum int64
		for j, c := range coeffs {
			sum += int64(c) * int64(hist[order-1-j])
		}
		r := int64(sig[i+order]) - sum>>shift
		if r != int64(int32(r)) {
			return false
		}
		res[i] = int32(r)
	}
	return true
}

// lpcResidual12 computes res[i] for sig[i+12] using a 12-tap predictor.
func lpcResidual12(sig []int32, c *[12]int32, shift uint, res []int32) bool {
	c0, c1, c2, c3, c4, c5 := int64(c[0]), int64(c[1]), int64(c[2]), int64(c[3]), int64(c[4]), int64(c[5])
	c6, c7, c8, c9, c10, c11 := int64(c[6]), int64(c[7]), int64(c[8]), int64(c[9]), int64(c[10]), int64(c[11])
	sig = sig[:len(res)+12]
	for i := range res {
		h := (*[13]int32)(sig[i : i+13])
		sum := c11*int64(h[0]) + c10*int64(h[1]) + c9*int64(h[2]) + c8*int64(h[3]) +
			c7*int64(h[4]) + c6*int64(h[5]) + c5*int64(h[6]) + c4*int64(h[7]) +
			c3*int64(h[8]) + c2*int64(h[9]) + c1*int64(h[10]) + c0*int64(h[11])
		r := int64(h[12]) - sum>>shift
		if r != int64(int32(r)) {
			return false
		}
		res[i] = int32(r)
	}
	return true
}

// ilog2 returns floor(log2(v)) for v > 0, and 0 for v == 0.
func ilog2(v uint) uint {
	if v == 0 {
		return 0
	}
	return uint(bits.Len(v)) - 1
}
