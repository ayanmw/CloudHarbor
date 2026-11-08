package main

import (
	"encoding/json"
	"net/http"
	"runtime"
	"time"
)

// Injected together by package.ps1; empty values identify an unrecorded development build.
var builtAtUTC = ""
var sourceSnapshot = ""

type buildInformation struct {
	Version        string `json:"version"`
	Revision       string `json:"revision"`
	SourceSnapshot string `json:"source_snapshot_sha256"`
	BuiltAtUTC     string `json:"built_at_utc"`
	Runtime        string `json:"runtime"`
	GoVersion      string `json:"go_version"`
	ObservedAtUTC  string `json:"observed_at_utc"`
}

func currentBuildInformation() buildInformation {
	return buildInformation{Version: version, Revision: revision, SourceSnapshot: sourceSnapshot, BuiltAtUTC: builtAtUTC, Runtime: runtime.GOOS + "/" + runtime.GOARCH, GoVersion: runtime.Version(), ObservedAtUTC: time.Now().UTC().Format(time.RFC3339Nano)}
}
func handleBuildInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(currentBuildInformation())
}
