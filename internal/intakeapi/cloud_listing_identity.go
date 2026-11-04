package intakeapi

import (
	"errors"
	"net/url"
	"strconv"
	"web_browser_baidu_onekey_upload/internal/downloader"
	"web_browser_baidu_onekey_upload/internal/taskstore"
)

func cloudStringIdentity(query url.Values) (bool, error) {
	values := query["identity_format"]
	if len(values) > 1 || (len(values) == 1 && values[0] != "" && values[0] != "string") {
		return false, errors.New("invalid cloud identity format")
	}
	return query.Get("identity_format") == "string", nil
}
func cloudGenerationResponse(generation int64, exact bool) any {
	if exact {
		return strconv.FormatInt(generation, 10)
	}
	return generation
}

func cloudDirectorySizeResponse(size taskstore.CloudDirectorySize, exact bool) any {
	if !exact {
		return size
	}
	return struct {
		taskstore.CloudDirectorySize
		Bytes       string `json:"bytes"`
		Files       string `json:"files"`
		Directories string `json:"directories"`
	}{size, strconv.FormatInt(size.Bytes, 10), strconv.FormatInt(size.Files, 10), strconv.FormatInt(size.Directories, 10)}
}

type cachedCloudStringEntry struct {
	taskstore.CloudInventoryEntry
	FsID               string  `json:"fs_id"`
	Size               string  `json:"size"`
	DirectorySizeBytes *string `json:"directory_size_bytes,omitempty"`
	ProviderMtime      string  `json:"provider_mtime"`
}

func cachedCloudEntriesResponse(entries []taskstore.CloudInventoryEntry, exact bool) any {
	if !exact {
		return entries
	}
	out := make([]cachedCloudStringEntry, 0, len(entries))
	for _, entry := range entries {
		var size *string
		if entry.DirectorySizeBytes != nil {
			value := strconv.FormatInt(*entry.DirectorySizeBytes, 10)
			size = &value
		}
		out = append(out, cachedCloudStringEntry{entry, strconv.FormatInt(entry.FsID, 10), strconv.FormatInt(entry.Size, 10), size, strconv.FormatInt(entry.ProviderMtime, 10)})
	}
	return out
}

type cloudStringEntry struct {
	downloader.CloudEntry
	FsID         string `json:"fs_id"`
	Size         string `json:"size"`
	ProviderSize string `json:"provider_size"`
	Mtime        string `json:"server_mtime"`
}

func cloudEntriesResponse(entries []downloader.CloudEntry, exact bool) any {
	if !exact {
		return entries
	}
	out := make([]cloudStringEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, cloudStringEntry{entry, strconv.FormatInt(entry.FsID, 10), strconv.FormatInt(entry.Size, 10), strconv.FormatInt(entry.ProviderSize, 10), strconv.FormatInt(entry.Mtime, 10)})
	}
	return out
}
func cloudListingResponse(listing downloader.CloudListing, exact bool) any {
	if !exact {
		return listing
	}
	return struct {
		downloader.CloudListing
		Entries any `json:"entries"`
	}{listing, cloudEntriesResponse(listing.Entries, true)}
}
