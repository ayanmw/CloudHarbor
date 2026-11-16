package downloader

import (
	"context"
	"encoding/hex"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
)

// Preview observations never authorize writes; MutateCloud still rechecks each item.
type CloudBatchPreviewRequest struct {
	RequestID            string              `json:"request_id"`
	Phase                string              `json:"phase"`
	Operation            string              `json:"operation"`
	DestinationDirectory string              `json:"destination_directory"`
	Expected             []baiduapi.FileInfo `json:"expected"`
	Offset               int                 `json:"offset"`
	ExpectedBoundary     string              `json:"expected_boundary"`
}
type CloudBatchPreviewRow struct {
	SourceID      string `json:"source_id"`
	SourcePath    string `json:"source_path"`
	CurrentPath   string `json:"current_path"`
	Status        string `json:"status"`
	ObservedAtUTC string `json:"observed_at_utc"`
}
type CloudBatchPreviewMatch struct {
	SourceID        string `json:"source_id"`
	DestinationPath string `json:"destination_path"`
	ConflictID      string `json:"conflict_id"`
	ConflictPath    string `json:"conflict_path"`
}
type CloudBatchPreviewNameConflict struct {
	SourceID        string `json:"source_id"`
	OtherSourceID   string `json:"other_source_id"`
	DestinationPath string `json:"destination_path"`
}
type CloudBatchPreviewResult struct {
	RequestID      string                          `json:"request_id"`
	Phase          string                          `json:"phase"`
	Operation      string                          `json:"operation"`
	Directory      string                          `json:"directory"`
	Offset         int                             `json:"offset"`
	SelectionIDs   []string                        `json:"selection_ids"`
	StartedAtUTC   string                          `json:"started_at_utc"`
	ObservedAtUTC  string                          `json:"observed_at_utc"`
	CheckedCount   int                             `json:"checked_count"`
	Rows           []CloudBatchPreviewRow          `json:"rows"`
	Matches        []CloudBatchPreviewMatch        `json:"matches"`
	BatchConflicts []CloudBatchPreviewNameConflict `json:"batch_conflicts"`
	NextOffset     *int                            `json:"next_offset"`
	NextBoundary   string                          `json:"next_boundary"`
}

var previewRequestID = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

// ValidateCloudBatchPreview checks the bounded read contract before any provider request.
func ValidateCloudBatchPreview(r CloudBatchPreviewRequest) error {
	limit := 100
	if r.Phase == "sources" {
		limit = 10
		if r.Offset != 0 || r.ExpectedBoundary != "" {
			return ErrIntakeInvalid
		}
	} else if r.Phase != "destination" {
		return ErrIntakeInvalid
	}
	if !previewRequestID.MatchString(r.RequestID) || (r.Operation != "move" && r.Operation != "copy") || len(r.Expected) < 1 || len(r.Expected) > limit || r.Offset < 0 || r.Offset > 1000000 {
		return ErrIntakeInvalid
	}
	if r.Offset > 0 {
		if len(r.ExpectedBoundary) != 64 {
			return ErrIntakeInvalid
		}
		if _, err := hex.DecodeString(r.ExpectedBoundary); err != nil {
			return ErrIntakeInvalid
		}
	} else if r.ExpectedBoundary != "" {
		return ErrIntakeInvalid
	}
	seen := make(map[int64]bool, len(r.Expected))
	for _, expected := range r.Expected {
		if seen[expected.FsID] || !validListingFile(path.Dir(expected.Path), expected) || expected.Mtime < 0 || !validCloudPath(expected.Path) || expected.Path == "/" {
			return ErrIntakeInvalid
		}
		seen[expected.FsID] = true
		target, err := validateCloudMutation(CloudMutationRequest{Operation: r.Operation, Expected: expected, DestinationDirectory: r.DestinationDirectory})
		if err != nil || target == expected.Path || expected.IsDir == 1 && insideCloud(r.DestinationDirectory, expected.Path) {
			return ErrIntakeInvalid
		}
	}
	return nil
}
func previewBoundary(file baiduapi.FileInfo) string {
	return (CloudPageBoundary{FsID: file.FsID, Path: file.Path, Name: file.ServerFilename, Directory: file.IsDir == 1, Size: file.Size, ProviderMtime: file.Mtime, ContentMD5: file.MD5}).ComputeFingerprint()
}
func (d *Downloader) PreviewCloudBatch(ctx context.Context, request CloudBatchPreviewRequest) (CloudBatchPreviewResult, error) {
	if err := ValidateCloudBatchPreview(request); err != nil {
		return CloudBatchPreviewResult{}, err
	}
	ctx, done, err := d.startIntake(ctx)
	if err != nil {
		return CloudBatchPreviewResult{}, err
	}
	defer done()
	if err = ctx.Err(); err != nil {
		return CloudBatchPreviewResult{}, err
	}
	d.cloudMutex.RLock()
	defer d.cloudMutex.RUnlock()
	for _, item := range request.Expected {
		if item.IsDir == 1 && insideCloud(d.QueueDirectory(), item.Path) {
			return CloudBatchPreviewResult{}, ErrIntakeInvalid
		}
	}
	client, epoch, err := d.authorizedClient(ctx)
	if err != nil {
		return CloudBatchPreviewResult{}, err
	}
	result := CloudBatchPreviewResult{RequestID: request.RequestID, Phase: request.Phase, Operation: request.Operation, Directory: request.DestinationDirectory, Offset: request.Offset, StartedAtUTC: time.Now().UTC().Format(time.RFC3339Nano), SelectionIDs: []string{}, Rows: []CloudBatchPreviewRow{}, Matches: []CloudBatchPreviewMatch{}, BatchConflicts: []CloudBatchPreviewNameConflict{}}
	for _, item := range request.Expected {
		result.SelectionIDs = append(result.SelectionIDs, strconv.FormatInt(item.FsID, 10))
	}
	if request.Phase == "sources" {
		for _, expected := range request.Expected {
			if err = ctx.Err(); err != nil {
				return CloudBatchPreviewResult{}, err
			}
			current, readErr := client.GetFileInfo(expected.FsID)
			if readErr != nil {
				d.reportAuthenticationFailure(readErr)
				return CloudBatchPreviewResult{}, readErr
			}
			if !validListingFile(path.Dir(current.Path), current) {
				return CloudBatchPreviewResult{}, ErrIntakeConflict
			}
			status := "changed"
			if sameCloudIdentity(expected, current) {
				status = "matched"
			}
			result.Rows = append(result.Rows, CloudBatchPreviewRow{SourceID: strconv.FormatInt(expected.FsID, 10), SourcePath: expected.Path, CurrentPath: current.Path, Status: status, ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)})
			result.CheckedCount++
		}
	} else {
		files, more, boundary, readErr := client.ListDirPageOrdered(request.DestinationDirectory, request.Offset, 100, baiduapi.DirectoryOrder{Field: "name"})
		if readErr != nil {
			d.reportAuthenticationFailure(readErr)
			return CloudBatchPreviewResult{}, readErr
		}
		for _, file := range files {
			if !validListingFile(request.DestinationDirectory, file) {
				return CloudBatchPreviewResult{}, ErrIntakeConflict
			}
		}
		if request.Offset > 0 && (len(files) == 0 || !strings.EqualFold(previewBoundary(files[0]), request.ExpectedBoundary)) {
			return CloudBatchPreviewResult{}, ErrIntakeStale
		}
		if more {
			if len(files) != 100 || boundary == nil || !validListingFile(request.DestinationDirectory, *boundary) {
				return CloudBatchPreviewResult{}, ErrIntakeConflict
			}
			next := request.Offset + len(files)
			if next > 1000000 {
				return CloudBatchPreviewResult{}, ErrIntakeConflict
			}
			result.NextOffset = &next
			result.NextBoundary = previewBoundary(*boundary)
		}
		for i, expected := range request.Expected {
			id := strconv.FormatInt(expected.FsID, 10)
			// One representative peer per item bounds duplicate-name output to n-1.
			for _, other := range request.Expected[:i] {
				if strings.EqualFold(expected.ServerFilename, other.ServerFilename) {
					result.BatchConflicts = append(result.BatchConflicts, CloudBatchPreviewNameConflict{SourceID: id, OtherSourceID: strconv.FormatInt(other.FsID, 10), DestinationPath: path.Join(request.DestinationDirectory, expected.ServerFilename)})
					break
				}
			}
			for _, file := range files {
				if strings.EqualFold(expected.ServerFilename, file.ServerFilename) {
					result.Matches = append(result.Matches, CloudBatchPreviewMatch{SourceID: id, DestinationPath: path.Join(request.DestinationDirectory, expected.ServerFilename), ConflictID: strconv.FormatInt(file.FsID, 10), ConflictPath: file.Path})
					// A conflict example proves the name is occupied; this is not an inventory of every variant.
					break
				}
			}
		}
		result.CheckedCount = len(files)
	}
	if err = ctx.Err(); err != nil {
		return CloudBatchPreviewResult{}, err
	}
	if _, err = d.intakeClient(ctx, epoch); err != nil {
		return CloudBatchPreviewResult{}, err
	}
	result.ObservedAtUTC = time.Now().UTC().Format(time.RFC3339Nano)
	return result, nil
}
