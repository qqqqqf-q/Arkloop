package audio

import "math"

// Music detection borrows screenpipe's heuristic: steady audio with low energy
// variance, low zero-crossing-rate variance, and non-noise spectral flatness is
// classified as music and dropped before transcription. Thresholds are evaluated
// per 0.5s window; the segment is music when a majority of energetic windows are.
const (
	musicWindowSamples = sampleRate / 2 // 0.5s
	evrThreshold       = 0.30
	zcrVarThreshold    = 0.04
	flatnessVeto       = 0.70
	fftSize            = 4096
)

func isMusic(seg []int16) bool {
	f := int16ToFloat(seg)
	windows := 0
	music := 0
	for i := 0; i+musicWindowSamples <= len(f); i += musicWindowSamples {
		w := f[i : i+musicWindowSamples]
		if rmsFloat(w) < rmsSilence {
			continue
		}
		windows++
		if windowIsMusic(w) {
			music++
		}
	}
	if windows == 0 {
		return false
	}
	return float64(music)/float64(windows) > 0.5
}

func windowIsMusic(w []float64) bool {
	if energyVarianceRatio(w) >= evrThreshold {
		return false
	}
	if zcrVariance(w) >= zcrVarThreshold {
		return false
	}
	if spectralFlatness(w) > flatnessVeto {
		return false
	}
	return true
}

func energyVarianceRatio(w []float64) float64 {
	const sub = 10
	n := len(w) / sub
	if n == 0 {
		return 1
	}
	rmsVals := make([]float64, 0, sub)
	for i := 0; i < sub; i++ {
		rmsVals = append(rmsVals, rmsFloat(w[i*n:(i+1)*n]))
	}
	return coefficientOfVariation(rmsVals)
}

func coefficientOfVariation(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	var mean float64
	for _, v := range x {
		mean += v
	}
	mean /= float64(len(x))
	if mean < 1e-9 {
		return 0
	}
	return math.Sqrt(populationVariance(x, mean)) / mean
}

func zcrVariance(w []float64) float64 {
	const sub = 10
	n := len(w) / sub
	if n < 2 {
		return 1
	}
	zcrs := make([]float64, 0, sub)
	for i := 0; i < sub; i++ {
		s := w[i*n : (i+1)*n]
		crossings := 0
		for j := 1; j < len(s); j++ {
			if (s[j-1] >= 0) != (s[j] >= 0) {
				crossings++
			}
		}
		zcrs = append(zcrs, float64(crossings)/float64(len(s)-1))
	}
	var mean float64
	for _, v := range zcrs {
		mean += v
	}
	mean /= float64(len(zcrs))
	return populationVariance(zcrs, mean)
}

func populationVariance(x []float64, mean float64) float64 {
	if len(x) == 0 {
		return 0
	}
	var v float64
	for _, val := range x {
		d := val - mean
		v += d * d
	}
	return v / float64(len(x))
}

// spectralFlatness returns the geometric/arithmetic mean ratio of the magnitude
// spectrum. White noise approaches 1; tonal content trends toward 0.
func spectralFlatness(w []float64) float64 {
	n := fftSize
	if len(w) < n {
		n = prevPow2(len(w))
	}
	if n < 2 {
		return 1
	}
	re := make([]float64, n)
	im := make([]float64, n)
	for i := 0; i < n; i++ {
		hann := 0.5 - 0.5*math.Cos(2*math.Pi*float64(i)/float64(n-1))
		re[i] = w[i] * hann
	}
	fft(re, im)

	half := n / 2
	var logSum, linSum float64
	count := 0
	for k := 1; k < half; k++ {
		mag := math.Sqrt(re[k]*re[k] + im[k]*im[k])
		if mag < 1e-12 {
			mag = 1e-12
		}
		logSum += math.Log(mag)
		linSum += mag
		count++
	}
	if count == 0 || linSum == 0 {
		return 1
	}
	geoMean := math.Exp(logSum / float64(count))
	arithMean := linSum / float64(count)
	return geoMean / arithMean
}

// fft performs an in-place iterative radix-2 Cooley-Tukey transform. len(re)
// must be a power of two and equal to len(im).
func fft(re, im []float64) {
	n := len(re)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			re[i], re[j] = re[j], re[i]
			im[i], im[j] = im[j], im[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := -2 * math.Pi / float64(length)
		wlenRe := math.Cos(ang)
		wlenIm := math.Sin(ang)
		for i := 0; i < n; i += length {
			wRe, wIm := 1.0, 0.0
			for j := 0; j < length/2; j++ {
				aRe := re[i+j]
				aIm := im[i+j]
				bRe := re[i+j+length/2]*wRe - im[i+j+length/2]*wIm
				bIm := re[i+j+length/2]*wIm + im[i+j+length/2]*wRe
				re[i+j] = aRe + bRe
				im[i+j] = aIm + bIm
				re[i+j+length/2] = aRe - bRe
				im[i+j+length/2] = aIm - bIm
				wRe, wIm = wRe*wlenRe-wIm*wlenIm, wRe*wlenIm+wIm*wlenRe
			}
		}
	}
}

func prevPow2(n int) int {
	p := 1
	for p*2 <= n {
		p *= 2
	}
	return p
}
