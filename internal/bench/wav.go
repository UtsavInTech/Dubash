// Copyright 2026 the lenguaraz India fork contributors. Apache-2.0.

package bench

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// SampleRate is the wire format every engine receives: PCM16 mono 16 kHz,
// the same as asr.BytesPerSecond.
const SampleRate = 16000

// ReadWAV loads a PCM16 WAV file of any sample rate and channel count and
// returns it as PCM16 mono at SampleRate, little endian. Other encodings
// (float, mp3, m4a) must be converted first, for example with
// `ffmpeg -i in.m4a -ac 1 -ar 16000 -sample_fmt s16 out.wav`.
func ReadWAV(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeWAV(f)
}

func decodeWAV(r io.Reader) ([]byte, error) {
	var riff [12]byte
	if _, err := io.ReadFull(r, riff[:]); err != nil {
		return nil, fmt.Errorf("wav header: %w", err)
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return nil, errors.New("not a RIFF/WAVE file")
	}
	var (
		format, channels, bits uint16
		rate                   uint32
		haveFmt                bool
	)
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			return nil, errors.New("wav: no data chunk")
		}
		id, size := string(hdr[0:4]), binary.LittleEndian.Uint32(hdr[4:8])
		switch id {
		case "fmt ":
			buf := make([]byte, size)
			if _, err := io.ReadFull(r, buf); err != nil || size < 16 {
				return nil, errors.New("wav: bad fmt chunk")
			}
			format = binary.LittleEndian.Uint16(buf[0:2])
			channels = binary.LittleEndian.Uint16(buf[2:4])
			rate = binary.LittleEndian.Uint32(buf[4:8])
			bits = binary.LittleEndian.Uint16(buf[14:16])
			if format == 0xFFFE && size >= 26 { // WAVE_FORMAT_EXTENSIBLE: subformat GUID starts at 24
				format = binary.LittleEndian.Uint16(buf[24:26])
			}
			haveFmt = true
		case "data":
			if !haveFmt {
				return nil, errors.New("wav: data before fmt")
			}
			if format != 1 || bits != 16 || channels == 0 {
				return nil, fmt.Errorf("wav: need 16-bit PCM, got format %d, %d bits; convert with ffmpeg -ac 1 -ar 16000 -sample_fmt s16", format, bits)
			}
			data, err := io.ReadAll(io.LimitReader(r, int64(size)))
			if err != nil {
				return nil, err
			}
			return resample(downmix(data, int(channels)), int(rate)), nil
		default:
			if _, err := io.CopyN(io.Discard, r, int64(size+size%2)); err != nil {
				return nil, errors.New("wav: truncated chunk")
			}
		}
	}
}

// downmix averages interleaved channels into one.
func downmix(pcm []byte, channels int) []int16 {
	frames := len(pcm) / (2 * channels)
	out := make([]int16, frames)
	for i := range frames {
		var sum int
		for c := range channels {
			o := 2 * (i*channels + c)
			sum += int(int16(binary.LittleEndian.Uint16(pcm[o:])))
		}
		out[i] = int16(sum / channels)
	}
	return out
}

// resample converts samples at rate to SampleRate by linear interpolation:
// good enough for speech recognition, not for listening.
func resample(in []int16, rate int) []byte {
	if rate == SampleRate || rate <= 0 {
		return pcmBytes(in)
	}
	n := int(int64(len(in)) * SampleRate / int64(rate))
	out := make([]int16, n)
	step := float64(rate) / SampleRate
	for i := range n {
		pos := float64(i) * step
		j := int(pos)
		frac := pos - float64(j)
		a := float64(in[min(j, len(in)-1)])
		b := float64(in[min(j+1, len(in)-1)])
		out[i] = int16(a + (b-a)*frac)
	}
	return pcmBytes(out)
}

func pcmBytes(s []int16) []byte {
	out := make([]byte, 2*len(s))
	for i, v := range s {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(v))
	}
	return out
}

// EncodeWAV wraps PCM16 mono SampleRate audio in a WAV header, for
// providers that take files.
func EncodeWAV(pcm []byte) []byte {
	out := make([]byte, 44+len(pcm))
	copy(out[0:], "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(36+len(pcm)))
	copy(out[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], 1)
	binary.LittleEndian.PutUint32(out[24:], SampleRate)
	binary.LittleEndian.PutUint32(out[28:], SampleRate*2)
	binary.LittleEndian.PutUint16(out[32:], 2)
	binary.LittleEndian.PutUint16(out[34:], 16)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(len(pcm)))
	copy(out[44:], pcm)
	return out
}

// Seconds of PCM16 mono SampleRate audio.
func Seconds(pcm []byte) float64 { return float64(len(pcm)) / (2 * SampleRate) }
