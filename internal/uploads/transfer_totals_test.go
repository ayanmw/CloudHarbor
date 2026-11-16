package uploads

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

func TestTransferTotalsExcludeOfflineGapsAndZeroDurationBytes(t *testing.T) {
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
	for index, values := range [][2]int64{{2, 1000000000}, {3, 0}, {0, 1000000000}} {
		at := time.Date(2020+index, 1, 1, 0, 0, 0, 0, time.UTC)
		sample := baiduapi.UploadTransferSample{PartIndex: 0, PayloadBytesRead: values[0], ActiveNanos: values[1], StartedAtUTC: at, FinishedAtUTC: at.Add(time.Duration(values[1]))}
		attempt := strings.Repeat(string(rune('a'+index)), 32)
		m.beginUploadAttempt(b.Items[0].ID, attempt)
		if err = m.recordTransferSample(b.Items[0].ID, attempt, sample); err != nil {
			t.Fatal(err)
		}
	}
	read, err := m.transferStatistics(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.SampleCount != "3" || read.PayloadBytesRead != "5" || read.TimedPayloadBytesRead != "2" || read.ActiveNanos != "2000000000" || read.ZeroDurationSamples != "1" || read.AverageBytesPerSecond == nil || *read.AverageBytesPerSecond != "1" {
		t.Fatalf("bad totals: %+v", read)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	after, err := m.transferStatistics(b.ID)
	if err != nil || after.ActiveNanos != read.ActiveNanos || after.PayloadBytesRead != read.PayloadBytesRead {
		t.Fatal("restart/offline gap changed totals")
	}
}

func TestTransferAccumulatorPreservesLargeIntegerTotals(t *testing.T) {
	a := &transferAccumulator{}
	a.payload.SetString("9007199254740993", 10)
	a.timedPayload.SetString("9007199254740993", 10)
	a.nanos.SetInt64(1000000000)
	a.count.SetInt64(1)
	a.add(2, 1000000000)
	totals := a.totals()
	if totals.PayloadBytesRead != "9007199254740995" || totals.AverageBytesPerSecond == nil || *totals.AverageBytesPerSecond != "4503599627370497" {
		t.Fatalf("large totals lost precision: %+v", totals)
	}
}

func TestTransferStatisticsHTTPIsScopedAndRejectsCorruptSamples(t *testing.T) {
	m, err := Open(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	first, err := m.Create("/target", []Input{{Path: "first", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := m.Create("/other", []Input{{Path: "other", Size: "3"}})
	if err != nil {
		t.Fatal(err)
	}
	sample := baiduapi.UploadTransferSample{PayloadBytesRead: 2, ActiveNanos: 1000000000, StartedAtUTC: time.Now().UTC(), FinishedAtUTC: time.Now().UTC()}
	for _, item := range []Item{first.Items[0], other.Items[0]} {
		m.beginUploadAttempt(item.ID, strings.Repeat("a", 32))
		if err = m.recordTransferSample(item.ID, strings.Repeat("a", 32), sample); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest("GET", "/statistics", nil)
	r.SetPathValue("batch", first.ID)
	w := httptest.NewRecorder()
	m.HandleTransferStatistics(w, r)
	var result BatchTransferTotals
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.SampleCount != "1" || len(result.Items) != 1 || result.Items[0].Path != "first" {
		t.Fatalf("batch isolation failed: %d %s", w.Code, w.Body.String())
	}
	post := httptest.NewRequest("POST", "/statistics", nil)
	post.SetPathValue("batch", first.ID)
	denied := httptest.NewRecorder()
	m.HandleTransferStatistics(denied, post)
	if denied.Code != 405 {
		t.Fatal(denied.Code)
	}
	if _, err = m.db.Exec(`INSERT INTO events(item,state,sent,recorded,error) VALUES(?,'uploading',2,?,?)`, first.Items[0].ID, now(), transferSamplePrefix+"bad-json"); err != nil {
		t.Fatal(err)
	}
	corrupt := httptest.NewRecorder()
	m.HandleTransferStatistics(corrupt, r)
	if corrupt.Code != 500 {
		t.Fatalf("bad sample accepted: %d", corrupt.Code)
	}
}
