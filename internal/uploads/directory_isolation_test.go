package uploads

import (
	"reflect"
	"testing"
)

func TestDirectoryIsolationRestoresUnknownPathsBeforeAdmission(t *testing.T) {
	dir := t.TempDir()
	m, err := Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.createManifest("/target", nil, []string{"root", "root/empty", "done"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = m.db.Exec(`UPDATE upload_directories SET state=CASE relative_path WHEN 'root' THEN 'unknown' WHEN 'root/empty' THEN 'sending' ELSE 'confirmed' END WHERE batch=?`, b.ID); err != nil {
		t.Fatal(err)
	}
	if err = m.Close(); err != nil {
		t.Fatal(err)
	}
	m, err = Open(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	var restored []string
	if err = m.RestoreDirectoryIsolation(func(remote string) error { restored = append(restored, remote); return nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored, []string{"/target/root", "/target/root/empty"}) {
		t.Fatalf("restored=%v", restored)
	}
	got, err := m.Get(b.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range got.Directories {
		if d.Path == "done" && d.State != "confirmed" {
			t.Fatal("confirmed evidence changed")
		}
	}
}
