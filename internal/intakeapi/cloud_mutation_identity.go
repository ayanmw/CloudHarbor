package intakeapi

import (
	"fmt"
	"net/http"
	"strconv"
	"github.com/ayanmw/CloudHarbor/internal/baiduapi"
	"github.com/ayanmw/CloudHarbor/internal/downloader"
)

type cloudStringExpectedFile struct {
	baiduapi.FileInfo
	FsID  string `json:"fs_id"`
	Size  string `json:"size"`
	Mtime string `json:"server_mtime"`
}

func parseCloudStringExpected(expected *cloudStringExpectedFile) (baiduapi.FileInfo, error) {
	value := expected.FileInfo
	for index, raw := range []string{expected.FsID, expected.Size, expected.Mtime} {
		number, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || number < 0 || strconv.FormatInt(number, 10) != raw {
			return value, fmt.Errorf("身份数字格式无效")
		}
		switch index {
		case 0:
			value.FsID = number
		case 1:
			value.Size = number
		case 2:
			value.Mtime = number
		}
	}
	return value, nil
}
func decodeCloudMutation(w http.ResponseWriter, r *http.Request, exact bool) (downloader.CloudMutationRequest, bool) {
	var request downloader.CloudMutationRequest
	if !exact {
		ok := decode(w, r, &request, false)
		return request, ok
	}
	var wire struct {
		downloader.CloudMutationRequest
		Expected *cloudStringExpectedFile `json:"expected"`
		Retained *cloudStringExpectedFile `json:"expected_retained"`
	}
	if !decode(w, r, &wire, false) {
		return request, false
	}
	request = wire.CloudMutationRequest
	if wire.Expected != nil {
		value, err := parseCloudStringExpected(wire.Expected)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return request, false
		}
		request.Expected = value
	}
	if wire.Retained != nil {
		value, err := parseCloudStringExpected(wire.Retained)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return request, false
		}
		request.ExpectedRetained = &value
	}
	return request, true
}

func cloudMutationResponse(result downloader.CloudMutationResult, exact bool) any {
	if !exact {
		return result
	}
	return struct {
		downloader.CloudMutationResult
		ObjectID string `json:"object_id"`
	}{result, strconv.FormatInt(result.ObjectID, 10)}
}
