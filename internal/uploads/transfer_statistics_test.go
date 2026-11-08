package uploads

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
	"web_browser_baidu_onekey_upload/internal/baiduapi"
)

func TestTransferObservationPersistsAcrossRestartWithoutChangingAcknowledgedBytes(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if m != nil {
			_ = m.Close()
		}
	})
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	sample := baiduapi.UploadTransferSample{PartIndex: 0, PayloadBytesRead: 2, ActiveNanos: 12345, StartedAtUTC: time.Now().UTC(), FinishedAtUTC: time.Now().UTC()}
	attempt := strings.Repeat("a", 32)
	m.beginUploadAttempt(b.Items[0].ID, attempt)
	if err = m.recordTransferSample(b.Items[0].ID, attempt, sample); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	var body string
	var sent int64
	if err = m.db.QueryRow(`SELECT sent,error FROM events WHERE item=? AND error LIKE 'upload_transfer_sample_v1:%'`, b.Items[0].ID).Scan(&sent, &body); err != nil {
		t.Fatal(err)
	}
	var evidence uploadTransferEvidence
	if err = json.Unmarshal([]byte(strings.TrimPrefix(body, "upload_transfer_sample_v1:")), &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.AttemptID != attempt || evidence.Sample.PayloadBytesRead != 2 || evidence.Sample.ActiveNanos != 12345 || sent != 2 {
		t.Fatalf("observation changed: %+v", evidence)
	}
	read, err := m.Get(b.ID)
	if err != nil || read.Items[0].Sent != "0" {
		t.Fatal("request read incorrectly advanced acknowledged bytes")
	}
}

func TestTransferObservationRejectsInvalidSamplesAndReturnsStorageFailure(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	b, err := m.Create("/target", []Input{{Path: "a", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	attempt := strings.Repeat("a", 32)
	m.beginUploadAttempt(b.Items[0].ID, attempt)
	sample := baiduapi.UploadTransferSample{PartIndex: 0, PayloadBytesRead: 2, ActiveNanos: 0, StartedAtUTC: time.Now().UTC(), FinishedAtUTC: time.Now().UTC()}
	invalid := sample
	invalid.PayloadBytesRead = -1
	if err = m.recordTransferSample(b.Items[0].ID, attempt, invalid); err == nil {
		t.Fatal("accepted negative observation")
	}
	// Zero-duration observations are valid timer-resolution evidence; aggregators
	// must not invent elapsed time or divide by zero to calculate a speed.
	if err = m.recordTransferSample(b.Items[0].ID, attempt, sample); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	if err = m.recordTransferSample(b.Items[0].ID, attempt, sample); err == nil {
		t.Fatal("sample storage failure was hidden")
	}
}
