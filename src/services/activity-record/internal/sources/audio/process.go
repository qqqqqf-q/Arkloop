package audio

import "math"

const (
	targetRMS = 0.2
	peakLimit = 0.95
)

func int16ToFloat(seg []int16) []float64 {
	out := make([]float64, len(seg))
	for i, s := range seg {
		out[i] = float64(s) / 32768.0
	}
	return out
}

func rmsInt16(seg []int16) float64 {
	if len(seg) == 0 {
		return 0
	}
	var sum float64
	for _, s := range seg {
		f := float64(s) / 32768.0
		sum += f * f
	}
	return math.Sqrt(sum / float64(len(seg)))
}

func rmsFloat(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	var sum float64
	for _, v := range x {
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(x)))
}

// normalize scales the segment toward targetRMS while keeping the peak below
// peakLimit. Returns the input unchanged when it carries no usable energy.
func normalize(seg []int16) []int16 {
	f := int16ToFloat(seg)
	rms := rmsFloat(f)
	if rms < 1e-6 {
		return seg
	}
	var peak float64
	for _, v := range f {
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	scale := targetRMS / rms
	if peak*scale > peakLimit {
		scale = peakLimit / peak
	}
	out := make([]int16, len(seg))
	for i, v := range f {
		s := v * scale * 32768.0
		switch {
		case s > 32767:
			s = 32767
		case s < -32768:
			s = -32768
		}
		out[i] = int16(s)
	}
	return out
}
