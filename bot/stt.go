package bot

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// sttURL is the base URL of the speech-to-text sidecar. Set in setupEars from
// the STT_URL env var (default http://localhost:8002 for local dev).
var sttURL string

// sttSem caps STT requests to one in flight. When the sidecar is busy, extra
// utterances are dropped rather than queued, so a backlog can't build up and
// spam "context deadline exceeded". Acquired in handleUtterance.
var sttSem = make(chan struct{}, 1)

// transcribe sends one utterance's Opus frames to the STT sidecar and returns
// the recognized text. Frames are serialized as [uint16 length][frame] repeated.
func transcribe(frames [][]byte) (string, error) {
	if sttURL == "" {
		return "", fmt.Errorf("STT URL not configured")
	}

	var body bytes.Buffer
	var lp [2]byte
	for _, f := range frames {
		binary.BigEndian.PutUint16(lp[:], uint16(len(f)))
		body.Write(lp[:])
		body.Write(f)
	}

	req, err := http.NewRequest(http.MethodPost, sttURL+"/transcribe", &body)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")

	client := &http.Client{Timeout: 60 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(res.Body)
		return "", fmt.Errorf("stt sidecar returned %d: %s", res.StatusCode, string(b))
	}

	var out struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Text, nil
}
