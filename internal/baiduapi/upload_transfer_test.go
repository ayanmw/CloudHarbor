package baiduapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadPayloadReaderExcludesMultipartAndHandlesPartialReads(t *testing.T) {
	reader := newUploadPayloadReader(bytes.NewReader([]byte("HEADhelloTAIL")), 4, 5)
	buffer := make([]byte, 6)
	if _, err := io.ReadFull(reader, buffer); err != nil {
		t.Fatal(err)
	}
	if reader.payloadRead.Load() != 2 {
		t.Fatal("multipart header counted as payload")
	}
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
	if reader.payloadRead.Load() != 5 {
		t.Fatalf("payload count %d", reader.payloadRead.Load())
	}
}

func TestMeasuredUploadObservesPayloadAfterPacerAndStopsOnPersistenceFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			original := http.DefaultTransport
			defer func() { http.DefaultTransport = original }()
			local := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
				t.Fatal(err)
			}
			pacer := &uploadObservationPacer{}
			calls := []string{}
			samples := 0
			saveErr := errors.New("sample persistence failed")
			http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
				method := r.URL.Query().Get("method")
				calls = append(calls, method)
				body := `{"errno":0,"uploadid":"fixture","return_type":1,"block_list":[0]}`
				if method == "upload" {
					data, err := io.ReadAll(r.Body)
					if err != nil || r.ContentLength != int64(len(data)) || !bytes.Contains(data, []byte("hello")) {
						t.Fatalf("request framing changed: %d %d %v", r.ContentLength, len(data), err)
					}
					body = `{"errno":0,"md5":"5d41402abc4b2a76b9719d911017c592"}`
				} else if method == "create" {
					body = `{"errno":0,"fs_id":123,"path":"/test","size":5}`
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})
			_, err := NewClient("fixture", "").WithRequestPacer(pacer).UploadLocalFileMeasured(context.Background(), local, "/test", func(string, int64) error { return nil }, func(sample UploadTransferSample) error {
				samples++
				if sample.PlannedPayloadBytes == nil || *sample.PlannedPayloadBytes != 5 || sample.PlannedPartCount != 1 || sample.PartPayloadBytes != 5 || !sample.PartConfirmed || sample.PayloadBytesRead != 5 || sample.PartIndex != 0 || sample.ActiveNanos < 0 || sample.StartedAtUTC.Before(pacer.last) || sample.FinishedAtUTC.Before(sample.StartedAtUTC) {
					t.Fatalf("invalid sample: %+v", sample)
				}
				if fail {
					return saveErr
				}
				return nil
			})
			if samples != 1 {
				t.Fatalf("samples=%d", samples)
			}
			if fail {
				if !errors.Is(err, saveErr) || len(calls) != 2 {
					t.Fatalf("merge continued after evidence failure: %v %v", calls, err)
				}
			} else if err != nil || len(calls) != 3 {
				t.Fatalf("upload failed: %v %v", calls, err)
			}
		})
	}
}

type uploadObservationPacer struct{ last time.Time }

func (p *uploadObservationPacer) Wait(context.Context) error { p.last = time.Now().UTC(); return nil }

func TestMeasuredUploadKeepsPartialReadEvidenceOnRequestFailure(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	local := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("method") == "upload" {
			header := []byte{}
			one := make([]byte, 1)
			for !bytes.HasSuffix(header, []byte("\r\n\r\n")) {
				if _, err := io.ReadFull(r.Body, one); err != nil {
					return nil, err
				}
				header = append(header, one[0])
			}
			if _, err := io.ReadFull(r.Body, make([]byte, 2)); err != nil {
				return nil, err
			}
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0,"uploadid":"fixture","return_type":1,"block_list":[0]}`)), Request: r}, nil
	})
	samples := 0
	_, err := NewClient("fixture", "").UploadLocalFileMeasured(context.Background(), local, "/test", func(string, int64) error { return nil }, func(s UploadTransferSample) error {
		samples++
		if s.PayloadBytesRead != 2 {
			t.Fatalf("partial observation: %+v", s)
		}
		return nil
	})
	if !errors.Is(err, ErrUploadUnconfirmed) || samples != 1 {
		t.Fatalf("partial request lost: %d %v", samples, err)
	}
}

func TestMeasuredFastCreateHasNoPartSamples(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	local := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0,"return_type":2,"fs_id":123,"path":"/test","size":5}`)), Request: r}, nil
	})
	samples := 0
	_, err := NewClient("fixture", "").UploadLocalFileMeasured(context.Background(), local, "/test", func(string, int64) error { return nil }, func(UploadTransferSample) error { samples++; return nil })
	if err != nil || calls != 1 || samples != 0 {
		t.Fatalf("fast create fabricated transfer: calls=%d samples=%d err=%v", calls, samples, err)
	}
}

func TestMeasuredUploadRejectsDuplicatePlanBeforePartWrites(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	local := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(local, []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"errno":0,"uploadid":"fixture","return_type":1,"block_list":[0,0]}`)), Request: r}, nil
	})
	_, err := NewClient("fixture", "").UploadLocalFileMeasured(context.Background(), local, "/test", func(string, int64) error { return nil }, func(UploadTransferSample) error { t.Fatal("invalid plan generated sample"); return nil })
	if !errors.Is(err, ErrUploadUnconfirmed) || calls != 1 {
		t.Fatalf("invalid plan wrote parts: %d %v", calls, err)
	}
}

func TestHashCheckpointFailureStopsBeforeRemoteWrites(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	local := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(local, bytes.Repeat([]byte("x"), 2*UploadBlockSize), 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, errors.New("unexpected remote request")
	})
	pause := errors.New("fixture checkpoint pause")
	checks := 0
	_, err := NewClient("fixture", "").UploadLocalFile(context.Background(), local, "/test", func(state string, _ int64) error {
		if state == "hashing" {
			checks++
			if checks == 3 {
				return pause
			}
		}
		return nil
	})
	if !errors.Is(err, pause) || calls != 0 {
		t.Fatalf("hash pause reached remote writes: checks=%d calls=%d err=%v", checks, calls, err)
	}
}
