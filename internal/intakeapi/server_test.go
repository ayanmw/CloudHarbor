package intakeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

type fakeBackend struct {
	inventoryLimitMode string
	refreshLimitMode   string

	commits                      int
	previews                     int
	cloudCalls                   int
	cloudOffsets                 []int
	cloudListings                map[int]downloader.CloudListing
	cloudErrorsByOffset          map[int]error
	mutationCalls                int
	searchCalls                  int
	searchQuery                  string
	searchOffset                 int
	searchLimit                  int
	incompleteDirectoryPage      taskstore.CloudInventoryIncompleteDirectoryPage
	incompleteDirectoryErr       error
	incompleteDirectoryQuery     string
	incompleteDirectoryReason    string
	incompleteDirectoryBefore    string
	incompleteDirectoryLimit     int
	incompleteDirectoryExpected  *int64
	cloudErr                     error
	cachedCloudErr               error
	cloudSizeErr                 error
	emptyDirectoriesErr          error
	duplicateStatusErr           error
	duplicatePreviewErr          error
	duplicatePreviewGroups       []taskstore.CloudDuplicateGroup
	duplicatePreviewTotal        int
	duplicatePreviewDeleteCount  int
	duplicatePreviewOffset       int
	duplicatePreviewLimit        int
	duplicateEventPageErr        error
	duplicateEvents              []taskstore.CloudDuplicateScanEvent
	duplicateEventsBefore        int64
	duplicateEventsNext          int64
	duplicateEventsMore          bool
	duplicateEventsLimit         int
	duplicateRuns                []taskstore.CloudDuplicateScanRun
	duplicateRunsNext            int64
	duplicateRunsMore            bool
	duplicateRunsErr             error
	duplicateRunsBefore          int64
	duplicateRunsLimit           int
	duplicateRunsFilter          taskstore.CloudDuplicateScanRunFilter
	scanHistoryFilter            taskstore.CloudScanRunFilter
	duplicateRunEvents           []taskstore.CloudDuplicateScanEvent
	duplicateRunEventsNext       int64
	duplicateRunEventsMore       bool
	duplicateRunEventsErr        error
	duplicateRunEventsID         string
	duplicateRunEventsBefore     int64
	duplicateRunEventsLimit      int
	duplicateRunsExport          []taskstore.CloudDuplicateScanRun
	duplicateRunsExportErr       error
	duplicateRunsExportFilter    taskstore.CloudDuplicateScanRunFilter
	duplicateRunsExportCalls     int
	duplicateEvidence            taskstore.CloudDuplicateScanEvidenceBundle
	duplicateEvidenceErr         error
	duplicateEvidenceID          string
	duplicateEvidenceCalls       int
	inventoryScanStatusErr       error
	inventoryScanBudget          int64
	inventoryRefreshStatusErr    error
	previewErr                   error
	mutationResult               downloader.CloudMutationResult
	mutationErr                  error
	duplicateEstimate            taskstore.CloudDuplicateHashCandidateEstimate
	duplicateEstimateErr         error
	refreshDirectory             string
	refreshBudget                int64
	refreshPauseCalls            int
	refreshStartErr              error
	refreshEvents                []taskstore.CloudInventoryRefreshEvent
	refreshEventsNext            int64
	refreshEventsMore            bool
	refreshEventsErr             error
	refreshEventsRunID           string
	refreshEventsKind            string
	refreshEventsReason          string
	refreshEventsBefore          int64
	refreshEventsLimit           int
	refreshChanges               []taskstore.CloudInventoryRefreshChange
	refreshChangePage            taskstore.CloudInventoryRefreshChangePage
	refreshChangesKind           string
	refreshChangesQuery          string
	refreshChangesNext           int64
	refreshChangesMore           bool
	refreshChangesErr            error
	refreshChangesRunID          string
	refreshChangesEventSequence  int64
	refreshChangesBefore         int64
	refreshChangesLimit          int
	refreshChangesExport         taskstore.CloudInventoryRefreshChangeExport
	refreshChangesExportErr      error
	refreshChangesExportRunID    string
	refreshChangesExportEvent    int64
	refreshChangesExportKind     string
	refreshChangesExportQuery    string
	refreshRuns                  []taskstore.CloudInventoryRefreshRun
	refreshRunsNext              int64
	refreshRunsMore              bool
	refreshRunsErr               error
	refreshRunsBefore            int64
	refreshRunsLimit             int
	refreshRunsFilter            taskstore.CloudInventoryRefreshRunFilter
	refreshEvidence              taskstore.CloudInventoryRefreshEvidenceBundle
	refreshEvidenceErr           error
	refreshEvidenceID            string
	refreshEvidenceCalls         int
	refreshSummaryExport         []taskstore.CloudInventoryRefreshRun
	refreshSummaryExportErr      error
	refreshSummaryExportFilter   taskstore.CloudInventoryRefreshRunFilter
	refreshSummaryExportCalls    int
	operationCalls               int
	operationSummaries           []taskstore.RemoteOperationSummary
	operationTotal               int
	operationPageErr             error
	operationFilter              taskstore.RemoteOperationFilter
	operationOffset              int
	operationLimit               int
	operationCursor              *taskstore.RemoteOperationCursor
	operationNextCursor          *taskstore.RemoteOperationCursor
	operationExportCalls         int
	operationExportFilter        taskstore.RemoteOperationFilter
	operationExports             []taskstore.RemoteOperationSummary
	operationExportErr           error
	operationEventCalls          int
	operationEventID             string
	operationEventStatus         string
	operationEventFilter         taskstore.RemoteOperationEventFilter
	operationBefore              int64
	operationEventLimit          int
	operationEvents              []taskstore.RemoteOperationEvent
	operationEventBefore         int64
	operationEventErr            error
	operationEvidenceExportCalls int
	operationEvidenceExportID    string
	operationEvidenceExport      taskstore.RemoteOperationEvidenceBundle
	operationEvidenceExportErr   error
	localMigrationEvidence       taskstore.LocalDirectoryMigrationEvidenceBundle
	localMigrationEvidenceErr    error
	localMigrationEvidenceCalls  int
	localMigrationList           []taskstore.LocalDirectoryMigration
	localMigrationListTotal      int
	localMigrationListErr        error
	localMigrationListCalls      int
	localMigrationListOffset     int
	localMigrationListLimit      int
	localMigrationFilter         taskstore.LocalDirectoryMigrationFilter
	localMigrationByID           taskstore.LocalDirectoryMigrationEvidenceBundle
	localMigrationByIDErr        error
	localMigrationByIDCalls      int
	localMigrationByIDValue      string
	localMigrationSummaries      []taskstore.LocalDirectoryMigration
	localMigrationSummariesErr   error
	localMigrationSummaryFilter  taskstore.LocalDirectoryMigrationFilter
	localMigrationSummaryCalls   int
	localMigrationEvents         []taskstore.LocalDirectoryMigrationEvidenceEvent
	localMigrationEventsNext     int64
	localMigrationEventsMore     bool
	localMigrationEventsErr      error
	localMigrationEventsCalls    int
	localMigrationEventsID       string
	localMigrationEventsBefore   int64
	localMigrationEventsLimit    int
	intakes                      []downloader.Intake
}

func (f *fakeBackend) ListCloud(_ context.Context, _ string, offset, _ int) (downloader.CloudListing, error) {
	f.cloudCalls++
	f.cloudOffsets = append(f.cloudOffsets, offset)
	if err, ok := f.cloudErrorsByOffset[offset]; ok {
		return downloader.CloudListing{}, err
	}
	if listing, ok := f.cloudListings[offset]; ok {
		return listing, f.cloudErr
	}
	return downloader.CloudListing{Directory: "/", Entries: []downloader.CloudEntry{{ID: "3400000000000000123", Path: "/a", Name: "a"}}}, f.cloudErr
}
func (f *fakeBackend) PreviewIntake(context.Context, []string) (downloader.Intake, error) {
	f.previews++
	return downloader.Intake{ID: strings.Repeat("a", 32), State: "preview"}, f.previewErr
}
func (f *fakeBackend) CommitIntake(context.Context, string) (downloader.Intake, error) {
	f.commits++
	return downloader.Intake{State: "finished", Items: []downloader.IntakeItem{{State: "unknown"}}}, nil
}
func (f *fakeBackend) RecheckIntake(context.Context, string) (downloader.Intake, error) {
	return downloader.Intake{Items: []downloader.IntakeItem{{State: "unknown"}}}, nil
}
func (f *fakeBackend) GetIntakes() []downloader.Intake {
	if f.intakes != nil {
		return append([]downloader.Intake(nil), f.intakes...)
	}
	return []downloader.Intake{{ID: strings.Repeat("a", 32), State: "finished", Items: []downloader.IntakeItem{{State: "unknown"}}}}
}
func (f *fakeBackend) ListCachedCloud(_ context.Context, _ string, _, _ int, expected *int64) ([]taskstore.CloudInventoryEntry, taskstore.CloudInventoryDirectory, int, error) {
	meta := taskstore.CloudInventoryDirectory{Status: "complete", Generation: 7}
	if expected != nil && *expected != meta.Generation {
		return nil, meta, 0, taskstore.ErrCloudInventoryGenerationChanged
	}
	return nil, meta, 0, f.cachedCloudErr
}
func (f *fakeBackend) ListIncompleteCloudInventoryDirectories(_ context.Context, before, reason, query string, limit int, expected *int64) (taskstore.CloudInventoryIncompleteDirectoryPage, error) {
	f.incompleteDirectoryBefore, f.incompleteDirectoryReason, f.incompleteDirectoryQuery, f.incompleteDirectoryLimit, f.incompleteDirectoryExpected = before, reason, query, limit, expected
	if expected != nil && *expected != f.incompleteDirectoryPage.Generation {
		return f.incompleteDirectoryPage, taskstore.ErrCloudInventoryGenerationChanged
	}
	return f.incompleteDirectoryPage, f.incompleteDirectoryErr
}
func (f *fakeBackend) SearchCachedCloud(_ context.Context, query string, offset, limit int, expected *int64) ([]taskstore.CloudInventoryEntry, int, taskstore.CloudInventorySearchCoverage, error) {
	f.searchCalls++
	f.searchQuery, f.searchOffset, f.searchLimit = query, offset, limit
	coverage := taskstore.CloudInventorySearchCoverage{Status: "incomplete", IncompleteDirectories: 2, Generation: 7}
	if expected != nil && *expected != coverage.Generation {
		return nil, 0, coverage, taskstore.ErrCloudInventoryGenerationChanged
	}
	return []taskstore.CloudInventoryEntry{{FsID: 12, Path: "/docs/report.pdf", Name: "report.pdf", Size: 40}}, 31, coverage, nil
}
func (f *fakeBackend) GetCachedCloudDirectorySize(context.Context, string) (taskstore.CloudDirectorySize, error) {
	return taskstore.CloudDirectorySize{Complete: true}, f.cloudSizeErr
}
func (f *fakeBackend) CloudInventoryScanStatus() (taskstore.CloudScanState, error) {
	reasons := &taskstore.CloudInventoryCoverageReasons{StaleDirectories: 4, MissingDirectoryStatus: 1}
	coverage := taskstore.CloudInventorySearchCoverage{Status: "incomplete", IncompleteDirectories: 2, Generation: 9, Reasons: reasons}
	return taskstore.CloudScanState{Status: "complete", BoundaryRestartCount: 1, InventoryCoverage: &coverage}, f.inventoryScanStatusErr
}

// Request budget is recorded by the fake to verify the user-visible input reaches the worker.
func (f *fakeBackend) StartCloudInventoryScan(_ context.Context, budget int64) (taskstore.CloudScanState, error) {
	f.inventoryScanBudget = budget
	return taskstore.CloudScanState{Status: "running"}, nil
}
func (f *fakeBackend) StartCloudInventoryScanWithLimit(ctx context.Context, budget int64, mode string) (taskstore.CloudScanState, error) {
	f.inventoryLimitMode = mode
	return f.StartCloudInventoryScan(ctx, budget)
}
func (f *fakeBackend) StartCloudInventoryRefreshWithLimit(ctx context.Context, directory string, budget int64, mode string) (taskstore.CloudInventoryRefreshState, error) {
	f.refreshLimitMode = mode
	return f.StartCloudInventoryRefresh(ctx, directory, budget)
}
func (f *fakeBackend) CloudScanHistoryRuns(_ context.Context, _ int64, filter taskstore.CloudScanRunFilter, _ int) ([]taskstore.CloudScanRun, int64, bool, error) {
	f.scanHistoryFilter = filter
	status := "complete"
	incomplete, generation := int64(0), int64(42)
	observed := time.Date(2026, 11, 2, 3, 4, 5, 0, time.UTC)
	reasons := &taskstore.CloudInventoryCoverageReasons{StaleDirectories: 4, MissingDirectoryStatus: 1}
	return []taskstore.CloudScanRun{{AccountFingerprint: "private-fingerprint", RunID: "run-one", Status: "complete", CoverageStatus: &status, CoverageIncompleteDirectories: &incomplete, CoverageGeneration: &generation, CoverageObservedAtUTC: &observed, CoverageReasons: reasons}}, 0, false, nil
}
func (f *fakeBackend) CloudScanHistoryEvents(context.Context, string, int64, int) ([]taskstore.CloudScanEvent, int64, bool, error) {
	return []taskstore.CloudScanEvent{{AccountFingerprint: "private-fingerprint", RunID: "run-one", Kind: "completed"}}, 0, false, nil
}
func (f *fakeBackend) CloudScanHistoryExport(context.Context, string) (taskstore.CloudScanEvidenceBundle, error) {
	status := "complete"
	incomplete, generation := int64(0), int64(42)
	observed := time.Date(2026, 11, 2, 3, 4, 5, 0, time.UTC)
	reasons := &taskstore.CloudInventoryCoverageReasons{StaleDirectories: 4, MissingDirectoryStatus: 1}
	return taskstore.CloudScanEvidenceBundle{Run: taskstore.CloudScanRun{AccountFingerprint: "private-fingerprint", RunID: "run-one", CoverageStatus: &status, CoverageIncompleteDirectories: &incomplete, CoverageGeneration: &generation, CoverageObservedAtUTC: &observed, CoverageReasons: reasons}, Events: []taskstore.CloudScanEvent{{AccountFingerprint: "private-fingerprint"}}}, nil
}
func (f *fakeBackend) QueueMonitorScanHistoryRuns(context.Context, int64, taskstore.QueueMonitorScanFilter, int) ([]taskstore.QueueMonitorScanRun, int64, bool, error) {
	return []taskstore.QueueMonitorScanRun{}, 0, false, nil
}
func (f *fakeBackend) QueueMonitorScanHistoryEvents(context.Context, string, int64, int) ([]taskstore.QueueMonitorScanEvent, int64, bool, error) {
	return []taskstore.QueueMonitorScanEvent{}, 0, false, nil
}
func (f *fakeBackend) QueueMonitorScanHistoryExport(context.Context, string) (taskstore.QueueMonitorScanBundle, error) {
	return taskstore.QueueMonitorScanBundle{Run: taskstore.QueueMonitorScanRun{AccountFingerprint: "private-fingerprint"}, Events: []taskstore.QueueMonitorScanEvent{{AccountFingerprint: "private-fingerprint"}}}, nil
}
func (f *fakeBackend) PauseCloudInventoryScan() {}
func (f *fakeBackend) CloudInventoryRefreshStatus() (taskstore.CloudInventoryRefreshState, error) {
	return taskstore.CloudInventoryRefreshState{Status: "complete", RootPath: "/target", PagesProcessed: 2, BoundaryRestartCount: 1}, f.inventoryRefreshStatusErr
}
func (f *fakeBackend) StartCloudInventoryRefresh(_ context.Context, directory string, budget int64) (taskstore.CloudInventoryRefreshState, error) {
	f.refreshDirectory = directory
	f.refreshBudget = budget
	return taskstore.CloudInventoryRefreshState{Status: "running", RootPath: directory}, f.refreshStartErr
}
func (f *fakeBackend) PauseCloudInventoryRefresh() { f.refreshPauseCalls++ }
func (f *fakeBackend) CloudInventoryRefreshEventPage(_ context.Context, runID, kind, restartReason string, before int64, limit int) ([]taskstore.CloudInventoryRefreshEvent, int64, bool, error) {
	f.refreshEventsRunID, f.refreshEventsKind, f.refreshEventsReason, f.refreshEventsBefore, f.refreshEventsLimit = runID, kind, restartReason, before, limit
	return f.refreshEvents, f.refreshEventsNext, f.refreshEventsMore, f.refreshEventsErr
}
func (f *fakeBackend) CloudInventoryRefreshChangePage(_ context.Context, runID string, eventSequence, before int64, limit int, kind, query string) (taskstore.CloudInventoryRefreshChangePage, error) {
	f.refreshChangesRunID, f.refreshChangesEventSequence, f.refreshChangesBefore, f.refreshChangesLimit, f.refreshChangesKind, f.refreshChangesQuery = runID, eventSequence, before, limit, kind, query
	return f.refreshChangePage, f.refreshChangesErr
}
func (f *fakeBackend) CloudInventoryRefreshChangesExport(_ context.Context, runID string, eventSequence int64, kind, query string) (taskstore.CloudInventoryRefreshChangeExport, error) {
	f.refreshChangesExportRunID, f.refreshChangesExportEvent, f.refreshChangesExportKind, f.refreshChangesExportQuery = runID, eventSequence, kind, query
	bundle := f.refreshChangesExport
	bundle.RunID, bundle.EventSequence, bundle.Kind, bundle.Query = runID, eventSequence, kind, query
	return bundle, f.refreshChangesExportErr
}

func (f *fakeBackend) CloudInventoryRefreshRuns(_ context.Context, before int64, limit int) ([]taskstore.CloudInventoryRefreshRun, int64, bool, error) {
	f.refreshRunsBefore, f.refreshRunsLimit = before, limit
	return f.refreshRuns, f.refreshRunsNext, f.refreshRunsMore, f.refreshRunsErr
}
func (f *fakeBackend) CloudInventoryRefreshRunsFiltered(_ context.Context, before int64, filter taskstore.CloudInventoryRefreshRunFilter, limit int) ([]taskstore.CloudInventoryRefreshRun, int64, bool, error) {
	f.refreshRunsBefore, f.refreshRunsLimit, f.refreshRunsFilter = before, limit, filter
	return f.refreshRuns, f.refreshRunsNext, f.refreshRunsMore, f.refreshRunsErr
}
func (f *fakeBackend) CloudInventoryRefreshEvidenceExport(_ context.Context, runID string) (taskstore.CloudInventoryRefreshEvidenceBundle, error) {
	f.refreshEvidenceID = runID
	f.refreshEvidenceCalls++
	return f.refreshEvidence, f.refreshEvidenceErr
}
func (f *fakeBackend) CloudInventoryRefreshRunSummariesExport(_ context.Context, filter taskstore.CloudInventoryRefreshRunFilter) ([]taskstore.CloudInventoryRefreshRun, error) {
	f.refreshSummaryExportCalls++
	f.refreshSummaryExportFilter = filter
	return f.refreshSummaryExport, f.refreshSummaryExportErr
}
func (f *fakeBackend) CloudDuplicateScanStatus() (taskstore.CloudDuplicateScanState, error) {
	return taskstore.CloudDuplicateScanState{Status: "paused"}, f.duplicateStatusErr
}
func (f *fakeBackend) CloudDuplicateScanEstimate() (taskstore.CloudDuplicateHashCandidateEstimate, error) {
	return f.duplicateEstimate, f.duplicateEstimateErr
}
func (f *fakeBackend) CloudDuplicateScanEventPage(_ context.Context, before int64, limit int) ([]taskstore.CloudDuplicateScanEvent, int64, bool, error) {
	f.duplicateEventsBefore, f.duplicateEventsLimit = before, limit
	return f.duplicateEvents, f.duplicateEventsNext, f.duplicateEventsMore, f.duplicateEventPageErr
}
func (f *fakeBackend) CloudDuplicateScanRuns(_ context.Context, before int64, filter taskstore.CloudDuplicateScanRunFilter, limit int) ([]taskstore.CloudDuplicateScanRun, int64, bool, error) {
	f.duplicateRunsBefore, f.duplicateRunsFilter, f.duplicateRunsLimit = before, filter, limit
	return f.duplicateRuns, f.duplicateRunsNext, f.duplicateRunsMore, f.duplicateRunsErr
}
func (f *fakeBackend) CloudDuplicateScanRunsExport(_ context.Context, filter taskstore.CloudDuplicateScanRunFilter) ([]taskstore.CloudDuplicateScanRun, error) {
	f.duplicateRunsExportCalls++
	f.duplicateRunsExportFilter = filter
	return f.duplicateRunsExport, f.duplicateRunsExportErr
}
func (f *fakeBackend) CloudDuplicateScanRunEventPage(_ context.Context, runID string, before int64, limit int) ([]taskstore.CloudDuplicateScanEvent, int64, bool, error) {
	f.duplicateRunEventsID, f.duplicateRunEventsBefore, f.duplicateRunEventsLimit = runID, before, limit
	return f.duplicateRunEvents, f.duplicateRunEventsNext, f.duplicateRunEventsMore, f.duplicateRunEventsErr
}
func (f *fakeBackend) CloudDuplicateScanEvidenceExport(_ context.Context, runID string) (taskstore.CloudDuplicateScanEvidenceBundle, error) {
	f.duplicateEvidenceCalls++
	f.duplicateEvidenceID = runID
	return f.duplicateEvidence, f.duplicateEvidenceErr
}
func (f *fakeBackend) StartCloudDuplicateScan(context.Context) (taskstore.CloudDuplicateScanState, error) {
	return taskstore.CloudDuplicateScanState{Status: "running"}, nil
}
func (f *fakeBackend) PauseCloudDuplicateScan() {}
func (f *fakeBackend) CloudDuplicatePreview(_ context.Context, offset, limit int) ([]taskstore.CloudDuplicateGroup, int, int, error) {
	f.duplicatePreviewOffset, f.duplicatePreviewLimit = offset, limit
	if f.duplicatePreviewGroups == nil {
		f.duplicatePreviewGroups = []taskstore.CloudDuplicateGroup{}
	}
	return f.duplicatePreviewGroups, f.duplicatePreviewTotal, f.duplicatePreviewDeleteCount, f.duplicatePreviewErr
}
func (f *fakeBackend) PreviewEmptyCloudDirectories(context.Context) ([]taskstore.CloudInventoryEntry, error) {
	return []taskstore.CloudInventoryEntry{{FsID: 9, Path: "/empty", Name: "empty", Directory: true}}, f.emptyDirectoriesErr
}
func (f *fakeBackend) MutateCloud(context.Context, downloader.CloudMutationRequest) (downloader.CloudMutationResult, error) {
	f.mutationCalls++
	if f.mutationResult.OperationID != "" || f.mutationErr != nil {
		return f.mutationResult, f.mutationErr
	}
	return downloader.CloudMutationResult{OperationID: strings.Repeat("b", 32), Status: "confirmed"}, nil
}

func (f *fakeBackend) CloudOperationPage(_ context.Context, filter taskstore.RemoteOperationFilter, offset, limit int) ([]taskstore.RemoteOperationSummary, int, error) {
	f.operationCalls++
	f.operationFilter, f.operationOffset, f.operationLimit = filter, offset, limit
	return f.operationSummaries, f.operationTotal, f.operationPageErr
}

func (f *fakeBackend) CloudOperationCursorPage(_ context.Context, filter taskstore.RemoteOperationFilter, before *taskstore.RemoteOperationCursor, limit int) ([]taskstore.RemoteOperationSummary, int, *taskstore.RemoteOperationCursor, error) {
	f.operationCalls++
	f.operationFilter, f.operationCursor, f.operationLimit = filter, before, limit
	return f.operationSummaries, f.operationTotal, f.operationNextCursor, f.operationPageErr
}

func (f *fakeBackend) CloudOperationExport(_ context.Context, filter taskstore.RemoteOperationFilter) ([]taskstore.RemoteOperationSummary, error) {
	f.operationExportCalls++
	f.operationExportFilter = filter
	return f.operationExports, f.operationExportErr
}

func (f *fakeBackend) CloudOperationEventPage(_ context.Context, id string, filter taskstore.RemoteOperationEventFilter, before int64, limit int) ([]taskstore.RemoteOperationEvent, int64, bool, error) {
	f.operationEventCalls++
	f.operationEventID, f.operationBefore, f.operationEventLimit = id, before, limit
	f.operationEventStatus, f.operationEventFilter = filter.Status, filter
	return f.operationEvents, f.operationEventBefore, false, f.operationEventErr
}

func (f *fakeBackend) CloudOperationEvidenceExportFiltered(_ context.Context, id string, filter taskstore.RemoteOperationEventFilter) (taskstore.RemoteOperationEvidenceBundle, error) {
	f.operationEvidenceExportCalls++
	f.operationEvidenceExportID, f.operationEventFilter = id, filter
	return f.operationEvidenceExport, f.operationEvidenceExportErr
}

func (f *fakeBackend) CloudOperationEvidenceExport(_ context.Context, id string) (taskstore.RemoteOperationEvidenceBundle, error) {
	f.operationEvidenceExportCalls++
	f.operationEvidenceExportID = id
	return f.operationEvidenceExport, f.operationEvidenceExportErr
}

func (f *fakeBackend) ExportLocalDirectoryMigrationEvidence(context.Context) (taskstore.LocalDirectoryMigrationEvidenceBundle, error) {
	f.localMigrationEvidenceCalls++
	return f.localMigrationEvidence, f.localMigrationEvidenceErr
}

func (f *fakeBackend) ListLocalDirectoryMigrations(_ context.Context, filter taskstore.LocalDirectoryMigrationFilter, offset, limit int) ([]taskstore.LocalDirectoryMigration, int, error) {
	f.localMigrationListCalls++
	f.localMigrationListOffset, f.localMigrationListLimit, f.localMigrationFilter = offset, limit, filter
	return f.localMigrationList, f.localMigrationListTotal, f.localMigrationListErr
}

func (f *fakeBackend) ExportLocalDirectoryMigrationEvidenceByID(_ context.Context, id string) (taskstore.LocalDirectoryMigrationEvidenceBundle, error) {
	f.localMigrationByIDCalls++
	f.localMigrationByIDValue = id
	return f.localMigrationByID, f.localMigrationByIDErr
}

func (f *fakeBackend) ExportLocalDirectoryMigrationSummaries(_ context.Context, filter taskstore.LocalDirectoryMigrationFilter) ([]taskstore.LocalDirectoryMigration, error) {
	f.localMigrationSummaryCalls++
	f.localMigrationSummaryFilter = filter
	return f.localMigrationSummaries, f.localMigrationSummariesErr
}

func (f *fakeBackend) LocalDirectoryMigrationEventPage(_ context.Context, id string, before int64, limit int) ([]taskstore.LocalDirectoryMigrationEvidenceEvent, int64, bool, error) {
	f.localMigrationEventsCalls++
	f.localMigrationEventsID, f.localMigrationEventsBefore, f.localMigrationEventsLimit = id, before, limit
	return f.localMigrationEvents, f.localMigrationEventsNext, f.localMigrationEventsMore, f.localMigrationEventsErr
}

func TestIntakeHTTPUsesStrictInputsAndNeverAddsManualRelease(t *testing.T) {
	backend := &fakeBackend{}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandlePreview(w, httptest.NewRequest("POST", "/api/intakes/preview", strings.NewReader(`{"paths":["/a"],"target":"/elsewhere"}`)))
	if w.Code != 400 || backend.previews != 0 {
		t.Fatal("client-selected target accepted")
	}
	w = httptest.NewRecorder()
	server.HandlePreview(w, httptest.NewRequest("POST", "/api/intakes/preview", strings.NewReader(`{"paths":["/a"]}`)))
	if w.Code != 200 || backend.previews != 1 {
		t.Fatal("preview not invoked")
	}
	w = httptest.NewRecorder()
	server.HandleAction(w, httptest.NewRequest("POST", "/api/intakes/"+strings.Repeat("a", 32)+"/commit", strings.NewReader(`{}`)))
	if w.Code != 200 || backend.commits != 1 || !strings.Contains(w.Body.String(), `"state":"unknown"`) {
		t.Fatal("partial/unknown result hidden")
	}
	for _, action := range []string{"release", "acknowledge", "force", "confirm-manual"} {
		w = httptest.NewRecorder()
		server.HandleAction(w, httptest.NewRequest("POST", "/api/intakes/"+strings.Repeat("a", 32)+"/"+action, nil))
		if w.Code != 404 {
			t.Fatal("manual release route exists")
		}
	}
}

func TestIntakeRecordsCursorPageSurvivesNewRecordAtHead(t *testing.T) {
	base := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	backend := &fakeBackend{intakes: make([]downloader.Intake, 21)}
	for i := range backend.intakes {
		backend.intakes[i] = downloader.Intake{ID: fmt.Sprintf("%032x", i+1), State: "finished", Created: base.Add(-time.Duration(i) * time.Minute)}
	}
	server := New(backend, nil)
	firstResponse := httptest.NewRecorder()
	server.HandleRecords(firstResponse, httptest.NewRequest(http.MethodGet, "/api/intakes?limit=20", nil))
	if firstResponse.Code != http.StatusOK {
		t.Fatalf("first page status=%d body=%s", firstResponse.Code, firstResponse.Body.String())
	}
	var first struct {
		Records    []downloader.Intake `json:"records"`
		NextCursor string              `json:"next_cursor"`
	}
	if err := json.Unmarshal(firstResponse.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Records) != 20 || first.NextCursor == "" {
		t.Fatalf("first page should provide a full page and cursor: count=%d cursor=%q body=%s", len(first.Records), first.NextCursor, firstResponse.Body.String())
	}
	backend.intakes = append([]downloader.Intake{{ID: fmt.Sprintf("%032x", 100), State: "preview", Created: base.Add(time.Minute)}}, backend.intakes...)
	nextRequest := httptest.NewRequest(http.MethodGet, "/api/intakes?limit=20&cursor="+url.QueryEscape(first.NextCursor), nil)
	nextResponse := httptest.NewRecorder()
	server.HandleRecords(nextResponse, nextRequest)
	if nextResponse.Code != http.StatusOK {
		t.Fatalf("next page status=%d body=%s", nextResponse.Code, nextResponse.Body.String())
	}
	var next struct {
		Records []downloader.Intake `json:"records"`
	}
	if err := json.Unmarshal(nextResponse.Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if len(next.Records) != 1 || next.Records[0].ID != backend.intakes[21].ID {
		t.Fatalf("head insertion shifted the cursor page: got=%+v expected id=%s", next.Records, backend.intakes[21].ID)
	}
}

func TestReadOnlyCloudErrorsNeverUseMutationWarning(t *testing.T) {
	errRead := errors.New("read failed")
	cases := []struct {
		name   string
		path   string
		setErr func(*fakeBackend)
		handle func(*Server, http.ResponseWriter, *http.Request)
	}{
		{name: "live listing", path: "/api/cloud", setErr: func(b *fakeBackend) { b.cloudErr = errRead }, handle: (*Server).HandleCloud},
		{name: "cached listing", path: "/api/cloud-inventory", setErr: func(b *fakeBackend) { b.cachedCloudErr = errRead }, handle: (*Server).HandleInventory},
		{name: "directory size", path: "/api/cloud-inventory/size", setErr: func(b *fakeBackend) { b.cloudSizeErr = errRead }, handle: (*Server).HandleInventorySize},
		{name: "empty-directory preview", path: "/api/cloud-cleanup/empty-directories", setErr: func(b *fakeBackend) { b.emptyDirectoriesErr = errRead }, handle: (*Server).HandleEmptyCloudDirectories},
		{name: "empty-directory scan status", path: "/api/cloud-cleanup/empty-directories", setErr: func(b *fakeBackend) { b.inventoryScanStatusErr = errRead }, handle: (*Server).HandleEmptyCloudDirectories},
		{name: "duplicate scan status", path: "/api/cloud-cleanup/duplicate-scan", setErr: func(b *fakeBackend) { b.duplicateStatusErr = errRead }, handle: (*Server).HandleDuplicateScan},
		{name: "duplicate scan events", path: "/api/cloud-cleanup/duplicate-scan/events", setErr: func(b *fakeBackend) { b.duplicateEventPageErr = errRead }, handle: (*Server).HandleDuplicateScanEvents},
		{name: "duplicate preview", path: "/api/cloud-cleanup/duplicates", setErr: func(b *fakeBackend) { b.duplicatePreviewErr = errRead }, handle: (*Server).HandleDuplicatePreview},
		{name: "inventory scan status", path: "/api/cloud-inventory/scan", setErr: func(b *fakeBackend) { b.inventoryScanStatusErr = errRead }, handle: (*Server).HandleInventoryScan},
		{name: "inventory refresh status", path: "/api/cloud-inventory/refresh", setErr: func(b *fakeBackend) { b.inventoryRefreshStatusErr = errRead }, handle: (*Server).HandleInventoryRefresh},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeBackend{}
			test.setErr(backend)
			server := New(backend, nil)
			response := httptest.NewRecorder()
			test.handle(server, response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "读取网盘信息失败") || strings.Contains(response.Body.String(), "不要盲目重发移动") {
				t.Fatalf("read-only error was misclassified: status=%d body=%q", response.Code, response.Body.String())
			}
		})
	}

	backend := &fakeBackend{}
	server := New(backend, nil)
	response := httptest.NewRecorder()
	server.HandleCloud(response, httptest.NewRequest(http.MethodGet, "/api/cloud?limit=0", nil))
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "不要盲目重发移动") {
		t.Fatalf("invalid read pagination was misclassified: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestIntakeRecordsRejectsInvalidCursor(t *testing.T) {
	server := New(&fakeBackend{}, nil)
	for _, target := range []string{
		"/api/intakes?cursor=not-base64!",
		"/api/intakes?cursor=e30",
		"/api/intakes?cursor=eyJjcmVhdGVkIjoiMjAyNi0xMC0zMFQwMDowMDowMFoiLCJpZCI6IngiLCJleHRyYSI6MX0",
		"/api/intakes?cursor=e30&cursor=e30",
		"/api/intakes?offset=1&cursor=e30",
	} {
		response := httptest.NewRecorder()
		server.HandleRecords(response, httptest.NewRequest(http.MethodGet, target, nil))
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid cursor accepted: target=%s status=%d body=%s", target, response.Code, response.Body.String())
		}
	}
}

func TestIntakeRecordsCursorUsesStableIDTieBreakAndKeepsOffsetCompatibility(t *testing.T) {
	created := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	backend := &fakeBackend{intakes: []downloader.Intake{
		{ID: fmt.Sprintf("%032x", 1), State: "finished", Created: created},
		{ID: fmt.Sprintf("%032x", 255), State: "finished", Created: created},
		{ID: fmt.Sprintf("%032x", 16), State: "finished", Created: created},
	}}
	server := New(backend, nil)
	first := httptest.NewRecorder()
	server.HandleRecords(first, httptest.NewRequest(http.MethodGet, "/api/intakes?limit=2", nil))
	var page struct {
		Records    []downloader.Intake `json:"records"`
		NextCursor string              `json:"next_cursor"`
	}
	if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &page) != nil || len(page.Records) != 2 || page.Records[0].ID != fmt.Sprintf("%032x", 255) || page.Records[1].ID != fmt.Sprintf("%032x", 16) || page.NextCursor == "" {
		t.Fatalf("same-time cursor order was not stable: status=%d body=%s", first.Code, first.Body.String())
	}
	last := httptest.NewRecorder()
	server.HandleRecords(last, httptest.NewRequest(http.MethodGet, "/api/intakes?limit=2&cursor="+url.QueryEscape(page.NextCursor), nil))
	var tail struct {
		Records []downloader.Intake `json:"records"`
	}
	if last.Code != http.StatusOK || json.Unmarshal(last.Body.Bytes(), &tail) != nil || len(tail.Records) != 1 || tail.Records[0].ID != fmt.Sprintf("%032x", 1) {
		t.Fatalf("same-time cursor tail mismatch: status=%d body=%s", last.Code, last.Body.String())
	}
	offset := httptest.NewRecorder()
	server.HandleRecords(offset, httptest.NewRequest(http.MethodGet, "/api/intakes?offset=0&limit=2", nil))
	if offset.Code != http.StatusOK || !strings.Contains(offset.Body.String(), `"next_offset":2`) || strings.Contains(offset.Body.String(), `"next_cursor"`) {
		t.Fatalf("legacy offset response changed: status=%d body=%s", offset.Code, offset.Body.String())
	}
}

func TestIntakeRecordsCursorCombinesWithUnresolvedFilter(t *testing.T) {
	created := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	backend := &fakeBackend{intakes: []downloader.Intake{
		{ID: fmt.Sprintf("%032x", 3), State: "finished", Created: created, Items: []downloader.IntakeItem{{State: "unknown"}}},
		{ID: fmt.Sprintf("%032x", 2), State: "finished", Created: created.Add(-time.Minute), Items: []downloader.IntakeItem{{State: "sending"}}},
		{ID: fmt.Sprintf("%032x", 1), State: "finished", Created: created.Add(-2 * time.Minute), Items: []downloader.IntakeItem{{State: "moved"}}},
	}}
	server := New(backend, nil)
	first := httptest.NewRecorder()
	server.HandleRecords(first, httptest.NewRequest(http.MethodGet, "/api/intakes?limit=1&unresolved=1", nil))
	var page struct {
		Records    []downloader.Intake `json:"records"`
		NextCursor string              `json:"next_cursor"`
	}
	if first.Code != http.StatusOK || json.Unmarshal(first.Body.Bytes(), &page) != nil || len(page.Records) != 1 || page.Records[0].ID != fmt.Sprintf("%032x", 3) || page.NextCursor == "" {
		t.Fatalf("first unresolved page mismatch: status=%d body=%s", first.Code, first.Body.String())
	}
	second := httptest.NewRecorder()
	target := "/api/intakes?limit=1&unresolved=1&cursor=" + url.QueryEscape(page.NextCursor)
	server.HandleRecords(second, httptest.NewRequest(http.MethodGet, target, nil))
	var next struct {
		Records []downloader.Intake `json:"records"`
	}
	if second.Code != http.StatusOK || json.Unmarshal(second.Body.Bytes(), &next) != nil || len(next.Records) != 1 || next.Records[0].ID != fmt.Sprintf("%032x", 2) {
		t.Fatalf("next unresolved page mismatch: status=%d body=%s", second.Code, second.Body.String())
	}
}

func TestDuplicateScanEventsExposeNewestPageAndMaskEvidence(t *testing.T) {
	backend := &fakeBackend{duplicateEvents: []taskstore.CloudDuplicateScanEvent{{Sequence: 9, RunID: "run", Status: "failed", Phase: "failed", Evidence: json.RawMessage(`{"last_error":"bad","refresh_token":"secret"}`)}}, duplicateEventsNext: 9, duplicateEventsMore: true}
	s := New(backend, func() {})
	w := httptest.NewRecorder()
	s.HandleDuplicateScanEvents(w, httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicate-scan/events?before_sequence=10&limit=7", nil))
	if w.Code != http.StatusOK || backend.duplicateEventsBefore != 10 || backend.duplicateEventsLimit != 7 || !strings.Contains(w.Body.String(), `"next_before_sequence":9`) || strings.Contains(w.Body.String(), `"refresh_token":"secret"`) || !strings.Contains(w.Body.String(), `"refresh_token":"[REDACTED]"`) {
		t.Fatalf("events response=%d %s backend=%+v", w.Code, w.Body.String(), backend)
	}
}

func TestDuplicateScanEventsRejectInvalidPagination(t *testing.T) {
	backend := &fakeBackend{}
	s := New(backend, func() {})
	for _, target := range []string{
		"/api/cloud-cleanup/duplicate-scan/events?before_sequence=-1",
		"/api/cloud-cleanup/duplicate-scan/events?limit=51",
		"/api/cloud-cleanup/duplicate-scan/events?limit=1&limit=2",
		"/api/cloud-cleanup/duplicate-scan/events?before_sequence=1&before_sequence=2",
		"/api/cloud-cleanup/duplicate-scan/events?offset=1",
	} {
		w := httptest.NewRecorder()
		s.HandleDuplicateScanEvents(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", target, w.Code, w.Body.String())
		}
	}
	if backend.duplicateEventsLimit != 0 {
		t.Fatalf("invalid event page reached backend: %+v", backend)
	}
}

func TestDuplicateScanRunHistoryFiltersDTOsAndExportsSelectedEvidence(t *testing.T) {
	runID := strings.Repeat("a", 32)
	backend := &fakeBackend{
		duplicateRuns:     []taskstore.CloudDuplicateScanRun{{StartSequence: 40, AccountFingerprint: "private-fingerprint", RunID: runID, Status: "complete", Phase: "complete", ProcessedFiles: 5}},
		duplicateRunsNext: 40, duplicateRunsMore: true,
		duplicateRunsExport: []taskstore.CloudDuplicateScanRun{{StartSequence: 40, AccountFingerprint: "private-fingerprint", RunID: runID, Status: "complete"}},
		duplicateRunEvents:  []taskstore.CloudDuplicateScanEvent{{Sequence: 41, RunID: runID, Status: "complete", Phase: "complete", Evidence: json.RawMessage(`{"refresh_token":"secret"}`)}},
		duplicateEvidence:   taskstore.CloudDuplicateScanEvidenceBundle{Run: taskstore.CloudDuplicateScanRun{RunID: runID, AccountFingerprint: "private-fingerprint", Status: "complete"}, Events: []taskstore.CloudDuplicateScanEvent{{Sequence: 41, RunID: runID, Status: "complete", Phase: "complete", Evidence: json.RawMessage(`{"refresh_token":"secret"}`)}}},
	}
	s := New(backend, func() {})
	page := httptest.NewRecorder()
	s.HandleDuplicateScanRuns(page, httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicate-scan/runs?before_start_sequence=50&limit=10&status=complete&started_from_utc=2026-10-30", nil))
	if page.Code != http.StatusOK || backend.duplicateRunsBefore != 50 || backend.duplicateRunsLimit != 10 || backend.duplicateRunsFilter.Status != "complete" || backend.duplicateRunsFilter.StartedFromUTC != "2026-10-30" || !strings.Contains(page.Body.String(), runID) || strings.Contains(page.Body.String(), "private-fingerprint") || !strings.Contains(page.Body.String(), `"next_before_start_sequence":40`) {
		t.Fatalf("run page=%d %s backend=%+v", page.Code, page.Body.String(), backend)
	}
	bad := httptest.NewRecorder()
	s.HandleDuplicateScanRuns(bad, httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicate-scan/runs?unexpected=1", nil))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("unknown run filter status=%d body=%s", bad.Code, bad.Body.String())
	}
	summary := httptest.NewRecorder()
	s.HandleDuplicateScanRunSummaryExport(summary, httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicate-scan/runs/export?status=complete", nil))
	if summary.Code != http.StatusOK || backend.duplicateRunsExportCalls != 1 || backend.duplicateRunsExportFilter.Status != "complete" || strings.Contains(summary.Body.String(), "private-fingerprint") || !strings.Contains(summary.Body.String(), `"total_runs":1`) || !strings.Contains(summary.Body.String(), `"filter":{"status":"complete","started_from_utc":"","started_through_utc":""}`) {
		t.Fatalf("summary export=%d %s backend=%+v", summary.Code, summary.Body.String(), backend)
	}
	for _, target := range []string{"/api/cloud-cleanup/duplicate-scan/runs?status=invalid", "/api/cloud-cleanup/duplicate-scan/runs?started_from_utc=2026-02-30", "/api/cloud-cleanup/duplicate-scan/runs?started_from_utc=2026-10-31&started_through_utc=2026-10-30", "/api/cloud-cleanup/duplicate-scan/runs/export?status=invalid"} {
		w := httptest.NewRecorder()
		if strings.Contains(target, "/export") {
			s.HandleDuplicateScanRunSummaryExport(w, httptest.NewRequest(http.MethodGet, target, nil))
		} else {
			s.HandleDuplicateScanRuns(w, httptest.NewRequest(http.MethodGet, target, nil))
		}
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid filter %s status=%d body=%s", target, w.Code, w.Body.String())
		}
	}
	eventRequest := httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicate-scan/runs/"+runID+"/events?before_sequence=45&limit=7", nil)
	eventRequest.SetPathValue("id", runID)
	events := httptest.NewRecorder()
	s.HandleDuplicateScanRunEvents(events, eventRequest)
	if events.Code != http.StatusOK || backend.duplicateRunEventsID != runID || backend.duplicateRunEventsBefore != 45 || backend.duplicateRunEventsLimit != 7 || strings.Contains(events.Body.String(), `"refresh_token":"secret"`) || !strings.Contains(events.Body.String(), `"refresh_token":"[REDACTED]"`) {
		t.Fatalf("run events=%d %s backend=%+v", events.Code, events.Body.String(), backend)
	}
	evidenceRequest := httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicate-scan/runs/"+runID+"/export", nil)
	evidenceRequest.SetPathValue("id", runID)
	evidence := httptest.NewRecorder()
	s.HandleDuplicateScanEvidenceExport(evidence, evidenceRequest)
	if evidence.Code != http.StatusOK || backend.duplicateEvidenceCalls != 1 || backend.duplicateEvidenceID != runID || strings.Contains(evidence.Body.String(), "private-fingerprint") || strings.Contains(evidence.Body.String(), `"refresh_token":"secret"`) || !strings.Contains(evidence.Body.String(), `"refresh_token":"[REDACTED]"`) {
		t.Fatalf("selected evidence=%d %s backend=%+v", evidence.Code, evidence.Body.String(), backend)
	}
}

func TestDuplicateScanEventsDefaultToTwentyNewest(t *testing.T) {
	backend := &fakeBackend{}
	s := New(backend, func() {})
	w := httptest.NewRecorder()
	s.HandleDuplicateScanEvents(w, httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicate-scan/events", nil))
	if w.Code != http.StatusOK || backend.duplicateEventsBefore != 0 || backend.duplicateEventsLimit != 20 || !strings.Contains(w.Body.String(), `"events":[]`) || !strings.Contains(w.Body.String(), `"next_before_sequence":null`) {
		t.Fatalf("default event page=%d %s backend=%+v", w.Code, w.Body.String(), backend)
	}
}

func TestCloudMutationFailureKeepsUnknownResultWarning(t *testing.T) {
	backend := &fakeBackend{mutationErr: errors.New("provider request failed")}
	server := New(backend, nil)
	response := httptest.NewRecorder()
	server.HandleCloudMutation(response, httptest.NewRequest(http.MethodPost, "/api/cloud/mutations", strings.NewReader(`{"operation":"mkdir","destination_directory":"/","new_name":"queue"}`)))
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), "不要盲目重发移动") {
		t.Fatalf("cloud mutation failure lost its unknown-result warning: status=%d body=%q", response.Code, response.Body.String())
	}
}

func TestCloudInventorySearchValidatesAndStaysLocal(t *testing.T) {
	backend := &fakeBackend{}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleInventorySearch(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/search?q=%25%25report&offset=10&limit=25", nil))
	if w.Code != http.StatusOK || backend.searchCalls != 1 || backend.searchQuery != "%%report" || backend.searchOffset != 10 || backend.searchLimit != 25 || backend.cloudCalls != 0 || !strings.Contains(w.Body.String(), `"next_offset":11`) || !strings.Contains(w.Body.String(), `"incomplete_directories":2`) {
		t.Fatalf("status=%d calls=%d query=%q offset=%d limit=%d liveCalls=%d body=%s", w.Code, backend.searchCalls, backend.searchQuery, backend.searchOffset, backend.searchLimit, backend.cloudCalls, w.Body.String())
	}
	for _, target := range []string{
		"/api/cloud-inventory/search",
		"/api/cloud-inventory/search?q=%20%20",
		"/api/cloud-inventory/search?q=report&limit=101",
	} {
		w = httptest.NewRecorder()
		server.HandleInventorySearch(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid search %q status=%d body=%s", target, w.Code, w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	server.HandleInventorySearch(w, httptest.NewRequest(http.MethodPost, "/api/cloud-inventory/search?q=report", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("unexpected method status=%d", w.Code)
	}
	if backend.searchCalls != 1 || backend.cloudCalls != 0 {
		t.Fatalf("validation called backend unexpectedly: search=%d live=%d", backend.searchCalls, backend.cloudCalls)
	}
}

func TestIncompleteCloudInventoryDirectoryEndpointIsLocalAndGenerationBound(t *testing.T) {
	path := "/资料/报表"
	backend := &fakeBackend{incompleteDirectoryPage: taskstore.CloudInventoryIncompleteDirectoryPage{Entries: []taskstore.CloudInventoryIncompleteDirectory{{Path: path, Status: "stale", Reason: "stale", CachedDirectory: true}}, Total: 9, NextBeforePath: path, HasMore: true, Generation: 17}}
	server := New(backend, nil)
	w := httptest.NewRecorder()
	server.HandleIncompleteCloudInventoryDirectories(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/incomplete-directories?reason=stale&q=%E6%8A%A5%E8%A1%A8&limit=7&cache_generation=17", nil))
	if w.Code != http.StatusOK || backend.incompleteDirectoryReason != "stale" || backend.incompleteDirectoryQuery != "报表" || backend.incompleteDirectoryLimit != 7 || backend.incompleteDirectoryExpected == nil || *backend.incompleteDirectoryExpected != 17 || backend.cloudCalls != 0 || !strings.Contains(w.Body.String(), `"path":"/资料/报表"`) || strings.Contains(w.Body.String(), "account_fingerprint") {
		t.Fatalf("status=%d page=%+v liveCalls=%d body=%s", w.Code, backend, backend.cloudCalls, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleIncompleteCloudInventoryDirectories(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/incomplete-directories?cache_generation=16", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"cache_generation_changed"`) || strings.Contains(w.Body.String(), `"entries"`) {
		t.Fatalf("stale page status=%d body=%s", w.Code, w.Body.String())
	}
	for _, target := range []string{"/api/cloud-inventory/incomplete-directories?reason=not-a-reason", "/api/cloud-inventory/incomplete-directories?limit=101", "/api/cloud-inventory/incomplete-directories?cache_generation=1&cache_generation=2", "/api/cloud-inventory/incomplete-directories?unexpected=1"} {
		w = httptest.NewRecorder()
		server.HandleIncompleteCloudInventoryDirectories(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid query %s status=%d", target, w.Code)
		}
	}
	if backend.cloudCalls != 0 {
		t.Fatalf("local cache listing called cloud provider %d times", backend.cloudCalls)
	}
}

func TestCachedBrowseGenerationConflictAndValidation(t *testing.T) {
	server := New(&fakeBackend{}, nil)
	w := httptest.NewRecorder()
	server.HandleInventory(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory?dir=%2F&cache_generation=7", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"cache_generation":7`) {
		t.Fatalf("matching directory generation response status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventory(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory?dir=%2F&offset=10&cache_generation=6", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"cache_generation_changed"`) || !strings.Contains(w.Body.String(), `"cache_generation":7`) || strings.Contains(w.Body.String(), `"entries"`) {
		t.Fatalf("stale directory page response status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventorySearch(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/search?q=report&cache_generation=6", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"code":"cache_generation_changed"`) || strings.Contains(w.Body.String(), `"entries"`) {
		t.Fatalf("stale search page response status=%d body=%s", w.Code, w.Body.String())
	}
	for _, target := range []string{
		"/api/cloud-inventory?cache_generation=-1",
		"/api/cloud-inventory?cache_generation=1&cache_generation=2",
		"/api/cloud-inventory/search?q=x&cache_generation=nope",
	} {
		w = httptest.NewRecorder()
		if strings.Contains(target, "/search") {
			server.HandleInventorySearch(w, httptest.NewRequest(http.MethodGet, target, nil))
		} else {
			server.HandleInventory(w, httptest.NewRequest(http.MethodGet, target, nil))
		}
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid generation query %q status=%d body=%s", target, w.Code, w.Body.String())
		}
	}
}

func TestDuplicateCleanupEndpointsExposeStatusPreviewAndPause(t *testing.T) {
	backend := &fakeBackend{duplicatePreviewGroups: []taskstore.CloudDuplicateGroup{{SHA256: strings.Repeat("a", 64)}}, duplicatePreviewTotal: 3, duplicatePreviewDeleteCount: 5}
	s := New(backend, func() {})
	w := httptest.NewRecorder()
	s.HandleDuplicateScan(w, httptest.NewRequest(http.MethodPost, "/api/cloud-cleanup/duplicate-scan", nil))
	if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"status":"running"`) {
		t.Fatalf("start response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.HandleDuplicatePreview(w, httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicates?offset=1&limit=1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"total":3`) || !strings.Contains(w.Body.String(), `"delete_count":5`) || !strings.Contains(w.Body.String(), `"next_offset":2`) || backend.duplicatePreviewOffset != 1 || backend.duplicatePreviewLimit != 1 {
		t.Fatalf("preview response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	s.HandleDuplicateScan(w, httptest.NewRequest(http.MethodDelete, "/api/cloud-cleanup/duplicate-scan", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("pause response %d", w.Code)
	}
}

func TestDuplicatePreviewRejectsInvalidOrRepeatedPagination(t *testing.T) {
	backend := &fakeBackend{}
	s := New(backend, func() {})
	for _, target := range []string{
		"/api/cloud-cleanup/duplicates?offset=-1",
		"/api/cloud-cleanup/duplicates?limit=51",
		"/api/cloud-cleanup/duplicates?offset=0&offset=1",
		"/api/cloud-cleanup/duplicates?limit=1&limit=2",
	} {
		w := httptest.NewRecorder()
		s.HandleDuplicatePreview(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s status=%d body=%s", target, w.Code, w.Body.String())
		}
	}
	if backend.duplicatePreviewLimit != 0 {
		t.Fatalf("invalid page reached backend: %+v", backend)
	}
}

func TestDuplicatePreviewUsesBoundedDefaultPage(t *testing.T) {
	backend := &fakeBackend{duplicatePreviewGroups: make([]taskstore.CloudDuplicateGroup, 25), duplicatePreviewTotal: 60, duplicatePreviewDeleteCount: 63}
	s := New(backend, func() {})
	w := httptest.NewRecorder()
	s.HandleDuplicatePreview(w, httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicates", nil))
	if w.Code != http.StatusOK || backend.duplicatePreviewOffset != 0 || backend.duplicatePreviewLimit != 25 || !strings.Contains(w.Body.String(), `"total":60`) || !strings.Contains(w.Body.String(), `"delete_count":63`) || !strings.Contains(w.Body.String(), `"next_offset":25`) {
		t.Fatalf("default preview response=%d %s backend=%+v", w.Code, w.Body.String(), backend)
	}
}

func TestCloudMutationAPIRequiresStrictExpectedIdentity(t *testing.T) {
	server := New(&fakeBackend{}, func() {})
	w := httptest.NewRecorder()
	server.HandleCloudMutation(w, httptest.NewRequest("POST", "/api/cloud/mutations", strings.NewReader(`{"operation":"delete","expected":{"fs_id":3,"path":"/a","server_filename":"a","size":1,"isdir":0,"server_mtime":4},"force":true}`)))
	if w.Code != 400 {
		t.Fatalf("unknown mutation field accepted: %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleCloudMutation(w, httptest.NewRequest("POST", "/api/cloud/mutations", strings.NewReader(`{"operation":"delete","expected":{"fs_id":3,"path":"/a","server_filename":"a","size":1,"isdir":0,"server_mtime":4}}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"confirmed"`) {
		t.Fatalf("mutation response=%d %s", w.Code, w.Body.String())
	}
}

func TestConfirmedCloudMutationWithReconciliationErrorIsNotReportedUnknown(t *testing.T) {
	backend := &fakeBackend{
		mutationResult: downloader.CloudMutationResult{OperationID: strings.Repeat("c", 32), Status: "confirmed"},
		mutationErr:    errors.New("download queue reconciliation failed"),
	}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleCloudMutation(w, httptest.NewRequest(http.MethodPost, "/api/cloud/mutations", strings.NewReader(`{"operation":"mkdir","destination_directory":"/","new_name":"queue"}`)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"confirmed"`) || !strings.Contains(w.Body.String(), `"warning":"远端操作已确认`) || strings.Contains(w.Body.String(), "未知结果") {
		t.Fatalf("confirmed operation was misreported: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestDuplicateScanEstimateReturnsReadSizeBeforeStart(t *testing.T) {
	backend := &fakeBackend{duplicateEstimate: taskstore.CloudDuplicateHashCandidateEstimate{Files: 37, EstimatedBytes: 987654321}}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleDuplicateScan(w, httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicate-scan?estimate=1", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"files":37`) || !strings.Contains(w.Body.String(), `"estimated_bytes":987654321`) {
		t.Fatalf("unexpected duplicate scan estimate: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestDuplicateScanEstimateUsesReadOnlyFailureMessage(t *testing.T) {
	backend := &fakeBackend{duplicateEstimateErr: downloader.ErrCloudInventoryIncomplete}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleDuplicateScan(w, httptest.NewRequest(http.MethodGet, "/api/cloud-cleanup/duplicate-scan?estimate=1", nil))
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "全盘列表缓存已失效或尚未完整") || strings.Contains(w.Body.String(), "入队项目") || strings.Contains(w.Body.String(), "不要盲目重发移动") {
		t.Fatalf("estimate failure was mislabeled as remote mutation: status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestCloudInventoryEndpointsExposeCacheSizeAndBackgroundScan(t *testing.T) {
	backend := &fakeBackend{}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleInventory(w, httptest.NewRequest("GET", "/api/cloud-inventory?dir=%2F", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"complete"`) {
		t.Fatalf("inventory response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventorySize(w, httptest.NewRequest("GET", "/api/cloud-inventory/size?dir=%2F", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"complete":true`) {
		t.Fatalf("size response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventoryScan(w, httptest.NewRequest("GET", "/api/cloud-inventory/scan", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"status":"complete"`) || !strings.Contains(w.Body.String(), `"pending_count":0`) || !strings.Contains(w.Body.String(), `"boundary_restart_count":1`) || !strings.Contains(w.Body.String(), `"inventory_coverage":{"status":"incomplete","incomplete_directories":2,"cache_generation":9,"incomplete_reasons":{"scanning_directories":0,"stale_directories":4,"unknown_directories":0,"failed_directories":0,"missing_directory_status":1,"orphan_directory_status":0,"parent_not_complete":0,"path_mismatch":0,"missing_root":0}}`) || strings.Contains(w.Body.String(), `"pending":[`) {
		t.Fatalf("scan status response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventoryScan(w, httptest.NewRequest("POST", "/api/cloud-inventory/scan", strings.NewReader(`{"request_budget":25}`)))
	if w.Code != http.StatusAccepted || backend.inventoryScanBudget != 25 || !strings.Contains(w.Body.String(), `"status":"running"`) {
		t.Fatalf("scan start response %d %s", w.Code, w.Body.String())
	}
	for _, invalid := range []string{`{"request_budget":-1}`, `{"request_budget":100001}`} {
		w = httptest.NewRecorder()
		server.HandleInventoryScan(w, httptest.NewRequest("POST", "/api/cloud-inventory/scan", strings.NewReader(invalid)))
		if w.Code != http.StatusBadRequest || backend.inventoryScanBudget != 25 {
			t.Fatalf("invalid scan budget %s response=%d budget=%d", invalid, w.Code, backend.inventoryScanBudget)
		}
	}
	w = httptest.NewRecorder()
	server.HandleInventoryScan(w, httptest.NewRequest("DELETE", "/api/cloud-inventory/scan", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("scan pause response %d", w.Code)
	}
}

func TestCloudInventoryRefreshEndpointStartsReportsAndPausesScopedRefresh(t *testing.T) {
	backend := &fakeBackend{}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleInventoryRefresh(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"complete"`) || !strings.Contains(w.Body.String(), `"root_path":"/target"`) || !strings.Contains(w.Body.String(), `"boundary_restart_count":1`) {
		t.Fatalf("refresh status response %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventoryRefresh(w, httptest.NewRequest(http.MethodPost, "/api/cloud-inventory/refresh", strings.NewReader(`{"directory":"/target/sub","request_budget":42}`)))
	if w.Code != http.StatusAccepted || backend.refreshDirectory != "/target/sub" || backend.refreshBudget != 42 || !strings.Contains(w.Body.String(), `"status":"running"`) {
		t.Fatalf("refresh start response %d directory=%q budget=%d body=%s", w.Code, backend.refreshDirectory, backend.refreshBudget, w.Body.String())
	}
	for _, budget := range []string{"-1", "100001"} {
		w = httptest.NewRecorder()
		server.HandleInventoryRefresh(w, httptest.NewRequest(http.MethodPost, "/api/cloud-inventory/refresh", strings.NewReader(`{"directory":"/target","request_budget":`+budget+`}`)))
		if w.Code != http.StatusBadRequest || backend.refreshBudget != 42 {
			t.Fatalf("invalid refresh budget %s response=%d backend budget=%d", budget, w.Code, backend.refreshBudget)
		}
	}
	w = httptest.NewRecorder()
	server.HandleInventoryRefresh(w, httptest.NewRequest(http.MethodPost, "/api/cloud-inventory/refresh", strings.NewReader(`{"directory":"/target","unexpected":true}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("refresh accepted unknown input field: status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleInventoryRefresh(w, httptest.NewRequest(http.MethodDelete, "/api/cloud-inventory/refresh", nil))
	if w.Code != http.StatusNoContent || backend.refreshPauseCalls != 1 {
		t.Fatalf("refresh pause response %d calls=%d", w.Code, backend.refreshPauseCalls)
	}
}

func TestFullScanHistoryEndpointsAreReadOnlyAndValidatePaging(t *testing.T) {
	backend := &fakeBackend{}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleCloudScanHistoryRuns(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/scan/runs?limit=20", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"run_id":"run-one"`) || !strings.Contains(w.Body.String(), `"coverage_generation":42`) || !strings.Contains(w.Body.String(), `"stale_directories":4`) || strings.Contains(w.Body.String(), "private-fingerprint") || strings.Contains(w.Body.String(), "account_fingerprint") || !strings.Contains(w.Body.String(), `"has_more":false`) {
		t.Fatalf("scan history runs response=%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleCloudScanHistoryRuns(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/scan/runs?status=failed&started_from_utc=2026-10-01&started_through_utc=2026-10-31", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("valid scan history filter response=%d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleCloudScanHistoryRuns(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/scan/runs?coverage_status=unrecorded&incomplete_directories_min=0&incomplete_directories_max=5&coverage_reason=stale_directories", nil))
	filter := backend.scanHistoryFilter
	if w.Code != http.StatusOK || filter.CoverageStatus != "unrecorded" || filter.CoverageReason != "stale_directories" || filter.CoverageIncompleteMin == nil || *filter.CoverageIncompleteMin != 0 || filter.CoverageIncompleteMax == nil || *filter.CoverageIncompleteMax != 5 {
		t.Fatalf("coverage history filters response=%d filter=%+v body=%s", w.Code, filter, w.Body.String())
	}
	w = httptest.NewRecorder()
	server.HandleCloudScanHistoryEvents(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/scan/events?run_id=run-one", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"kind":"completed"`) || strings.Contains(w.Body.String(), "private-fingerprint") || strings.Contains(w.Body.String(), "account_fingerprint") {
		t.Fatalf("scan history events response=%d %s", w.Code, w.Body.String())
	}
	for _, target := range []string{"/api/cloud-inventory/scan/runs?unknown=1", "/api/cloud-inventory/scan/runs?status=bogus", "/api/cloud-inventory/scan/runs?started_from_utc=not-a-date", "/api/cloud-inventory/scan/events?run_id=run-one&before_sequence=-1", "/api/cloud-inventory/scan/events"} {
		w = httptest.NewRecorder()
		server.HandleCloudScanHistoryEvents(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid scan history query %s response=%d", target, w.Code)
		}
	}
	for _, target := range []string{
		"/api/cloud-inventory/scan/runs?coverage_status=bogus",
		"/api/cloud-inventory/scan/runs?coverage_reason=unknown_column",
		"/api/cloud-inventory/scan/runs?incomplete_directories_min=-1",
		"/api/cloud-inventory/scan/runs?incomplete_directories_max=1000001",
		"/api/cloud-inventory/scan/runs?incomplete_directories_min=3&incomplete_directories_max=2",
		"/api/cloud-inventory/scan/runs?coverage_reason=stale_directories&coverage_reason=missing_root",
	} {
		w = httptest.NewRecorder()
		server.HandleCloudScanHistoryRuns(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("invalid scan history coverage query %s response=%d body=%s", target, w.Code, w.Body.String())
		}
	}
}

func TestFullScanEvidenceExportUsesAttachmentAndValidatesRunID(t *testing.T) {
	server := New(&fakeBackend{}, func() {})
	id := "0123456789abcdef0123456789abcdef"
	r := httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/scan/runs/"+id+"/export", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	server.HandleCloudScanHistoryExport(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Header().Get("Content-Disposition"), "cloud-scan-"+id+".json") || !strings.Contains(w.Body.String(), `"total_events":1`) || !strings.Contains(w.Body.String(), `"coverage_generation":42`) || !strings.Contains(w.Body.String(), `"stale_directories":4`) || !strings.Contains(w.Body.String(), `"coverage_observed_at_utc":"2026-11-02T03:04:05Z"`) || strings.Contains(w.Body.String(), "account_fingerprint") || strings.Contains(w.Body.String(), "private-fingerprint") {
		t.Fatalf("full-scan export response=%d headers=%v body=%s", w.Code, w.Header(), w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/scan/runs/not-hex/export", nil)
	r.SetPathValue("id", "not-hex")
	w = httptest.NewRecorder()
	server.HandleCloudScanHistoryExport(w, r)
	if w.Code != http.StatusNotFound {
		t.Fatalf("invalid run export status=%d", w.Code)
	}
}

func TestCloudInventoryRefreshEventsEndpointUsesBoundedStableCursor(t *testing.T) {
	added, removed, changed := int64(2), int64(3), int64(4)
	offset, fsID := int64(100), int64(9001)
	backend := &fakeBackend{refreshEvents: []taskstore.CloudInventoryRefreshEvent{{Sequence: 9, AccountFingerprint: "private-fingerprint", RunID: "run-1", Kind: "boundary_restart", RestartReason: "provider_duplicate_page_id", RestartOffset: &offset, RestartFSID: &fsID, CurrentDirectory: "/a", AddedCount: &added, RemovedCount: &removed, ChangedCount: &changed}}, refreshEventsNext: 9, refreshEventsMore: true}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleInventoryRefreshEvents(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/events?run_id=run-1&kind=boundary_restart&restart_reason=provider_duplicate_page_id&before_sequence=12&limit=1", nil))
	if w.Code != http.StatusOK || backend.refreshEventsRunID != "run-1" || backend.refreshEventsKind != "boundary_restart" || backend.refreshEventsReason != "provider_duplicate_page_id" || backend.refreshEventsBefore != 12 || backend.refreshEventsLimit != 1 || !strings.Contains(w.Body.String(), `"next_before_sequence":9`) || !strings.Contains(w.Body.String(), `"has_more":true`) || !strings.Contains(w.Body.String(), `"restart_offset":100`) || !strings.Contains(w.Body.String(), `"restart_fs_id":9001`) {
		t.Fatalf("refresh event page status=%d call=(%q,%q,%d,%d) body=%s", w.Code, backend.refreshEventsRunID, backend.refreshEventsKind, backend.refreshEventsBefore, backend.refreshEventsLimit, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-fingerprint") || strings.Contains(w.Body.String(), "account_fingerprint") {
		t.Fatalf("refresh event API exposed internal account identity: %s", w.Body.String())
	}
	for _, target := range []string{
		"/api/cloud-inventory/refresh/events?before_sequence=2",
		"/api/cloud-inventory/refresh/events?run_id=run-1&before_sequence=-1",
		"/api/cloud-inventory/refresh/events?run_id=run-1&limit=101",
		"/api/cloud-inventory/refresh/events?run_id=run-1&limit=1&limit=2",
		"/api/cloud-inventory/refresh/events?run_id=run-1&kind=not-real",
		"/api/cloud-inventory/refresh/events?run_id=run-1&restart_reason=not-real",
		"/api/cloud-inventory/refresh/events?run_id=run-1&kind=started&kind=failed",
		"/api/cloud-inventory/refresh/events?run_id=run-1&unknown=x",
	} {
		w = httptest.NewRecorder()
		server.HandleInventoryRefreshEvents(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid refresh event query accepted: %s -> %d %s", target, w.Code, w.Body.String())
		}
	}
}

func TestCloudInventoryRefreshEventsEndpointAcceptsKnownKindsAndAllKinds(t *testing.T) {
	backend := &fakeBackend{}
	server := New(backend, func() {})
	for _, kind := range []string{"", "started", "resumed", "paused", "failed", "completed", "directory_published"} {
		target := "/api/cloud-inventory/refresh/events?run_id=run-1"
		if kind != "" {
			target += "&kind=" + kind
		}
		w := httptest.NewRecorder()
		server.HandleInventoryRefreshEvents(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusOK || backend.refreshEventsKind != kind {
			t.Fatalf("kind %q status=%d passed=%q body=%s", kind, w.Code, backend.refreshEventsKind, w.Body.String())
		}
	}
}

func TestCloudInventoryRefreshChangesEndpointPagesPrivateSnapshots(t *testing.T) {
	backend := &fakeBackend{refreshChangePage: taskstore.CloudInventoryRefreshChangePage{Changes: []taskstore.CloudInventoryRefreshChange{{Sequence: 7, Ordinal: 1, Kind: "identity_changed", Old: &taskstore.CloudInventoryRefreshChangeItem{FsID: 10, Path: "/old", Name: "old", Size: 3}, New: &taskstore.CloudInventoryRefreshChangeItem{FsID: 11, Path: "/new", Name: "new", Size: 4}, RecordedAtUTC: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC), CloudObservedAtUTC: time.Date(2026, 10, 29, 23, 59, 0, 0, time.UTC)}}, MatchedCount: 1, NextBeforeSequence: 7, HasMore: true}}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleInventoryRefreshChanges(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=22&before_sequence=9&limit=1", nil))
	if w.Code != http.StatusOK || backend.refreshChangesRunID != "run-1" || backend.refreshChangesEventSequence != 22 || backend.refreshChangesBefore != 9 || backend.refreshChangesLimit != 1 || !strings.Contains(w.Body.String(), `"old"`) || !strings.Contains(w.Body.String(), `"new"`) || !strings.Contains(w.Body.String(), `"next_before_sequence":7`) || !strings.Contains(w.Body.String(), `"has_more":true`) {
		t.Fatalf("refresh changes status=%d call=(%q,%d,%d,%d) body=%s", w.Code, backend.refreshChangesRunID, backend.refreshChangesEventSequence, backend.refreshChangesBefore, backend.refreshChangesLimit, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "account_fingerprint") || strings.Contains(w.Body.String(), "private-fingerprint") {
		t.Fatalf("refresh change API exposed internal account identity: %s", w.Body.String())
	}
	for _, target := range []string{
		"/api/cloud-inventory/refresh/changes?run_id=run-1",
		"/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=0",
		"/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=22&before_sequence=-1",
		"/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=22&limit=51",
		"/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=22&limit=1&limit=2",
		"/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=22&unexpected=1",
	} {
		w = httptest.NewRecorder()
		server.HandleInventoryRefreshChanges(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid refresh changes query accepted: %s -> %d %s", target, w.Code, w.Body.String())
		}
	}
	backend.refreshChangesErr = taskstore.ErrCloudInventoryRefreshNotFound
	w = httptest.NewRecorder()
	server.HandleInventoryRefreshChanges(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=22", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing refresh event status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCloudInventoryRefreshChangesEndpointFiltersAndReturnsUnfilteredCounts(t *testing.T) {
	backend := &fakeBackend{refreshChangePage: taskstore.CloudInventoryRefreshChangePage{Changes: []taskstore.CloudInventoryRefreshChange{{Sequence: 7, Kind: "added"}}, Counts: taskstore.CloudInventoryRefreshChangeCounts{Total: 9, Added: 4, Removed: 2, MetadataChanged: 2, IdentityChanged: 1}, MatchedCount: 1, NextBeforeSequence: 7, HasMore: true}}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleInventoryRefreshChanges(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=22&before_sequence=9&limit=1&kind=added&q=%E6%8A%A5%E5%91%8A", nil))
	if w.Code != http.StatusOK || backend.refreshChangesKind != "added" || backend.refreshChangesQuery != "报告" || !strings.Contains(w.Body.String(), `"total":9`) || !strings.Contains(w.Body.String(), `"matched_count":1`) {
		t.Fatalf("filtered refresh changes status=%d kind=%q query=%q body=%s", w.Code, backend.refreshChangesKind, backend.refreshChangesQuery, w.Body.String())
	}
	for _, target := range []string{
		"/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=22&kind=not-real",
		"/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=22&kind=added&kind=removed",
		"/api/cloud-inventory/refresh/changes?run_id=run-1&event_sequence=22&q=" + strings.Repeat("x", 129),
	} {
		w = httptest.NewRecorder()
		server.HandleInventoryRefreshChanges(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid detail filter accepted: %s -> %d %s", target, w.Code, w.Body.String())
		}
	}
}

func TestCloudInventoryRefreshChangesExportEndpointIsBoundedAndPrivate(t *testing.T) {
	backend := &fakeBackend{refreshChangesExport: taskstore.CloudInventoryRefreshChangeExport{RunID: "run-1", EventSequence: 22, MatchedCount: 1, Changes: []taskstore.CloudInventoryRefreshChange{{Sequence: 7, Kind: "added"}}}}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleInventoryRefreshChangesExport(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/changes/export?run_id=run-1&event_sequence=22&kind=added&q=report", nil))
	if w.Code != http.StatusOK || backend.refreshChangesExportRunID != "run-1" || backend.refreshChangesExportEvent != 22 || backend.refreshChangesExportKind != "added" || backend.refreshChangesExportQuery != "report" || !strings.Contains(w.Header().Get("Content-Disposition"), "cloud-refresh-changes.json") || !strings.Contains(w.Body.String(), `"query":"report"`) || strings.Contains(w.Body.String(), "account_fingerprint") {
		t.Fatalf("refresh changes export status=%d call=(%q,%d,%q,%q) headers=%v body=%s", w.Code, backend.refreshChangesExportRunID, backend.refreshChangesExportEvent, backend.refreshChangesExportKind, backend.refreshChangesExportQuery, w.Header(), w.Body.String())
	}
	backend.refreshChangesExportErr = taskstore.ErrCloudInventoryRefreshChangesExportTooLarge
	w = httptest.NewRecorder()
	server.HandleInventoryRefreshChangesExport(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/changes/export?run_id=run-1&event_sequence=22", nil))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized detail export status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestCloudInventoryRefreshRunsEndpointUsesBoundedStableCursor(t *testing.T) {
	backend := &fakeBackend{refreshRuns: []taskstore.CloudInventoryRefreshRun{{StartSequence: 15, RunID: "run-1", LatestKind: "paused", Status: "paused", StartedAtUTC: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)}}, refreshRunsNext: 15, refreshRunsMore: true}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleInventoryRefreshRuns(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/runs?before_sequence=20&limit=1&status=paused&started_from_utc=2026-10-29&started_through_utc=2026-10-30", nil))
	if w.Code != http.StatusOK || backend.refreshRunsBefore != 20 || backend.refreshRunsLimit != 1 || backend.refreshRunsFilter != (taskstore.CloudInventoryRefreshRunFilter{Status: "paused", StartedFromUTC: "2026-10-29", StartedThroughUTC: "2026-10-30"}) || !strings.Contains(w.Body.String(), `"next_before_sequence":15`) || !strings.Contains(w.Body.String(), `"has_more":true`) || !strings.Contains(w.Body.String(), `"run_id":"run-1"`) {
		t.Fatalf("refresh run page status=%d call=(%d,%d) body=%s", w.Code, backend.refreshRunsBefore, backend.refreshRunsLimit, w.Body.String())
	}
	for _, target := range []string{
		"/api/cloud-inventory/refresh/runs?before_sequence=-1",
		"/api/cloud-inventory/refresh/runs?limit=101",
		"/api/cloud-inventory/refresh/runs?limit=1&limit=2",
		"/api/cloud-inventory/refresh/runs?unknown=1",
		"/api/cloud-inventory/refresh/runs?status=unknown",
		"/api/cloud-inventory/refresh/runs?started_from_utc=2026-02-30",
		"/api/cloud-inventory/refresh/runs?started_through_utc=2026-2-3",
		"/api/cloud-inventory/refresh/runs?started_from_utc=2026-10-30&started_through_utc=2026-10-29",
		"/api/cloud-inventory/refresh/runs?status=failed&status=paused",
	} {
		w = httptest.NewRecorder()
		server.HandleInventoryRefreshRuns(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid refresh run query accepted: %s -> %d %s", target, w.Code, w.Body.String())
		}
	}
}

func TestCloudInventoryRefreshRunSummaryExportUsesCurrentFiltersAndRejectsBadRequests(t *testing.T) {
	id := strings.Repeat("a", 32)
	f := &fakeBackend{refreshSummaryExport: []taskstore.CloudInventoryRefreshRun{{RunID: id, Status: "failed", StartedAtUTC: time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)}}}
	server := New(f, nil)
	w := httptest.NewRecorder()
	server.HandleInventoryRefreshRunSummaryExport(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/runs/export?status=failed&started_from_utc=2026-10-01&started_through_utc=2026-10-30", nil))
	if w.Code != http.StatusOK || f.refreshSummaryExportCalls != 1 || f.refreshSummaryExportFilter != (taskstore.CloudInventoryRefreshRunFilter{Status: "failed", StartedFromUTC: "2026-10-01", StartedThroughUTC: "2026-10-30"}) || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), `filename="cloud-refresh-runs-`) || !strings.Contains(w.Body.String(), `"total_runs":1`) || !strings.Contains(w.Body.String(), `"started_through_utc":"2026-10-30"`) {
		t.Fatalf("export status=%d calls=%d filter=%+v headers=%v body=%s", w.Code, f.refreshSummaryExportCalls, f.refreshSummaryExportFilter, w.Header(), w.Body.String())
	}
	if strings.Contains(w.Body.String(), "account_fingerprint") || strings.Contains(w.Body.String(), "private-fingerprint") {
		t.Fatalf("summary export leaked account identity: %s", w.Body.String())
	}
	f.refreshSummaryExportErr = taskstore.ErrCloudInventoryRefreshRunSummaryExportTooLarge
	w = httptest.NewRecorder()
	server.HandleInventoryRefreshRunSummaryExport(w, httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/runs/export", nil))
	if w.Code != http.StatusRequestEntityTooLarge || f.refreshSummaryExportCalls != 2 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("oversized export status=%d calls=%d headers=%v", w.Code, f.refreshSummaryExportCalls, w.Header())
	}
	for _, target := range []string{
		"/api/cloud-inventory/refresh/runs/export?status=unknown",
		"/api/cloud-inventory/refresh/runs/export?started_from_utc=2026-02-30",
		"/api/cloud-inventory/refresh/runs/export?started_from_utc=2026-10-30&started_through_utc=2026-10-29",
		"/api/cloud-inventory/refresh/runs/export?status=failed&status=paused",
		"/api/cloud-inventory/refresh/runs/export?before_sequence=10",
	} {
		w = httptest.NewRecorder()
		server.HandleInventoryRefreshRunSummaryExport(w, httptest.NewRequest(http.MethodGet, target, nil))
		if w.Code != http.StatusBadRequest {
			t.Errorf("invalid export query accepted: %s -> %d %s", target, w.Code, w.Body.String())
		}
	}
	w = httptest.NewRecorder()
	server.HandleInventoryRefreshRunSummaryExport(w, httptest.NewRequest(http.MethodPost, "/api/cloud-inventory/refresh/runs/export", nil))
	if w.Code != http.StatusMethodNotAllowed || f.refreshSummaryExportCalls != 2 {
		t.Fatalf("unexpected export method status=%d calls=%d", w.Code, f.refreshSummaryExportCalls)
	}
}

func TestCloudInventoryRefreshEvidenceExportIsExplicitBoundedAndPrivate(t *testing.T) {
	id := strings.Repeat("a", 32)
	f := &fakeBackend{refreshEvidence: taskstore.CloudInventoryRefreshEvidenceBundle{
		Run:    taskstore.CloudInventoryRefreshRun{RunID: id, Status: "complete", StartedAtUTC: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)},
		Events: []taskstore.CloudInventoryRefreshEvent{{Sequence: 1, AccountFingerprint: "private-fingerprint", RunID: id, Kind: "started", Status: "running", RecordedAtUTC: time.Date(2026, 10, 30, 1, 0, 0, 0, time.UTC)}},
	}}
	server := New(f, nil)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/runs/"+id+"/export", nil)
	r.SetPathValue("id", id)
	server.HandleInventoryRefreshEvidenceExport(w, r)
	if w.Code != http.StatusOK || f.refreshEvidenceCalls != 1 || f.refreshEvidenceID != id || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), `filename="cloud-refresh-`+id+`.json"`) {
		t.Fatalf("export status=%d calls=%d id=%s headers=%v body=%s", w.Code, f.refreshEvidenceCalls, f.refreshEvidenceID, w.Header(), w.Body.String())
	}
	if strings.Contains(w.Body.String(), "private-fingerprint") || strings.Contains(w.Body.String(), "account_fingerprint") || !strings.Contains(w.Body.String(), `"total_events":1`) || !strings.Contains(w.Body.String(), `"kind":"started"`) {
		t.Fatalf("export leaked private identity or omitted evidence: %s", w.Body.String())
	}
	f.refreshEvidenceErr = taskstore.ErrCloudInventoryRefreshEvidenceExportTooLarge
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/runs/"+id+"/export", nil)
	r.SetPathValue("id", id)
	server.HandleInventoryRefreshEvidenceExport(w, r)
	if w.Code != http.StatusRequestEntityTooLarge || f.refreshEvidenceCalls != 2 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("oversized export status=%d calls=%d headers=%v", w.Code, f.refreshEvidenceCalls, w.Header())
	}
	for _, method := range []string{http.MethodPost} {
		w = httptest.NewRecorder()
		r = httptest.NewRequest(method, "/api/cloud-inventory/refresh/runs/"+id+"/export", nil)
		r.SetPathValue("id", id)
		server.HandleInventoryRefreshEvidenceExport(w, r)
		if w.Code != http.StatusMethodNotAllowed || f.refreshEvidenceCalls != 2 {
			t.Fatalf("unexpected method status=%d calls=%d", w.Code, f.refreshEvidenceCalls)
		}
	}
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/api/cloud-inventory/refresh/runs/not-hex/export", nil)
	r.SetPathValue("id", "not-hex")
	server.HandleInventoryRefreshEvidenceExport(w, r)
	if w.Code != http.StatusNotFound || f.refreshEvidenceCalls != 2 {
		t.Fatalf("invalid ID status=%d calls=%d", w.Code, f.refreshEvidenceCalls)
	}
}

func TestEmptyDirectoryPreviewEndpointReturnsCandidatesAndScanEvidence(t *testing.T) {
	server := New(&fakeBackend{}, func() {})
	w := httptest.NewRecorder()
	server.HandleEmptyCloudDirectories(w, httptest.NewRequest("GET", "/api/cloud-cleanup/empty-directories", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"path":"/empty"`) || !strings.Contains(w.Body.String(), `"scan_run_id":""`) || !strings.Contains(w.Body.String(), `"status":"complete"`) {
		t.Fatalf("empty directory preview response %d %s", w.Code, w.Body.String())
	}
}

func TestCloudListingPreservesIDsAndBoundsPagination(t *testing.T) {
	server := New(&fakeBackend{}, func() {})
	w := httptest.NewRecorder()
	server.HandleCloud(w, httptest.NewRequest("GET", "/api/cloud?dir=/&offset=-1", nil))
	if w.Code != 400 {
		t.Fatal("negative pagination accepted")
	}
	w = httptest.NewRecorder()
	server.HandleCloud(w, httptest.NewRequest(http.MethodGet, "/api/cloud?dir=/", nil))
	var value struct {
		Entries []struct {
			ID string `json:"id"`
		}
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &value) != nil || value.Entries[0].ID != "3400000000000000123" {
		t.Fatal("cloud ID lost precision")
	}
}

func TestCloudListingReanchorsWhenPageBoundaryMoved(t *testing.T) {
	backend := &fakeBackend{cloudListings: map[int]downloader.CloudListing{
		100: {Directory: "/docs", Entries: []downloader.CloudEntry{{FsID: 202, Path: "/docs/shifted", Name: "shifted"}}, Offset: 100},
		0:   {Directory: "/docs", Entries: []downloader.CloudEntry{{FsID: 1, Path: "/docs/first", Name: "first"}}, Offset: 0},
	}}
	server := New(backend, func() {})
	encoded := `{"fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	w := httptest.NewRecorder()
	server.HandleCloud(w, httptest.NewRequest(http.MethodGet, "/api/cloud?dir=%2Fdocs&offset=100&expected_boundary="+url.QueryEscape(encoded), nil))
	var result downloader.CloudListing
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.BoundaryChanged || result.BoundaryChangeReason != "page_boundary_mismatch" || result.Offset != 0 || len(result.Entries) != 1 || result.Entries[0].Path != "/docs/first" {
		t.Fatalf("drifted page was not reanchored: status=%d body=%s", w.Code, w.Body.String())
	}
	if backend.cloudCalls != 2 || len(backend.cloudOffsets) != 2 || backend.cloudOffsets[0] != 100 || backend.cloudOffsets[1] != 0 {
		t.Fatalf("unexpected reanchor requests: calls=%d offsets=%v", backend.cloudCalls, backend.cloudOffsets)
	}
}

func TestCloudListingReanchorsAfterDuplicatePageIdentity(t *testing.T) {
	backend := &fakeBackend{
		cloudListings: map[int]downloader.CloudListing{
			0: {Directory: "/docs", Entries: []downloader.CloudEntry{{FsID: 1, Path: "/docs/first", Name: "first"}}, Offset: 0},
		},
		cloudErrorsByOffset: map[int]error{100: fmt.Errorf("%w: fs_id=101", baiduapi.ErrDuplicatePageItem)},
	}
	server := New(backend, func() {})
	encoded := `{"fingerprint":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}`
	w := httptest.NewRecorder()
	server.HandleCloud(w, httptest.NewRequest(http.MethodGet, "/api/cloud?dir=%2Fdocs&offset=100&expected_boundary="+url.QueryEscape(encoded), nil))
	var result downloader.CloudListing
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &result) != nil || !result.BoundaryChanged || result.BoundaryChangeReason != "provider_duplicate_page_id" || result.Offset != 0 || len(result.Entries) != 1 || result.Entries[0].Path != "/docs/first" {
		t.Fatalf("duplicate page was not reanchored: status=%d body=%s", w.Code, w.Body.String())
	}
	if backend.cloudCalls != 2 || len(backend.cloudOffsets) != 2 || backend.cloudOffsets[0] != 100 || backend.cloudOffsets[1] != 0 {
		t.Fatalf("unexpected reanchor requests: calls=%d offsets=%v", backend.cloudCalls, backend.cloudOffsets)
	}
}

func TestCloudListingDoesNotReanchorFirstPageOrOtherErrors(t *testing.T) {
	tests := []struct {
		name   string
		offset int
		err    error
	}{
		{name: "first-page-duplicate", offset: 0, err: baiduapi.ErrDuplicatePageItem},
		{name: "other-page-error", offset: 100, err: errors.New("provider unavailable")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &fakeBackend{cloudErrorsByOffset: map[int]error{test.offset: test.err}}
			server := New(backend, func() {})
			w := httptest.NewRecorder()
			server.HandleCloud(w, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/cloud?dir=%%2Fdocs&offset=%d", test.offset), nil))
			if w.Code != http.StatusBadGateway || backend.cloudCalls != 1 || len(backend.cloudOffsets) != 1 || backend.cloudOffsets[0] != test.offset {
				t.Fatalf("unexpected fallback for %s: status=%d calls=%d offsets=%v", test.name, w.Code, backend.cloudCalls, backend.cloudOffsets)
			}
		})
	}
}

func TestCloudListingBoundaryResponseDoesNotExposeLookaheadPath(t *testing.T) {
	boundary := downloader.CloudPageBoundary{FsID: 101, Path: "/docs/private-lookahead-name", Name: "private-lookahead-name", Size: 100, ProviderMtime: 1000, ContentMD5: "md5-100"}
	boundary.Fingerprint = boundary.ComputeFingerprint()
	backend := &fakeBackend{cloudListings: map[int]downloader.CloudListing{
		0: {Directory: "/docs", Entries: []downloader.CloudEntry{{FsID: 1, Path: "/docs/visible", Name: "visible"}}, NextBoundary: &boundary},
	}}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleCloud(w, httptest.NewRequest(http.MethodGet, "/api/cloud?dir=%2Fdocs", nil))
	if w.Code != http.StatusOK || strings.Contains(w.Body.String(), boundary.Path) || strings.Contains(w.Body.String(), boundary.Name) || !strings.Contains(w.Body.String(), boundary.Fingerprint) {
		t.Fatalf("lookahead response leaked its path or omitted fingerprint: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestReadOnlyCloudFailuresShowSafeRetryableDiagnostics(t *testing.T) {
	backend := &fakeBackend{cloudErr: &baiduapi.APIError{Operation: "list", Status: http.StatusTooManyRequests, Code: -9, RetryAfter: 4 * time.Second}}
	server := New(backend, func() {})
	w := httptest.NewRecorder()
	server.HandleCloud(w, httptest.NewRequest(http.MethodGet, "/api/cloud?dir=%2Fqueue", nil))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "HTTP=429") || !strings.Contains(w.Body.String(), "错误码=-9") || !strings.Contains(w.Body.String(), "可安全重试") || w.Header().Get("Retry-After") != "4" || !strings.Contains(w.Body.String(), "约 4 秒") || strings.Contains(w.Body.String(), "重发移动") {
		t.Fatalf("read-only cloud failure did not explain safe retry: status=%d body=%q", w.Code, w.Body.String())
	}

	backend.previewErr = errors.New("Get https://pan.baidu.com/?access_token=secret-value: dial tcp refused")
	w = httptest.NewRecorder()
	server.HandlePreview(w, httptest.NewRequest(http.MethodPost, "/api/intakes/preview", strings.NewReader(`{"paths":["/a"]}`)))
	if w.Code != http.StatusBadGateway || !strings.Contains(w.Body.String(), "可安全重试") || strings.Contains(w.Body.String(), "secret-value") || strings.Contains(w.Body.String(), "重发移动") {
		t.Fatalf("read-only preview failure leaked request details or used mutation warning: status=%d body=%q", w.Code, w.Body.String())
	}
}

type timeoutNetworkError struct{}

func (timeoutNetworkError) Error() string   { return "dial tcp timeout; token=secret-value" }
func (timeoutNetworkError) Timeout() bool   { return true }
func (timeoutNetworkError) Temporary() bool { return true }

func TestReadOnlyPreviewClassifiesNetworkFailuresWithoutLeakingDetails(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "DNS lookup",
			err:  &url.Error{Op: "Get", URL: "https://pan.baidu.com/rest?access_token=secret-value", Err: &net.DNSError{Err: "no such host", Name: "pan.baidu.com"}},
			want: "无法解析百度网盘域名",
		},
		{
			name: "network timeout",
			err:  &url.Error{Op: "Get", URL: "https://pan.baidu.com/rest?access_token=secret-value", Err: timeoutNetworkError{}},
			want: "连接百度网盘超时",
		},
		{
			name: "connection refused",
			err:  &url.Error{Op: "Get", URL: "https://pan.baidu.com/rest?access_token=secret-value", Err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}},
			want: "无法建立网盘连接",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := New(&fakeBackend{previewErr: test.err}, func() {})
			response := httptest.NewRecorder()
			server.HandlePreview(response, httptest.NewRequest(http.MethodPost, "/api/intakes/preview", strings.NewReader(`{"paths":["/a"]}`)))
			body := response.Body.String()
			if response.Code != http.StatusBadGateway || !strings.Contains(body, test.want) || !strings.Contains(body, "只读查询可安全重试") {
				t.Fatalf("network failure lacks actionable safe diagnostic: status=%d body=%q", response.Code, body)
			}
			if strings.Contains(body, "secret-value") || strings.Contains(body, "https://") || strings.Contains(body, "connection refused") {
				t.Fatalf("network diagnostic leaked provider request details: %q", body)
			}
		})
	}
}
