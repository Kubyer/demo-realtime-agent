package session

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RecorderPaths holds the WAV file paths produced by Recorder.Save.
// User and Agent paths are empty if no audio was recorded for that side.
type RecorderPaths struct {
	Mixed string // interleaved user+agent, always the primary recording
	User  string // user audio only — used for overlap analysis
	Agent string // agent (TTS) audio only — used for MOS scoring
}

// HasAny returns true if at least the mixed recording was written.
func (p RecorderPaths) HasAny() bool { return p.Mixed != "" }

// Recorder accumulates audio separately for the user (inbound) and the
// assistant (TTS outbound), then flushes valid WAV files on Save.
//
// Three files are produced:
//   - recordings/{id}.wav          — mixed (interleaved arrival order)
//   - recordings/{id}-user.wav     — user only (for overlap analysis)
//   - recordings/{id}-agent.wav    — agent only (for MOS scoring)
//
// All Write* methods are safe for concurrent use.
type Recorder struct {
	mu        sync.Mutex
	mixBuf    []byte // interleaved PCM-16 LE 8 kHz (user + agent)
	userBuf   []byte // user only
	agentBuf  []byte // agent only
	source    string // "twilio" | "browser"
}

func NewRecorder(source string) *Recorder {
	return &Recorder{source: source}
}

// OffsetMs returns elapsed recording time in milliseconds.
// 8 kHz × 16-bit = 16 bytes per millisecond.
func (r *Recorder) OffsetMs() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int64(len(r.mixBuf) / 16)
}

// WriteUser appends inbound user audio.
//   - Twilio:  8 kHz µ-law → decoded to PCM-16 LE 8 kHz.
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
	r.mixBuf = append(r.mixBuf, pcm...)
	r.userBuf = append(r.userBuf, pcm...)
	r.mu.Unlock()
}

// WriteAssistant appends outbound assistant audio (always 8 kHz µ-law from TTS).
func (r *Recorder) WriteAssistant(chunk []byte) {
	if len(chunk) == 0 {
		return
	}
	pcm := mulawToPCM16(chunk)
	r.mu.Lock()
	r.mixBuf = append(r.mixBuf, pcm...)
	r.agentBuf = append(r.agentBuf, pcm...)
	r.mu.Unlock()
}

// Save writes up to three WAV files under recordings/ and returns their paths.
// It snapshots buffers under the lock so concurrent writes during Save are safe.
// Files for empty channels (e.g. no agent audio recorded) are skipped.
func (r *Recorder) Save(sessionID string) (RecorderPaths, error) {
	r.mu.Lock()
	mix := make([]byte, len(r.mixBuf))
	copy(mix, r.mixBuf)
	user := make([]byte, len(r.userBuf))
	copy(user, r.userBuf)
	agent := make([]byte, len(r.agentBuf))
	copy(agent, r.agentBuf)
	r.mu.Unlock()

	if len(mix) == 0 {
		return RecorderPaths{}, nil
	}

	// Use the absolute path of recordings/ so QA and other callers
	// can open the files regardless of their own working directory.
	recDir, err := filepath.Abs("recordings")
	if err != nil {
		return RecorderPaths{}, fmt.Errorf("recorder abs path: %w", err)
	}
	if err := os.MkdirAll(recDir, 0o755); err != nil {
		return RecorderPaths{}, fmt.Errorf("recorder mkdir: %w", err)
	}

	var paths RecorderPaths

	if paths.Mixed, err = writeWAV(filepath.Join(recDir, sessionID+".wav"), mix); err != nil {
		return paths, err
	}
	if len(user) > 0 {
		paths.User, _ = writeWAV(filepath.Join(recDir, sessionID+"-user.wav"), user)
	}
	if len(agent) > 0 {
		paths.Agent, _ = writeWAV(filepath.Join(recDir, sessionID+"-agent.wav"), agent)
	}
	return paths, nil
}

// writeWAV creates a PCM-16 LE mono 8 kHz WAV file at path and returns the path.
func writeWAV(path string, data []byte) (string, error) {
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("recorder create %s: %w", path, err)
	}
	defer f.Close()
	if err := writeWAVHeader(f, 8000, 1, 16, len(data)); err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		return "", fmt.Errorf("recorder write data %s: %w", path, err)
	}
	return path, nil
}

// teeAudio fans out src into two channels. The primary channel (a) is always
// served first. The recorder channel (b) is best-effort: drops if full so a
// stalled recorder never back-pressures the main audio pipeline.
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

var mulawTable [256]int16

func init() {
	const bias = int16(0x84)
	for i := range mulawTable {
		u := ^byte(i)
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

func mulawToPCM16(ulaw []byte) []byte {
	out := make([]byte, len(ulaw)*2)
	for i, b := range ulaw {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(mulawTable[b]))
	}
	return out
}

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
	binary.LittleEndian.PutUint32(h[16:], 16)
	binary.LittleEndian.PutUint16(h[20:], 1)
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
