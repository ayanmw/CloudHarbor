package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A resumed file must not attribute bytes from the previous attempt to this one.
func TestTransferFinishedLogsBytesWrittenThisAttempt(t *testing.T) {
	for _, ignoreRange := range []bool{false, true} {
		t.Run(fmt.Sprint(ignoreRange), func(t *testing.T) {
			var output bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&output)
			defer log.SetOutput(previous)
			requests := 0
			d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if requests == 1 {
					w.Header().Set("Content-Length", "6")
					io.WriteString(w, "abc")
					return
				}
				if r.Header.Get("Range") == "bytes=3-" && !ignoreRange {
					w.Header().Set("Content-Range", "bytes 3-5/6")
					w.WriteHeader(http.StatusPartialContent)
					io.WriteString(w, "def")
					return
				}
				io.WriteString(w, "abcdef")
			})
			info := file(1, "a.txt", 6)
			local := filepath.Join(d.cfg.LocalDownDir, "a.txt")
			run := func(wantErr bool, wantBytes int) {
				t.Helper()
				output.Reset()
				err := d.transfer(context.Background(), info, "a.txt", local)
				if (err != nil) != wantErr {
					t.Fatalf("transfer error = %v, want error %v", err, wantErr)
				}
				var finished string
				for _, line := range strings.Split(output.String(), "\n") {
					if strings.Contains(line, "event=transfer_finished ") {
						finished = line
					}
				}
				want := fmt.Sprintf("written_bytes_this_attempt=%d ", wantBytes)
				if !strings.Contains(finished, want) {
					t.Fatalf("want %q in finish log: %s", want, finished)
				}
			}
			run(true, 3)
			remaining := 3
			if ignoreRange {
				remaining = 6
			}
			run(false, remaining)
			run(false, 0) // Reusing a verified local file performs no new writes.
		})
	}
}

func TestTransferFinishedLogsNoWritesWhenAlreadyCancelled(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	requests := 0
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { requests++ })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	info := file(1, "a.txt", 6)
	if err := d.transfer(ctx, info, "a.txt", filepath.Join(d.cfg.LocalDownDir, "a.txt")); err != context.Canceled {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if requests != 0 || !strings.Contains(output.String(), "event=transfer_finished outcome=cancelled written_bytes_this_attempt=0 ") {
		t.Fatalf("requests=%d logs=%s", requests, output.String())
	}
}

// Even a failing Write may have committed a prefix to the partial file.
func TestTransferFinishedLogsPartialLocalWrite(t *testing.T) {
	for _, writeErr := range []error{nil, io.ErrClosedPipe} {
		t.Run(fmt.Sprint(writeErr), func(t *testing.T) {
			var output bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&output)
			defer log.SetOutput(previous)
			d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abcdef") })
			oldWrite := writePartialChunk
			writePartialChunk = func(f *os.File, data []byte) (int, error) {
				n, err := f.Write(data[:2])
				if err != nil {
					return n, err
				}
				return n, writeErr
			}
			defer func() { writePartialChunk = oldWrite }()
			info := file(1, "a.txt", 6)
			err := d.transfer(context.Background(), info, "a.txt", filepath.Join(d.cfg.LocalDownDir, "a.txt"))
			wantErr := writeErr
			if wantErr == nil {
				wantErr = io.ErrShortWrite
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("error = %v, want %v", err, wantErr)
			}
			if !strings.Contains(output.String(), "event=transfer_finished outcome=failed written_bytes_this_attempt=2 ") {
				t.Fatalf("wrong write count: %s", output.String())
			}
		})
	}
}
