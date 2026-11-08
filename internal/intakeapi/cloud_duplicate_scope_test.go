package intakeapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

type scopedDuplicateBackend struct {
	fakeBackend
	directory string
}

func (f *scopedDuplicateBackend) CloudDuplicatePreviewInDirectory(_ context.Context, directory string, offset, limit int) ([]taskstore.CloudDuplicateGroup, int, int, error) {
	f.directory = directory
	return []taskstore.CloudDuplicateGroup{}, 0, 0, nil
}
func TestDuplicatePreviewDirectoryIsBoundAndValidated(t *testing.T) {
	f := &scopedDuplicateBackend{}
	s := New(f, nil)
	w := httptest.NewRecorder()
	s.HandleDuplicatePreview(w, httptest.NewRequest("GET", "/api/cloud-cleanup/duplicates?identity_format=string&directory=%2Fa", nil))
	if w.Code != 200 || f.directory != "/a" || !strings.Contains(w.Body.String(), `"directory":"/a"`) {
		t.Fatalf("scope %s %d %s", f.directory, w.Code, w.Body.String())
	}
	for _, q := range []string{"directory=relative", "directory=%2Fa%2F..%2Fb", "directory=%2Fa&directory=%2Fb", "directory="} {
		w = httptest.NewRecorder()
		s.HandleDuplicatePreview(w, httptest.NewRequest("GET", "/api/cloud-cleanup/duplicates?"+q, nil))
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d", q, w.Code)
		}
	}
}
