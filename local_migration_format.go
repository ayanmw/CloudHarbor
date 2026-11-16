package main

import (
	"strconv"
	"github.com/ayanmw/CloudHarbor/internal/taskstore"
)

func localMigrationResponse(m taskstore.LocalDirectoryMigration, exact bool) any {
	if !exact {
		return m
	}
	return struct {
		taskstore.LocalDirectoryMigration
		TotalFiles     string `json:"total_files"`
		ProcessedFiles string `json:"processed_files"`
		TotalBytes     string `json:"total_bytes"`
		ProcessedBytes string `json:"processed_bytes"`
	}{m, strconv.FormatInt(m.TotalFiles, 10), strconv.FormatInt(m.ProcessedFiles, 10), strconv.FormatInt(m.TotalBytes, 10), strconv.FormatInt(m.ProcessedBytes, 10)}
}
