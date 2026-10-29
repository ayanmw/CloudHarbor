package downloader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type cancellingHashReader struct {
	cancel context.CancelFunc
	reads  int
	eof    bool
}

func (r *cancellingHashReader) Read(p []byte) (int, error) {
	r.reads++
	if r.reads > 1 {
		return 0, io.EOF
	}
	copy(p, "abc")
	r.cancel()
	if r.eof {
		return 3, io.EOF
	}
	return 3, nil
}

func TestHashStopsOnCancellationIncludingFinalRead(t *testing.T) {
	for _, eof := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		reader := &cancellingHashReader{cancel: cancel, eof: eof}
		digest, err := hashReader(ctx, reader)
		if !errors.Is(err, context.Canceled) || digest != "" || reader.reads != 1 {
			t.Fatalf("cancelled hash returned receipt or kept reading: digest=%q error=%v reads=%d", digest, err, reader.reads)
		}
	}
}

func TestHashSuccessAndCancelledFileOpen(t *testing.T) {
	digest, err := hashReader(context.Background(), strings.NewReader("abc"))
	want := sha256.Sum256([]byte("abc"))
	if err != nil || digest != hex.EncodeToString(want[:]) {
		t.Fatalf("wrong digest: %q %v", digest, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := hashFile(ctx, filepath.Join(t.TempDir(), "missing")); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel did not precede file access: %v", err)
	}
}

func TestCancelledPartialHashDoesNotTruncateBoundData(t *testing.T) {
	d, _ := setup(t, func(_ http.ResponseWriter, _ *http.Request) { t.Error("unexpected network request") })
	const size = 64 << 20
	info := file(1, "large.bin", size)
	key := sha256.Sum256([]byte("large.bin"))
	state := filepath.Join(d.cfg.LocalDownDir, stateDirectory, hex.EncodeToString(key[:]))
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(state, "content.part")
	if err := os.WriteFile(partial, make([]byte, size), 0600); err != nil {
		t.Fatal(err)
	}
	record := recordFor(info, "large.bin")
	if err := writeRecord(filepath.Join(state, "partial.json"), record); err != nil {
		t.Fatal(err)
	}
	record.SHA256 = strings.Repeat("f", 64) // Invalid receipt must not cause a reset after cancellation.
	if err := writeRecord(filepath.Join(state, "complete.json"), record); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timer := time.AfterFunc(5*time.Millisecond, cancel)
	defer timer.Stop()
	err := d.transfer(ctx, info, "large.bin", filepath.Join(d.cfg.LocalDownDir, "large.bin"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation lost: %v", err)
	}
	stat, err := os.Stat(partial)
	if err != nil || stat.Size() != size {
		t.Fatalf("cancelled verification discarded partial: %v size=%v", err, stat)
	}
}
