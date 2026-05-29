package audio

import (
	"encoding/binary"

	webrtcvad "github.com/baabaaox/go-webrtcvad"
)

const (
	vadMergeGapFrames = 15             // 合并间隔小于 300ms 的相邻语音区
	vadPaddingSamples = sampleRate / 5 // 语音区前后各保留 200ms,避免切掉词首词尾
	minSpeechSamples  = sampleRate / 2 // 语音总时长小于 0.5s 视为误触发,丢弃
)

type vadGate struct {
	inst  webrtcvad.VadInst
	frame []byte
}

func newVAD(mode int) (*vadGate, error) {
	inst := webrtcvad.Create()
	if err := webrtcvad.Init(inst); err != nil {
		webrtcvad.Free(inst)
		return nil, err
	}
	if err := webrtcvad.SetMode(inst, mode); err != nil {
		webrtcvad.Free(inst)
		return nil, err
	}
	return &vadGate{inst: inst, frame: make([]byte, vadFrameSamples*2)}, nil
}

func (v *vadGate) Close() {
	webrtcvad.Free(v.inst)
}

// speechRegions detects voiced frames and groups them into sample ranges
// [start,end). Adjacent regions separated by a short gap are merged, each region
// is padded to keep word edges, and regions below the minimum length are dropped.
// Cutting silence out of a segment before transcription means only spoken audio
// is sent and billed.
func (v *vadGate) speechRegions(seg []int16) [][2]int {
	nFrames := len(seg) / vadFrameSamples
	if nFrames == 0 {
		return nil
	}
	voiced := make([]bool, nFrames)
	for i := 0; i < nFrames; i++ {
		off := i * vadFrameSamples
		for j := 0; j < vadFrameSamples; j++ {
			binary.LittleEndian.PutUint16(v.frame[j*2:], uint16(seg[off+j]))
		}
		active, err := webrtcvad.Process(v.inst, sampleRate, v.frame, vadFrameSamples)
		voiced[i] = err == nil && active
	}

	// 填充小 gap:相邻语音帧间隔不超过阈值时,把中间帧并入语音,避免一句话被切碎。
	last := -1
	for i := 0; i < nFrames; i++ {
		if !voiced[i] {
			continue
		}
		if last >= 0 && i-last <= vadMergeGapFrames {
			for k := last + 1; k < i; k++ {
				voiced[k] = true
			}
		}
		last = i
	}

	var regions [][2]int
	i := 0
	for i < nFrames {
		if !voiced[i] {
			i++
			continue
		}
		start := i
		for i < nFrames && voiced[i] {
			i++
		}
		s := start*vadFrameSamples - vadPaddingSamples
		e := i*vadFrameSamples + vadPaddingSamples
		if s < 0 {
			s = 0
		}
		if e > len(seg) {
			e = len(seg)
		}
		if n := len(regions); n > 0 && s <= regions[n-1][1] {
			regions[n-1][1] = e
		} else {
			regions = append(regions, [2]int{s, e})
		}
	}

	out := regions[:0]
	for _, r := range regions {
		if r[1]-r[0] >= minSpeechSamples {
			out = append(out, r)
		}
	}
	return out
}
