package flac

import (
	"math"
	"strings"
	"testing"
)

// cosineSum evaluates the generalized cosine window
// a0 - a1*cos(2*pi*x) + a2*cos(4*pi*x) - a3*cos(6*pi*x) + a4*cos(8*pi*x).
func cosineSum(x float64, a ...float64) float64 {
	v, sign := 0.0, 1.0
	for k, ak := range a {
		v += sign * ak * math.Cos(2*math.Pi*float64(k)*x)
		sign = -sign
	}
	return v
}

// windowDefinitions gives each fixed window as a function of the sample
// index n and the window length L, written from the textbook definitions
// (N = L-1, x = n/N). The cosine-sum coefficients are the published ones:
// Blackman, Blackman-Harris 4-term (-92 dB), Blackman-Nuttall ("nuttall"),
// the flat top window, and the 4-term Kaiser-Bessel approximation.
var windowDefinitions = map[string]func(n, L int) float64{
	"bartlett": func(n, L int) float64 { return 1 - math.Abs(2*float64(n)/float64(L-1)-1) },
	"bartlett_hann": func(n, L int) float64 {
		x := float64(n) / float64(L-1)
		return 0.62 - 0.48*math.Abs(x-0.5) - 0.38*math.Cos(2*math.Pi*x)
	},
	"blackman": func(n, L int) float64 { return cosineSum(float64(n)/float64(L-1), 0.42, 0.5, 0.08) },
	"blackman_harris_4term_92db": func(n, L int) float64 {
		return cosineSum(float64(n)/float64(L-1), 0.35875, 0.48829, 0.14128, 0.01168)
	},
	"connes": func(n, L int) float64 {
		k := 2*float64(n)/float64(L-1) - 1
		return (1 - k*k) * (1 - k*k)
	},
	"flattop": func(n, L int) float64 {
		return cosineSum(float64(n)/float64(L-1), 0.21557895, 0.41663158, 0.277263158, 0.083578947, 0.006947368)
	},
	"gauss(0.25)": func(n, L int) float64 {
		h := float64(L-1) / 2
		k := (float64(n) - h) / (0.25 * h)
		return math.Exp(-k * k / 2)
	},
	"hamming": func(n, L int) float64 { return 0.54 - 0.46*math.Cos(2*math.Pi*float64(n)/float64(L-1)) },
	"hann":    func(n, L int) float64 { return 0.5 - 0.5*math.Cos(2*math.Pi*float64(n)/float64(L-1)) },
	"kaiser_bessel": func(n, L int) float64 {
		return cosineSum(float64(n)/float64(L-1), 0.402, 0.498, 0.098, 0.001)
	},
	"nuttall": func(n, L int) float64 {
		return cosineSum(float64(n)/float64(L-1), 0.3635819, 0.4891775, 0.1365995, 0.0106411)
	},
	"rectangle": func(n, L int) float64 { return 1 },
	// The triangle window does not reach zero at its ends: it is the
	// Bartlett window of length L+2 without its zero end points.
	"triangle": func(n, L int) float64 { return 1 - math.Abs(float64(2*n+1-L))/float64(L+1) },
	"welch": func(n, L int) float64 {
		k := 2*float64(n)/float64(L-1) - 1
		return 1 - k*k
	},
}

func computeSpec(t *testing.T, spec string, L int) []float64 {
	t.Helper()
	apods := parseApodization(spec)
	if len(apods) != 1 {
		t.Fatalf("%s: parsed %d windows", spec, len(apods))
	}
	w := make([]float64, L)
	computeWindow(w, apods[0])
	return w
}

// Every fixed window matches its definition and is symmetric.
func TestWindowsMatchDefinitions(t *testing.T) {
	for spec, def := range windowDefinitions {
		for _, L := range []int{2, 3, 16, 17, 64, 588, 4608} {
			w := computeSpec(t, spec, L)
			for n := range L {
				if want := def(n, L); math.Abs(w[n]-want) > 1e-12 {
					t.Fatalf("%s L=%d: w[%d] = %.15g, want %.15g", spec, L, n, w[n], want)
				}
				if math.Abs(w[n]-w[L-1-n]) > 1e-12 {
					t.Fatalf("%s L=%d: not symmetric at %d", spec, L, n)
				}
			}
		}
	}
	// A one-sample window is 1 for every kind.
	for spec := range windowDefinitions {
		if w := computeSpec(t, spec, 1); w[0] != 1 {
			t.Errorf("%s L=1: %v", spec, w)
		}
	}
}

// Characteristic values that follow directly from the definitions.
func TestWindowCharacteristicValues(t *testing.T) {
	const L = 17 // odd, so the centre sample is n = 8
	checks := []struct {
		spec        string
		end, centre float64
	}{
		{"bartlett", 0, 1},
		{"hann", 0, 1},
		{"hamming", 0.08, 1},
		{"blackman", 0, 1},
		{"welch", 0, 1},
		{"connes", 0, 1},
		{"rectangle", 1, 1},
		{"triangle", 1.0 / 9, 1},
		{"gauss(0.25)", math.Exp(-8), 1},
	}
	for _, c := range checks {
		w := computeSpec(t, c.spec, L)
		if math.Abs(w[0]-c.end) > 1e-12 || math.Abs(w[L/2]-c.centre) > 1e-12 {
			t.Errorf("%s: w[0] = %v, w[centre] = %v, want %v and %v", c.spec, w[0], w[L/2], c.end, c.centre)
		}
	}
	// Connes is the square of Welch.
	welch, connes := computeSpec(t, "welch", 100), computeSpec(t, "connes", 100)
	for n := range welch {
		if math.Abs(connes[n]-welch[n]*welch[n]) > 1e-12 {
			t.Fatalf("connes[%d] != welch[%d]^2", n, n)
		}
	}
}

// Tukey(p) is a rectangle for p <= 0, a Hann window for p >= 1, and
// otherwise a flat top with raised cosine tapers of Np+1 samples
// (Np = floor(p/2*L) - 1) at both ends.
func TestTukeyWindow(t *testing.T) {
	for _, L := range []int{16, 17, 64, 4608} {
		w := make([]float64, L)
		tukey(w, 0)
		for n := range w {
			if w[n] != 1 {
				t.Fatalf("tukey(0) L=%d: w[%d] = %v", L, n, w[n])
			}
		}
		tukey(w, 1)
		hn := computeSpec(t, "hann", L)
		for n := range w {
			if w[n] != hn[n] {
				t.Fatalf("tukey(1) L=%d differs from hann at %d", L, n)
			}
		}
		for _, p := range []float64{0.1, 0.25, 0.5, 0.9} {
			tukey(w, p)
			Np := int(p/2*float64(L)) - 1
			for n := range L {
				want := 1.0
				switch {
				case Np > 0 && n <= Np:
					want = 0.5 - 0.5*math.Cos(math.Pi*float64(n)/float64(Np))
				case Np > 0 && n >= L-1-Np:
					want = 0.5 - 0.5*math.Cos(math.Pi*float64(L-1-n)/float64(Np))
				}
				if math.Abs(w[n]-want) > 1e-12 {
					t.Fatalf("tukey(%v) L=%d: w[%d] = %v, want %v", p, L, n, w[n], want)
				}
			}
		}
	}
	// The default specification is tukey(0.5).
	got := computeSpec(t, "", 64)
	want := make([]float64, 64)
	tukey(want, 0.5)
	for n := range got {
		if got[n] != want[n] {
			t.Fatalf("default window differs from tukey(0.5) at %d", n)
		}
	}
}

// Partial Tukey windows are zero outside their segment [start*L, end*L)
// and one inside it away from the tapers; punchout Tukey windows are the
// opposite. All values lie in [0, 1].
func TestPartialAndPunchoutTukey(t *testing.T) {
	const L = 4608
	for _, spec := range []string{"partial_tukey(2)", "partial_tukey(3/0.5/0.3)", "punchout_tukey(3)", "punchout_tukey(4/0.1)"} {
		apods := parseApodization(spec)
		if len(apods) < 2 {
			t.Fatalf("%s: %d windows", spec, len(apods))
		}
		for _, a := range apods {
			w := make([]float64, L)
			computeWindow(w, a)
			startN, endN := int(a.start*L), int(a.end*L)
			if startN >= endN {
				t.Fatalf("%s: empty segment %+v", spec, a)
			}
			taper := int(a.p / 2 * L) // generous bound on any taper length
			for n, v := range w {
				if v < 0 || v > 1 {
					t.Fatalf("%s %+v: w[%d] = %v out of range", spec, a, n, v)
				}
				inside := n >= startN && n < endN
				deepInside := n >= startN+taper && n < endN-taper
				farOutside := n < startN-taper || n >= endN+taper
				switch a.kind {
				case winPartialTukey:
					if !inside && v != 0 {
						t.Fatalf("%s %+v: w[%d] = %v outside the segment", spec, a, n, v)
					}
					if deepInside && v != 1 {
						t.Fatalf("%s %+v: w[%d] = %v inside the segment", spec, a, n, v)
					}
				case winPunchoutTukey:
					if inside && v != 0 {
						t.Fatalf("%s %+v: w[%d] = %v inside the punched-out segment", spec, a, n, v)
					}
					if farOutside && n >= taper && n < L-taper && v != 1 {
						t.Fatalf("%s %+v: w[%d] = %v outside the punched-out segment", spec, a, n, v)
					}
				default:
					t.Fatalf("%s: unexpected kind %v", spec, a.kind)
				}
			}
		}
	}
}

func TestParseApodization(t *testing.T) {
	tukeyDefault := []apodization{{kind: winTukey, p: 0.5}}
	tests := []struct {
		spec string
		want []apodization
	}{
		{"", tukeyDefault},
		{"bogus", tukeyDefault},
		{"tukey(1.5)", tukeyDefault}, // out of range: ignored
		{"gauss(0.6)", tukeyDefault}, // out of range: ignored
		{"hamming", []apodization{{kind: winHamming}}},
		{"bogus;hann;", []apodization{{kind: winHann}}},
		{"tukey(5e-1)", []apodization{{kind: winTukey, p: 0.5}}},
		{"gauss(0.25)", []apodization{{kind: winGauss, p: 0.25}}},
		// The testOptions parameters: zero parts degrade to tukey(P).
		{"partial_tukey(0/1.0/1.0)", []apodization{{kind: winTukey, p: 1}}},
		{"punchout_tukey(1)", []apodization{{kind: winTukey, p: 0.2}}},
		{"partial_tukey(2)", []apodization{
			{kind: winPartialTukey, p: 0.2, start: 0, end: (1 + 1/0.9 - 1) / (2 + 1/0.9 - 1)},
			{kind: winPartialTukey, p: 0.2, start: 1 / (2 + 1/0.9 - 1), end: 1},
		}},
	}
	for _, tc := range tests {
		got := parseApodization(tc.spec)
		if len(got) != len(tc.want) {
			t.Errorf("%q: got %d windows %+v, want %+v", tc.spec, len(got), got, tc.want)
			continue
		}
		for i := range got {
			g, w := got[i], tc.want[i]
			if g.kind != w.kind || math.Abs(g.p-w.p) > 1e-12 || math.Abs(g.start-w.start) > 1e-12 || math.Abs(g.end-w.end) > 1e-12 {
				t.Errorf("%q[%d] = %+v, want %+v", tc.spec, i, g, w)
			}
		}
	}

	// At most 32 windows.
	if n := len(parseApodization(strings.Repeat("hann;", 40))); n != maxApodizations {
		t.Errorf("40 windows parsed to %d, want %d", n, maxApodizations)
	}
	if n := len(parseApodization("partial_tukey(32);hann")); n != 1 {
		t.Errorf("too many partial_tukey parts: got %d windows, want 1 (hann only)", n)
	}
}

func TestStrtod(t *testing.T) {
	tests := map[string]float64{
		"0.5)": 0.5, "5e-1)": 0.5, "1.0/1.0)": 1, " 2)": 2, "-1": -1, "x": 0, "": 0, ".5": 0.5, "3e": 3, "1e+1x": 10,
	}
	for in, want := range tests {
		if got := parseFloatPrefix(in); got != want {
			t.Errorf("parseFloatPrefix(%q) = %v, want %v", in, got, want)
		}
	}
}
