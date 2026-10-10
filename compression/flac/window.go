package flac

import (
	"math"
	"strconv"
	"strings"
)

// Apodization (LPC analysis window) support. Windows are computed in
// float64.

type windowKind int

const (
	winBartlett windowKind = iota
	winBartlettHann
	winBlackman
	winBlackmanHarris4Term92dB
	winConnes
	winFlattop
	winGauss
	winHamming
	winHann
	winKaiserBessel
	winNuttall
	winRectangle
	winTriangle
	winTukey
	winPartialTukey
	winPunchoutTukey
	winWelch
)

const maxApodizations = 32

// apodization is one parsed window function with its parameters.
type apodization struct {
	kind       windowKind
	p          float64 // tukey p, gauss stddev
	start, end float64 // partial / punchout tukey
}

// parseApodization parses a FLAC apodization specification: a list of window
// names separated by ';'. Unknown names and out-of-range parameters are
// ignored; if nothing valid remains the default tukey(0.5) is used.
func parseApodization(spec string) []apodization {
	var out []apodization
	for tok := range strings.SplitSeq(spec, ";") {
		switch tok {
		case "bartlett":
			out = append(out, apodization{kind: winBartlett})
		case "bartlett_hann":
			out = append(out, apodization{kind: winBartlettHann})
		case "blackman":
			out = append(out, apodization{kind: winBlackman})
		case "blackman_harris_4term_92db":
			out = append(out, apodization{kind: winBlackmanHarris4Term92dB})
		case "connes":
			out = append(out, apodization{kind: winConnes})
		case "flattop":
			out = append(out, apodization{kind: winFlattop})
		case "hamming":
			out = append(out, apodization{kind: winHamming})
		case "hann":
			out = append(out, apodization{kind: winHann})
		case "kaiser_bessel":
			out = append(out, apodization{kind: winKaiserBessel})
		case "nuttall":
			out = append(out, apodization{kind: winNuttall})
		case "rectangle":
			out = append(out, apodization{kind: winRectangle})
		case "triangle":
			out = append(out, apodization{kind: winTriangle})
		case "welch":
			out = append(out, apodization{kind: winWelch})
		default:
			out = parseParameterizedWindow(out, tok)
		}
		if len(out) >= maxApodizations {
			out = out[:maxApodizations]
			break
		}
	}
	if len(out) == 0 {
		out = []apodization{{kind: winTukey, p: 0.5}}
	}
	return out
}

func parseParameterizedWindow(out []apodization, tok string) []apodization {
	switch {
	case len(tok) > 7 && strings.HasPrefix(tok, "gauss("):
		if s := parseFloatPrefix(tok[6:]); s > 0 && s <= 0.5 {
			out = append(out, apodization{kind: winGauss, p: s})
		}
	case len(tok) > 7 && strings.HasPrefix(tok, "tukey("):
		if p := parseFloatPrefix(tok[6:]); p >= 0 && p <= 1 {
			out = append(out, apodization{kind: winTukey, p: p})
		}
	case len(tok) > 15 && strings.HasPrefix(tok, "partial_tukey("):
		out = appendMultiTukey(out, tok[14:], winPartialTukey, 0.1)
	case len(tok) > 16 && strings.HasPrefix(tok, "punchout_tukey("):
		out = appendMultiTukey(out, tok[15:], winPunchoutTukey, 0.2)
	}
	return out
}

// appendMultiTukey handles "partial_tukey(n[/ov[/P]])" and
// "punchout_tukey(n[/ov[/P]])".
func appendMultiTukey(out []apodization, args string, kind windowKind, defOverlap float64) []apodization {
	parts := int(parseFloatPrefix(args))
	overlap := defOverlap
	p := 0.2
	if i := strings.IndexByte(args, '/'); i >= 0 {
		overlap = min(parseFloatPrefix(args[i+1:]), 0.99)
		if j := strings.IndexByte(args[i+1:], '/'); j >= 0 {
			p = parseFloatPrefix(args[i+1+j+1:])
		}
	}
	overlapUnits := 1/(1-overlap) - 1
	if parts <= 1 {
		return append(out, apodization{kind: winTukey, p: p})
	}
	if len(out)+parts >= maxApodizations {
		return out
	}
	for m := range parts {
		out = append(out, apodization{
			kind:  kind,
			p:     p,
			start: float64(m) / (float64(parts) + overlapUnits),
			end:   (float64(m) + 1 + overlapUnits) / (float64(parts) + overlapUnits),
		})
	}
	return out
}

// parseFloatPrefix parses the longest prefix of s that is a decimal floating
// point number (after leading white space); it returns 0 if there is none.
func parseFloatPrefix(s string) float64 {
	s = strings.TrimLeft(s, " \t\n\v\f\r")
	end := 0
	if end < len(s) && (s[end] == '+' || s[end] == '-') {
		end++
	}
	digits := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
		digits++
	}
	if end < len(s) && s[end] == '.' {
		end++
		for end < len(s) && s[end] >= '0' && s[end] <= '9' {
			end++
			digits++
		}
	}
	if digits == 0 {
		return 0
	}
	if end < len(s) && (s[end] == 'e' || s[end] == 'E') {
		e := end + 1
		if e < len(s) && (s[e] == '+' || s[e] == '-') {
			e++
		}
		if e < len(s) && s[e] >= '0' && s[e] <= '9' {
			for e < len(s) && s[e] >= '0' && s[e] <= '9' {
				e++
			}
			end = e
		}
	}
	// Only range errors are possible here, for which ParseFloat returns
	// the correctly signed infinity or zero.
	v, _ := strconv.ParseFloat(s[:end], 64)
	return v
}

// computeWindow fills w (length L) with the given window.
func computeWindow(w []float64, a apodization) {
	L := len(w)
	if L == 0 {
		return
	}
	N := float64(L - 1)
	if L == 1 {
		// Degenerate: every formula divides by N; a single sample window
		// is simply 1.
		w[0] = 1
		return
	}
	switch a.kind {
	case winBartlett:
		half := (L-1)/2 + 1
		if L&1 == 0 {
			half = L / 2
		}
		for n := range L {
			if n < half {
				w[n] = 2 * float64(n) / N
			} else {
				w[n] = 2 - 2*float64(n)/N
			}
		}
	case winBartlettHann:
		for n := range L {
			x := float64(n) / N
			w[n] = 0.62 - 0.48*math.Abs(x-0.5) - 0.38*math.Cos(2*math.Pi*x)
		}
	case winBlackman:
		for n := range L {
			x := float64(n) / N
			w[n] = 0.42 - 0.5*math.Cos(2*math.Pi*x) + 0.08*math.Cos(4*math.Pi*x)
		}
	case winBlackmanHarris4Term92dB:
		for n := range L {
			x := float64(n) / N
			w[n] = 0.35875 - 0.48829*math.Cos(2*math.Pi*x) + 0.14128*math.Cos(4*math.Pi*x) - 0.01168*math.Cos(6*math.Pi*x)
		}
	case winConnes:
		N2 := N / 2
		for n := range L {
			k := (float64(n) - N2) / N2
			k = 1 - k*k
			w[n] = k * k
		}
	case winFlattop:
		for n := range L {
			x := float64(n) / N
			w[n] = 0.21557895 - 0.41663158*math.Cos(2*math.Pi*x) + 0.277263158*math.Cos(4*math.Pi*x) -
				0.083578947*math.Cos(6*math.Pi*x) + 0.006947368*math.Cos(8*math.Pi*x)
		}
	case winGauss:
		N2 := N / 2
		for n := range L {
			k := (float64(n) - N2) / (a.p * N2)
			w[n] = math.Exp(-0.5 * k * k)
		}
	case winHamming:
		for n := range L {
			w[n] = 0.54 - 0.46*math.Cos(2*math.Pi*float64(n)/N)
		}
	case winHann:
		hann(w)
	case winKaiserBessel:
		for n := range L {
			x := float64(n) / N
			w[n] = 0.402 - 0.498*math.Cos(2*math.Pi*x) + 0.098*math.Cos(4*math.Pi*x) - 0.001*math.Cos(6*math.Pi*x)
		}
	case winNuttall:
		for n := range L {
			x := float64(n) / N
			w[n] = 0.3635819 - 0.4891775*math.Cos(2*math.Pi*x) + 0.1365995*math.Cos(4*math.Pi*x) - 0.0106411*math.Cos(6*math.Pi*x)
		}
	case winRectangle:
		for n := range L {
			w[n] = 1
		}
	case winTriangle:
		half := L / 2
		if L&1 != 0 {
			half = (L + 1) / 2
		}
		for n := 1; n <= L; n++ {
			if n <= half {
				w[n-1] = 2 * float64(n) / (float64(L) + 1)
			} else {
				w[n-1] = float64(2*(L-n+1)) / (float64(L) + 1)
			}
		}
	case winTukey:
		tukey(w, a.p)
	case winPartialTukey:
		partialTukey(w, a.p, a.start, a.end)
	case winPunchoutTukey:
		punchoutTukey(w, a.p, a.start, a.end)
	case winWelch:
		N2 := N / 2
		for n := range L {
			k := (float64(n) - N2) / N2
			w[n] = 1 - k*k
		}
	}
}

func hann(w []float64) {
	N := float64(len(w) - 1)
	for n := range w {
		w[n] = 0.5 - 0.5*math.Cos(2*math.Pi*float64(n)/N)
	}
}

func tukey(w []float64, p float64) {
	L := len(w)
	switch {
	case p <= 0:
		for n := range w {
			w[n] = 1
		}
	case p >= 1:
		hann(w)
	default:
		Np := int(p/2*float64(L)) - 1
		for n := range w {
			w[n] = 1
		}
		if Np > 0 {
			for n := 0; n <= Np; n++ {
				w[n] = 0.5 - 0.5*math.Cos(math.Pi*float64(n)/float64(Np))
				w[L-Np-1+n] = 0.5 - 0.5*math.Cos(math.Pi*float64(n+Np)/float64(Np))
			}
		}
	}
}

func partialTukey(w []float64, p, start, end float64) {
	switch {
	case p <= 0:
		p = 0.05
	case p >= 1:
		p = 0.95
	}
	L := len(w)
	startN := int(start * float64(L))
	endN := int(end * float64(L))
	Np := int(p / 2 * float64(endN-startN))
	n := 0
	for ; n < startN && n < L; n++ {
		w[n] = 0
	}
	for i := 1; n < startN+Np && n < L; n, i = n+1, i+1 {
		w[n] = 0.5 - 0.5*math.Cos(math.Pi*float64(i)/float64(Np))
	}
	for ; n < endN-Np && n < L; n++ {
		w[n] = 1
	}
	for i := Np; n < endN && n < L; n, i = n+1, i-1 {
		w[n] = 0.5 - 0.5*math.Cos(math.Pi*float64(i)/float64(Np))
	}
	for ; n < L; n++ {
		w[n] = 0
	}
}

func punchoutTukey(w []float64, p, start, end float64) {
	switch {
	case p <= 0:
		p = 0.05
	case p >= 1:
		p = 0.95
	}
	L := len(w)
	startN := int(start * float64(L))
	endN := int(end * float64(L))
	Ns := int(p / 2 * float64(startN))
	Ne := int(p / 2 * float64(L-endN))
	n := 0
	for i := 1; n < Ns && n < L; n, i = n+1, i+1 {
		w[n] = 0.5 - 0.5*math.Cos(math.Pi*float64(i)/float64(Ns))
	}
	for ; n < startN-Ns && n < L; n++ {
		w[n] = 1
	}
	for i := Ns; n < startN && n < L; n, i = n+1, i-1 {
		w[n] = 0.5 - 0.5*math.Cos(math.Pi*float64(i)/float64(Ns))
	}
	for ; n < endN && n < L; n++ {
		w[n] = 0
	}
	for i := 1; n < endN+Ne && n < L; n, i = n+1, i+1 {
		w[n] = 0.5 - 0.5*math.Cos(math.Pi*float64(i)/float64(Ne))
	}
	for ; n < L-Ne && n < L; n++ {
		w[n] = 1
	}
	for i := Ne; n < L; n, i = n+1, i-1 {
		w[n] = 0.5 - 0.5*math.Cos(math.Pi*float64(i)/float64(Ne))
	}
}
