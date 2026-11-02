package downloader

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTransferLogsLinkBodyAndLocalPublicationWithOneFileAndRootAttempt(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	d, _ := setup(t, func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "abc") })
	root := file(99, "root", 0)
	root.IsDir = 1
	d.queue.jobs[root.FsID] = &queueJob{Info: root, AttemptID: "root-attempt-1"}
	info := file(1, "a.txt", 3)
	info.Path = root.Path + "/a.txt"
	if err := d.transfer(context.Background(), info, "root/a.txt", filepath.Join(d.cfg.LocalDownDir, "root", "a.txt")); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, event := range []string{"transfer_started", "link_request", "link_finished", "http_request", "http_response", "local_published", "transfer_finished"} {
		if !strings.Contains(text, "event="+event) {
			t.Errorf("missing %s in %s", event, text)
		}
	}
	ids := regexp.MustCompile(`transfer_id="([a-f0-9]{32})"`).FindAllStringSubmatch(text, -1)
	if len(ids) < 7 {
		t.Fatalf("missing trace ids: %s", text)
	}
	for _, id := range ids {
		if id[1] != ids[0][1] {
			t.Fatal("transfer correlation changed between stages")
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if !strings.Contains(line, `batch_id="root-attempt-1"`) || !strings.Contains(line, `path="/queue/root/a.txt"`) {
			t.Errorf("missing batch or file path: %s", line)
		}
	}
}
