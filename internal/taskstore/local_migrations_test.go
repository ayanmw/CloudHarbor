package taskstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalDirectoryMigrationEvidencePersistsAndIsAppendOnly(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	migration := LocalDirectoryMigration{ID: "0123456789abcdef0123456789abcdef", SourcePath: filepath.Join(t.TempDir(), "old"), TargetPath: filepath.Join(t.TempDir(), "new"), DesiredQueuePath: "/next", TombstonePath: filepath.Join(t.TempDir(), "old.migrated"), Status: "running", Phase: "copying", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	migration.Phase = "verifying"
	migration.TotalFiles, migration.TotalBytes = 2, 10
	migration.ProcessedFiles, migration.ProcessedBytes = 2, 10
	migration.UpdatedAtUTC = now.Add(time.Second)
	if err = store.UpdateLocalDirectoryMigration(migration, true); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadActiveLocalDirectoryMigration()
	if err != nil || loaded.Phase != "verifying" || loaded.TotalBytes != 10 || loaded.DesiredQueuePath != "/next" {
		t.Fatalf("active migration did not persist: %#v, %v", loaded, err)
	}
	if _, err = store.db.Exec(`UPDATE local_directory_migration_events SET phase='tampered' WHERE migration_id=?`, migration.ID); err == nil {
		t.Fatal("migration event history allowed update")
	}
	var events int
	if err = store.db.QueryRow(`SELECT count(*) FROM local_directory_migration_events WHERE migration_id=?`, migration.ID).Scan(&events); err != nil || events != 2 {
		t.Fatalf("migration event history: %d, %v", events, err)
	}
	migration.Status, migration.Phase = "completed", "complete"
	migration.FinishedAtUTC = now.Add(2 * time.Second)
	migration.UpdatedAtUTC = migration.FinishedAtUTC
	if err = store.UpdateLocalDirectoryMigration(migration, true); err != nil {
		t.Fatal(err)
	}
	if _, err = store.LoadActiveLocalDirectoryMigration(); err != sql.ErrNoRows {
		t.Fatalf("completed migration stayed active: %v", err)
	}
	loaded, err = store.LoadLatestLocalDirectoryMigration()
	if err != nil || loaded.Status != "completed" || !loaded.FinishedAtUTC.Equal(migration.FinishedAtUTC) {
		t.Fatalf("completed migration evidence missing: %#v, %v", loaded, err)
	}
}

func TestExportLatestLocalDirectoryMigrationEvidenceIsOrderedAndBounded(t *testing.T) {
	queueDir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(queueDir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	defer func() { _ = store.Close() }()
	if _, err = store.ExportLatestLocalDirectoryMigrationEvidence(context.Background()); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty history err=%v", err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	migration := LocalDirectoryMigration{ID: "0123456789abcdef0123456789abcdef", SourcePath: filepath.Join(t.TempDir(), "old"), TargetPath: filepath.Join(t.TempDir(), "new"), TombstonePath: filepath.Join(t.TempDir(), "old.migrated"), Status: "running", Phase: "copying", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.CreateLocalDirectoryMigration(migration); err != nil {
		t.Fatal(err)
	}
	migration.Phase, migration.UpdatedAtUTC = "verifying", now.Add(time.Second)
	if err = store.UpdateLocalDirectoryMigration(migration, true); err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(queueDir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := store.ExportLatestLocalDirectoryMigrationEvidence(context.Background())
	if err != nil || bundle.Migration.ID != migration.ID || len(bundle.Events) != 2 || bundle.Events[0].Sequence >= bundle.Events[1].Sequence || bundle.Events[0].Phase != "copying" || bundle.Events[1].Phase != "verifying" || !bundle.Events[1].RecordedAtUTC.Equal(migration.UpdatedAtUTC) || !json.Valid(bundle.Events[1].Evidence) {
		t.Fatalf("evidence bundle=%+v err=%v", bundle, err)
	}
	for i := 0; i <= LocalDirectoryMigrationEvidenceEventLimit; i++ {
		if _, err = store.db.Exec(`INSERT INTO local_directory_migration_events(migration_id,status,phase,recorded_at_utc,evidence_json) VALUES(?,?,?,?,?)`, migration.ID, "running", "copying", now.Add(time.Duration(i+2)*time.Second).UnixMicro(), []byte(`{"ok":true}`)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = store.ExportLatestLocalDirectoryMigrationEvidence(context.Background()); !errors.Is(err, ErrLocalMigrationEvidenceTooLarge) {
		t.Fatalf("oversize history err=%v", err)
	}
}

func TestExportLatestLocalDirectoryMigrationEvidenceRejectsClosedStore(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ExportLatestLocalDirectoryMigrationEvidence(context.Background()); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed store export err=%v", err)
	}
}

func TestLocalDirectoryMigrationHistoryUsesStableDescendingPaginationAndSelectedEvidence(t *testing.T) {
	queueDir := filepath.Join(t.TempDir(), "queue")
	store, err := Open(queueDir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	ids := []string{"00000000000000000000000000000001", "00000000000000000000000000000002", "00000000000000000000000000000003"}
	for _, id := range ids {
		migration := LocalDirectoryMigration{ID: id, SourcePath: filepath.Join(t.TempDir(), id+"-old"), TargetPath: filepath.Join(t.TempDir(), id+"-new"), TombstonePath: filepath.Join(t.TempDir(), id+"-tomb"), Status: "running", Phase: "copying", StartedAtUTC: now, UpdatedAtUTC: now}
		if err = store.CreateLocalDirectoryMigration(migration); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = Open(queueDir, "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	page, total, err := store.ListLocalDirectoryMigrations(context.Background(), 0, 2)
	if err != nil || total != 3 || len(page) != 2 || page[0].ID != ids[2] || page[1].ID != ids[1] {
		t.Fatalf("page=%+v total=%d err=%v", page, total, err)
	}
	page, total, err = store.ListLocalDirectoryMigrations(context.Background(), 2, 2)
	if err != nil || total != 3 || len(page) != 1 || page[0].ID != ids[0] {
		t.Fatalf("last page=%+v total=%d err=%v", page, total, err)
	}
	newer := LocalDirectoryMigration{ID: "00000000000000000000000000000004", SourcePath: filepath.Join(t.TempDir(), "newer-old"), TargetPath: filepath.Join(t.TempDir(), "newer-new"), TombstonePath: filepath.Join(t.TempDir(), "newer-tomb"), Status: "running", Phase: "copying", StartedAtUTC: now.Add(time.Second), UpdatedAtUTC: now.Add(time.Second)}
	if err = store.CreateLocalDirectoryMigration(newer); err != nil {
		t.Fatal(err)
	}
	bundle, err := store.ExportLocalDirectoryMigrationEvidence(context.Background(), ids[0])
	if err != nil || bundle.Migration.ID != ids[0] || len(bundle.Events) != 1 {
		t.Fatalf("selected evidence=%+v err=%v", bundle, err)
	}
	latest, err := store.LoadLatestLocalDirectoryMigration()
	if err != nil || latest.ID != newer.ID {
		t.Fatalf("latest migration=%+v err=%v", latest, err)
	}
	if _, err = store.ExportLocalDirectoryMigrationEvidence(context.Background(), "ffffffffffffffffffffffffffffffff"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown ID err=%v", err)
	}
}

func TestLocalDirectoryMigrationHistoryRejectsInvalidPage(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	items, total, err := store.ListLocalDirectoryMigrations(context.Background(), 0, 20)
	if err != nil || total != 0 || items == nil || len(items) != 0 {
		t.Fatalf("empty history items=%v total=%d err=%v", items, total, err)
	}
	for _, tc := range []struct{ offset, limit int }{{-1, 10}, {0, 0}, {0, LocalDirectoryMigrationPageLimit + 1}} {
		if _, _, err = store.ListLocalDirectoryMigrations(context.Background(), tc.offset, tc.limit); err == nil {
			t.Fatalf("accepted offset=%d limit=%d", tc.offset, tc.limit)
		}
	}
}

func TestFilteredLocalDirectoryMigrationHistoryMatchesLiteralPathsAndStatus(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := t.TempDir()
	now := time.Now().UTC().Truncate(time.Microsecond)
	fixtures := []struct {
		id, source, target, status string
	}{
		{"00000000000000000000000000000001", filepath.Join(root, "percent%needle"), filepath.Join(root, "target-a"), "failed"},
		{"00000000000000000000000000000002", filepath.Join(root, "xneedle"), filepath.Join(root, "target-b"), "completed"},
		{"00000000000000000000000000000003", filepath.Join(root, "source-c"), filepath.Join(root, "literal_needle"), "completed"},
		{"00000000000000000000000000000004", filepath.Join(root, "slash", "needle"), filepath.Join(root, "target-d"), "running"},
	}
	for i, fixture := range fixtures {
		migration := LocalDirectoryMigration{ID: fixture.id, SourcePath: fixture.source, TargetPath: fixture.target, TombstonePath: fixture.source + ".moved", Status: "running", Phase: "copying", StartedAtUTC: now.Add(time.Duration(i) * time.Second), UpdatedAtUTC: now.Add(time.Duration(i) * time.Second)}
		if err = store.CreateLocalDirectoryMigration(migration); err != nil {
			t.Fatal(err)
		}
		if fixture.status != "running" {
			migration.Status, migration.Phase = fixture.status, "complete"
			migration.UpdatedAtUTC = now.Add(time.Duration(i+1) * time.Second)
			if fixture.status == "completed" {
				migration.FinishedAtUTC = migration.UpdatedAtUTC
			}
			if err = store.UpdateLocalDirectoryMigration(migration, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, tc := range []struct {
		query  string
		status string
		want   string
	}{
		{"%needle", "", fixtures[0].id},
		{"_needle", "", fixtures[2].id},
		// Linux CI exposed a Windows-only query against filepath.Join fixtures.
		{string(filepath.Separator) + "needle", "", fixtures[3].id},
		{"xneedle", "completed", fixtures[1].id},
	} {
		items, total, listErr := store.ListFilteredLocalDirectoryMigrations(context.Background(), LocalDirectoryMigrationFilter{Status: tc.status, Query: tc.query}, 0, 10)
		if listErr != nil || total != 1 || len(items) != 1 || items[0].ID != tc.want {
			t.Fatalf("filter=%+v items=%+v total=%d err=%v", tc, items, total, listErr)
		}
	}
	if filepath.Separator != '\\' {
		// A backslash remains literal on Unix; it must not match the slash fixture.
		items, total, err := store.ListFilteredLocalDirectoryMigrations(context.Background(), LocalDirectoryMigrationFilter{Query: `\needle`}, 0, 10)
		if err != nil || total != 0 || len(items) != 0 {
			t.Fatalf("literal backslash items=%+v total=%d err=%v", items, total, err)
		}
	}
	items, total, err := store.ListFilteredLocalDirectoryMigrations(context.Background(), LocalDirectoryMigrationFilter{Status: "completed", Query: "needle"}, 0, 1)
	if err != nil || total != 2 || len(items) != 1 || items[0].ID != fixtures[2].id {
		t.Fatalf("combined stable page=%+v total=%d err=%v", items, total, err)
	}
	if _, _, err = store.ListFilteredLocalDirectoryMigrations(context.Background(), LocalDirectoryMigrationFilter{Status: "unknown"}, 0, 10); err == nil {
		t.Fatal("invalid migration status was accepted")
	}
	if _, _, err = store.ListFilteredLocalDirectoryMigrations(context.Background(), LocalDirectoryMigrationFilter{Query: strings.Repeat("x", 257)}, 0, 10); err == nil {
		t.Fatal("oversized migration path query was accepted")
	}
}

func TestLocalDirectoryMigrationHistoryFiltersInclusiveUTCCalendarDates(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root := t.TempDir()
	fixtures := []struct {
		id string
		at time.Time
	}{
		{"00000000000000000000000000000011", time.Date(2024, 2, 28, 23, 59, 59, 999999000, time.UTC)},
		{"00000000000000000000000000000012", time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC)},
		{"00000000000000000000000000000013", time.Date(2024, 2, 29, 23, 59, 59, 999999000, time.UTC)},
		{"00000000000000000000000000000014", time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)},
	}
	for i, fixture := range fixtures {
		path := filepath.Join(root, fmt.Sprintf("migration-%d", i))
		m := LocalDirectoryMigration{ID: fixture.id, SourcePath: path, TargetPath: path + "-target", TombstonePath: path + ".moved", Status: "running", Phase: "copying", StartedAtUTC: fixture.at, UpdatedAtUTC: fixture.at}
		if err = store.CreateLocalDirectoryMigration(m); err != nil {
			t.Fatal(err)
		}
	}
	filter := LocalDirectoryMigrationFilter{StartedFromUTC: "2024-02-29", StartedThroughUTC: "2024-02-29"}
	items, total, err := store.ListFilteredLocalDirectoryMigrations(context.Background(), filter, 0, 1)
	if err != nil || total != 2 || len(items) != 1 || items[0].ID != fixtures[2].id {
		t.Fatalf("inclusive UTC date page=%+v total=%d err=%v", items, total, err)
	}
	page2, total, err := store.ListFilteredLocalDirectoryMigrations(context.Background(), filter, 1, 1)
	if err != nil || total != 2 || len(page2) != 1 || page2[0].ID != fixtures[1].id {
		t.Fatalf("stable UTC date page=%+v total=%d err=%v", page2, total, err)
	}
	exported, err := store.ExportFilteredLocalDirectoryMigrationSummaries(context.Background(), filter)
	if err != nil || len(exported) != 2 || exported[0].ID != fixtures[2].id || exported[1].ID != fixtures[1].id {
		t.Fatalf("UTC date export=%+v err=%v", exported, err)
	}
	fromOnly, fromTotal, err := store.ListFilteredLocalDirectoryMigrations(context.Background(), LocalDirectoryMigrationFilter{StartedFromUTC: "2024-03-01"}, 0, 10)
	if err != nil || fromTotal != 1 || len(fromOnly) != 1 || fromOnly[0].ID != fixtures[3].id {
		t.Fatalf("UTC start-only filter=%+v total=%d err=%v", fromOnly, fromTotal, err)
	}
	throughOnly, throughTotal, err := store.ListFilteredLocalDirectoryMigrations(context.Background(), LocalDirectoryMigrationFilter{StartedThroughUTC: "2024-02-28"}, 0, 10)
	if err != nil || throughTotal != 1 || len(throughOnly) != 1 || throughOnly[0].ID != fixtures[0].id {
		t.Fatalf("UTC end-only filter=%+v total=%d err=%v", throughOnly, throughTotal, err)
	}
	combined, combinedTotal, err := store.ListFilteredLocalDirectoryMigrations(context.Background(), LocalDirectoryMigrationFilter{Status: "running", Query: "migration-1", StartedFromUTC: "2024-02-29", StartedThroughUTC: "2024-02-29"}, 0, 10)
	if err != nil || combinedTotal != 1 || len(combined) != 1 || combined[0].ID != fixtures[1].id {
		t.Fatalf("combined UTC date/status/path filter=%+v total=%d err=%v", combined, combinedTotal, err)
	}
	for _, invalid := range []LocalDirectoryMigrationFilter{{StartedFromUTC: "2024-02-30"}, {StartedThroughUTC: "2024-2-9"}, {StartedFromUTC: "2024-03-01", StartedThroughUTC: "2024-02-29"}} {
		if _, _, err = store.ListFilteredLocalDirectoryMigrations(context.Background(), invalid, 0, 10); err == nil {
			t.Fatalf("invalid UTC date filter was accepted: %+v", invalid)
		}
	}
}

func TestExportFilteredLocalDirectoryMigrationSummariesIsBoundedAndOrdered(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	items, err := store.ExportFilteredLocalDirectoryMigrationSummaries(context.Background(), LocalDirectoryMigrationFilter{})
	if err != nil || items == nil || len(items) != 0 {
		t.Fatalf("empty export items=%v err=%v", items, err)
	}
	root, now := t.TempDir(), time.Now().UTC().Truncate(time.Microsecond).UnixMicro()
	tx, err := store.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < LocalDirectoryMigrationSummaryExportLimit; i++ {
		id := fmt.Sprintf("%032x", i+1)
		path := filepath.Join(root, "match", id)
		if _, err = tx.Exec(`INSERT INTO local_directory_migrations(migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, path, filepath.Join(root, "target", id), "", path+".moved", "running", "copying", 1, 2, 0, 0, now+int64(i), now+int64(i), nil, ""); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	items, err = store.ExportFilteredLocalDirectoryMigrationSummaries(context.Background(), LocalDirectoryMigrationFilter{Query: "match"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != LocalDirectoryMigrationSummaryExportLimit {
		t.Fatalf("export count=%d", len(items))
	}
	if items[0].ID != fmt.Sprintf("%032x", LocalDirectoryMigrationSummaryExportLimit) || items[len(items)-1].ID != fmt.Sprintf("%032x", 1) {
		t.Fatalf("export sort first=%v last=%v", items[0].ID, items[len(items)-1].ID)
	}
	if _, err = store.db.Exec(`INSERT INTO local_directory_migrations(migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, fmt.Sprintf("%032x", LocalDirectoryMigrationSummaryExportLimit+1), filepath.Join(root, "match", "extra"), filepath.Join(root, "target", "extra"), "", filepath.Join(root, "match", "extra.moved"), "running", "copying", 1, 2, 0, 0, now+int64(LocalDirectoryMigrationSummaryExportLimit+1), now+int64(LocalDirectoryMigrationSummaryExportLimit+1), nil, ""); err != nil {
		t.Fatal(err)
	}
	if _, err = store.ExportFilteredLocalDirectoryMigrationSummaries(context.Background(), LocalDirectoryMigrationFilter{Query: "match"}); !errors.Is(err, ErrLocalMigrationSummaryExportTooLarge) {
		t.Fatalf("oversized export err=%v", err)
	}
}

func TestLocalDirectoryMigrationEventPageScopesIDAndUsesSequenceCursor(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "queue"), "/queue")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Microsecond)
	first := LocalDirectoryMigration{ID: "000000000000000000000000000000a1", SourcePath: filepath.Join(t.TempDir(), "old-a"), TargetPath: filepath.Join(t.TempDir(), "new-a"), TombstonePath: filepath.Join(t.TempDir(), "old-a.moved"), Status: "running", Phase: "copying", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.CreateLocalDirectoryMigration(first); err != nil {
		t.Fatal(err)
	}
	for i, phase := range []string{"verifying", "retiring_source"} {
		first.Phase, first.UpdatedAtUTC = phase, now.Add(time.Duration(i+1)*time.Second)
		if err = store.UpdateLocalDirectoryMigration(first, true); err != nil {
			t.Fatal(err)
		}
	}
	second := LocalDirectoryMigration{ID: "000000000000000000000000000000a2", SourcePath: filepath.Join(t.TempDir(), "old-b"), TargetPath: filepath.Join(t.TempDir(), "new-b"), TombstonePath: filepath.Join(t.TempDir(), "old-b.moved"), Status: "running", Phase: "copying", StartedAtUTC: now, UpdatedAtUTC: now}
	if err = store.CreateLocalDirectoryMigration(second); err != nil {
		t.Fatal(err)
	}
	eventlessID := "000000000000000000000000000000a3"
	eventlessPath := filepath.Join(t.TempDir(), "old-eventless")
	if _, err = store.db.Exec(`INSERT INTO local_directory_migrations(migration_id,source_path,target_path,desired_queue_path,tombstone_path,status,phase,total_files,total_bytes,processed_files,processed_bytes,started_at_utc,updated_at_utc,finished_at_utc,last_error) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, eventlessID, eventlessPath, eventlessPath+"-new", "", eventlessPath+".moved", "running", "copying", 0, 0, 0, 0, now.UnixMicro(), now.UnixMicro(), nil, ""); err != nil {
		t.Fatal(err)
	}
	page, next, more, err := store.LocalDirectoryMigrationEventPage(context.Background(), first.ID, 0, 2)
	if err != nil || len(page) != 2 || page[0].Phase != "retiring_source" || page[1].Phase != "verifying" || !more || next != page[1].Sequence || !page[0].RecordedAtUTC.Equal(first.UpdatedAtUTC) {
		t.Fatalf("latest events=%+v next=%d more=%v err=%v", page, next, more, err)
	}
	older, next, more, err := store.LocalDirectoryMigrationEventPage(context.Background(), first.ID, next, 2)
	if err != nil || len(older) != 1 || older[0].Phase != "copying" || more || next != 0 || older[0].Sequence >= page[1].Sequence {
		t.Fatalf("older events=%+v next=%d more=%v err=%v", older, next, more, err)
	}
	other, _, _, err := store.LocalDirectoryMigrationEventPage(context.Background(), second.ID, 0, 2)
	if err != nil || len(other) != 1 || other[0].Phase != "copying" || other[0].Sequence == older[0].Sequence {
		t.Fatalf("other migration events=%+v err=%v", other, err)
	}
	empty, next, more, err := store.LocalDirectoryMigrationEventPage(context.Background(), eventlessID, 0, 2)
	if err != nil || len(empty) != 0 || next != 0 || more {
		t.Fatalf("eventless migration page=%+v next=%d more=%v err=%v", empty, next, more, err)
	}
	if _, _, _, err = store.LocalDirectoryMigrationEventPage(context.Background(), "ffffffffffffffffffffffffffffffff", 0, 2); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("unknown migration ID err=%v", err)
	}
	for _, tc := range []struct {
		before int64
		limit  int
	}{{-1, 2}, {0, 0}, {0, LocalDirectoryMigrationEventPageLimit + 1}} {
		if _, _, _, err = store.LocalDirectoryMigrationEventPage(context.Background(), first.ID, tc.before, tc.limit); err == nil {
			t.Fatalf("accepted event page before=%d limit=%d", tc.before, tc.limit)
		}
	}
}
