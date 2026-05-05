package session

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Recorder accumulates mixed audio (user + assistant) as PCM-16 LE mono 8 kHz
// and flushes a valid WAV file on Save.
//
// Both WriteUser and WriteAssistant are safe for concurrent use. Audio is
// interleaved in the order goroutines acquire the mutex, which matches
// real-time arrival order closely enough for debugging and replay.
type Recorder struct {
	mu     sync.Mutex
	buf    []byte // PCM-16 LE samples
	source string // "twilio" | "browser"
}

func NewRecorder(source string) *Recorder {
	return &Recorder{source: source}
}

// OffsetMs returns the current elapsed time of the recorded audio in milliseconds.
// Since we record 8 kHz 16-bit PCM, there are 16 bytes per millisecond (8000 samples/sec * 2 bytes/sample / 1000 = 16 bytes/ms).
func (r *Recorder) OffsetMs() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.buf) / 16)
}

// WriteUser appends inbound user audio.
//   - Twilio:  8 kHz µ-law (PCMU, inverted bits) → decoded to PCM-16 LE 8 kHz.
//   - Browser: 16 kHz PCM-16 LE → decimated 2:1 to 8 kHz.
func (r *Recorder) WriteUser(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	var pcm []byte
	if r.source == "browser" {
		pcm = pcm16kTo8k(chunk)
	} else {
		pcm = mulawToPCM16(chunk)
	}
	r.mu.Lock()
	r.buf = append(r.buf, pcm...)
	r.mu.Unlock()
}

// WriteAssistant appends outbound assistant audio (always 8 kHz µ-law from ElevenLabs).
func (r *Recorder) WriteAssistant(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	pcm := mulawToPCM16(chunk)
	r.mu.Lock()
	r.buf = append(r.buf, pcm...)
	r.mu.Unlock()
}

// Save writes recordings/{sessionID}.wav and returns the path.
// It is safe to call Save while WriteUser/WriteAssistant are still running;
// it snapshots the buffer under the lock.
func (r *Recorder) Save(sessionID string) (string, error) {
	r.mu.Lock()
	data := make([]byte, len(r.buf))
	copy(data, r.buf)
	r.mu.Unlock()

	if len(data) == 0 {
		// Do not create empty recordings
		return "", nil
	}

	if err := os.MkdirAll("recordings", 0o755); err != nil {
		return "", fmt.Errorf("recorder mkdir: %w", err)
	}
	path := filepath.Join("recordings", sessionID+".wav")
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("recorder create: %w", err)
	}
	defer f.Close()

	if err := writeWAVHeader(f, 8000, 1, 16, len(data)); err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("recorder write data: %w", err)
	}
	return path, nil
}

// teeAudio fans out src into two channels. The primary channel (a) is sent to
// first and blocks if full; the recorder channel (b) receives a non-blocking
// best-effort copy so a stalled recorder never back-pressures the main audio
// pipeline. Both channels are closed when src closes or ctx is cancelled.
func teeAudio(ctx context.Context, src <-chan []byte) (primary <-chan []byte, recorder <-chan []byte) {
	a := make(chan []byte, 64)
	b := make(chan []byte, 64)
	go func() {
		defer close(a)
		defer close(b)
		for {
			select {
			case chunk, ok := <-src:
				if !ok {
					return
				}
				select {
				case a <- chunk:
				case <-ctx.Done():
					return
				}
				// Non-blocking: drop the recorder copy rather than stalling
				// the primary pipeline (e.g. when dispatcher stops reading on barge-in).
				select {
				case b <- chunk:
				default:
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	return a, b
}

// ---------------------------------------------------------------------------
// G.711 µ-law decode
// ---------------------------------------------------------------------------

// mulawTable maps PCMU byte values (inverted µ-law as transmitted by Twilio/RTP)
// to signed PCM-16 samples. Computed at init time using the CCITT G.711 algorithm
// (matches sox/libsndfile: bias=0x84, ~byte to un-invert PCMU).
var mulawTable [256]int16

func init() {
	const bias = int16(0x84) // 132
	for i := range mulawTable {
		u := ^byte(i) // undo PCMU bit-inversion
		mant := int16(u & 0x0F)
		exp := uint((u & 0x70) >> 4)
		t := ((mant << 3) + bias) << exp
		if u&0x80 != 0 {
			mulawTable[i] = bias - t
		} else {
			mulawTable[i] = t - bias
		}
	}
}

// mulawToPCM16 converts a slice of PCMU bytes to PCM-16 LE bytes (2× size).
func mulawToPCM16(ulaw []byte) []byte {
	out := make([]byte, len(ulaw)*2)
	for i, b := range ulaw {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(mulawTable[b]))
	}
	return out
}

// pcm16kTo8k decimates 16 kHz PCM-16 LE (browser) to 8 kHz by keeping
// every other sample. Simple 2:1 decimation is sufficient for voice.
func pcm16kTo8k(pcm []byte) []byte {
	nSamples := len(pcm) / 2
	out := make([]byte, (nSamples/2)*2)
	for i, j := 0, 0; i < nSamples-1; i += 2 {
		copy(out[j:j+2], pcm[i*2:i*2+2])
		j += 2
	}
	return out
}

// ---------------------------------------------------------------------------
// WAV header
// ---------------------------------------------------------------------------

func writeWAVHeader(f *os.File, sampleRate, channels, bitDepth, dataLen int) error {
	byteRate := sampleRate * channels * bitDepth / 8
	blockAlign := channels * bitDepth / 8

	h := make([]byte, 44)
	copy(h[0:], "RIFF")
	binary.LittleEndian.PutUint32(h[4:], uint32(36+dataLen))
	copy(h[8:], "WAVE")
	copy(h[12:], "fmt ")
	binary.LittleEndian.PutUint32(h[16:], 16) // chunk size
	binary.LittleEndian.PutUint16(h[20:], 1)  // PCM
	binary.LittleEndian.PutUint16(h[22:], uint16(channels))
	binary.LittleEndian.PutUint32(h[24:], uint32(sampleRate))
	binary.LittleEndian.PutUint32(h[28:], uint32(byteRate))
	binary.LittleEndian.PutUint16(h[32:], uint16(blockAlign))
	binary.LittleEndian.PutUint16(h[34:], uint16(bitDepth))
	copy(h[36:], "data")
	binary.LittleEndian.PutUint32(h[40:], uint32(dataLen))

	_, err := f.Write(h)
	return err
}
