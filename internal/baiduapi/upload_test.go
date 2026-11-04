package baiduapi

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestUploadUsesAllBlockHashesAndDoesNotRetryAnUnknownCommit(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	calls := []string{}
	file := t.TempDir() + "/file"
	os.WriteFile(file, []byte("hello"), 0600)
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		method := r.URL.Query().Get("method")
		calls = append(calls, method)
		body := `{"errno":0,"uploadid":"fixture","return_type":1,"block_list":[0]}`
		if method == "upload" {
			body = `{"errno":0,"md5":"5d41402abc4b2a76b9719d911017c592"}`
		} else if method == "create" {
			r.ParseForm()
			if r.Form.Get("block_list") != `["5d41402abc4b2a76b9719d911017c592"]` || r.Form.Get("rtype") != "1" {
				t.Fatal("invalid create scope")
			}
			return nil, io.ErrUnexpectedEOF
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	_, err := NewClient("token", "").UploadLocalFile(context.Background(), file, "/test.txt", func(string, int64) error { return nil })
	if !errors.Is(err, ErrUploadUnconfirmed) || len(calls) != 3 {
		t.Fatalf("err=%v calls=%v", err, calls)
	}
}

func TestUploadMergeRetainsHashesOfServerExistingBlocks(t *testing.T) {
	original := http.DefaultTransport
	defer func() { http.DefaultTransport = original }()
	payload := append(bytes.Repeat([]byte("a"), UploadBlockSize), byte('b'))
	file := t.TempDir() + "/file"
	if err := os.WriteFile(file, payload, 0600); err != nil {
		t.Fatal(err)
	}
	first := md5.Sum(payload[:UploadBlockSize])
	second := md5.Sum(payload[UploadBlockSize:])
	expected := fmt.Sprintf(`["%x","%x"]`, first, second)
	finalizing := false
	http.DefaultTransport = apiTransport(func(r *http.Request) (*http.Response, error) {
		body := `{"errno":0,"uploadid":"fixture","return_type":1,"block_list":[1]}`
		switch r.URL.Query().Get("method") {
		case "upload":
			if r.URL.Query().Get("partseq") != "1" {
				t.Fatal("wrong block")
			}
			body = fmt.Sprintf(`{"errno":0,"md5":"%x"}`, second)
		case "create":
			r.ParseForm()
			if !finalizing || r.Form.Get("block_list") != expected {
				t.Fatalf("commit omitted existing block: %s", r.Form.Get("block_list"))
			}
			body = `{"errno":0,"fs_id":123,"path":"/file(1).txt","size":4194305}`
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})
	receipt, err := NewClient("fixture", "").UploadLocalFile(context.Background(), file, "/file.txt", func(phase string, _ int64) error {
		if phase == "finalizing" {
			finalizing = true
		}
		return nil
	})
	if err != nil || receipt.Path != "/file(1).txt" || receipt.Size != "4194305" {
		t.Fatalf("receipt=%+v err=%v", receipt, err)
	}
}
