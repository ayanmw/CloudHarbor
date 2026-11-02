package baiduapi

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestConcurrentDownloadTracePreservesIdentityWithoutURLsOrCredentials(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") }))
	defer server.Close()
	base := NewClient("private-access-token", "private-app-key")
	paths := []string{"/queue/课程/a.txt", "/queue/课程/b\n.txt"}
	var wg sync.WaitGroup
	for i := range paths {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			client := base.WithDownloadTrace(strconv.Itoa(i+1), "batch-1", int64(i+1), paths[i]).WithContext(context.Background())
			body, _, err := client.DownloadFileWithRange(server.URL+"?token=temporary-secret", 0)
			if err != nil {
				t.Error(err)
				return
			}
			io.Copy(io.Discard, body)
			body.Close()
		}(i)
	}
	wg.Wait()
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected two request and response pairs: %s", output.String())
	}
	for _, line := range lines {
		matched := false
		for i, p := range paths {
			if strings.Contains(line, `transfer_id="`+strconv.Itoa(i+1)+`"`) {
				matched = true
				if !strings.Contains(line, "path="+strconv.Quote(p)) || !strings.Contains(line, `batch_id="batch-1"`) {
					t.Errorf("mixed trace fields: %s", line)
				}
			}
		}
		if !matched {
			t.Errorf("missing transfer id: %s", line)
		}
	}
	for _, secret := range []string{server.URL, "temporary-secret", "private-access-token", "private-app-key"} {
		if strings.Contains(output.String(), secret) {
			t.Fatal("download log exposed URL or credential")
		}
	}
	if base.downloadTrace.transferID != "" {
		t.Fatal("trace mutated shared client")
	}
}
