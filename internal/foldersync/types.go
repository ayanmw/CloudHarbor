package foldersync

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"
)

type runContextKey struct{}

func WithRunID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, runContextKey{}, id)
}
func ContextRunID(ctx context.Context) string {
	id, _ := ctx.Value(runContextKey{}).(string)
	return id
}

// NotStartedError proves no visible namespace change was attempted by this step.
// Composite remote operations must stop using it after their first side effect.
type NotStartedError struct{ Err error }

func (e *NotStartedError) Error() string { return e.Err.Error() }
func (e *NotStartedError) Unwrap() error { return e.Err }

type Entry struct {
	Path      string    `json:"path"`
	ID        string    `json:"id"`
	Directory bool      `json:"directory"`
	Size      int64     `json:"size"`
	Mtime     int64     `json:"mtime"`
	MtimeUnit string    `json:"mtime_unit"`
	SHA256    string    `json:"sha256,omitempty"`
	MD5       string    `json:"provider_md5,omitempty"`
	Observed  time.Time `json:"observed_at_utc"`
}

// API and evidence integers remain exact in browsers. Accept the early local
// prototype's numeric representation on read; all new writes use decimal text.
type entryJSONAlias Entry

func (e Entry) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		*entryJSONAlias
		Size  string `json:"size"`
		Mtime string `json:"mtime"`
	}{(*entryJSONAlias)(&e), strconv.FormatInt(e.Size, 10), strconv.FormatInt(e.Mtime, 10)})
}
func (e *Entry) UnmarshalJSON(data []byte) error {
	var wire struct {
		entryJSONAlias
		Size  json.RawMessage `json:"size"`
		Mtime json.RawMessage `json:"mtime"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	parse := func(raw json.RawMessage) (int64, error) {
		if len(raw) == 0 {
			return 0, nil
		}
		text := string(raw)
		if raw[0] == '"' {
			if err := json.Unmarshal(raw, &text); err != nil {
				return 0, err
			}
		}
		value, err := strconv.ParseInt(text, 10, 64)
		if err != nil || strconv.FormatInt(value, 10) != text {
			return 0, fmt.Errorf("同步整数证据无效")
		}
		return value, nil
	}
	var err error
	value := Entry(wire.entryJSONAlias)
	value.Size, err = parse(wire.Size)
	if err != nil || value.Size < 0 {
		return fmt.Errorf("同步大小证据无效")
	}
	value.Mtime, err = parse(wire.Mtime)
	if err != nil {
		return err
	}
	*e = value
	return nil
}

type Snapshot struct {
	Local  []Entry `json:"local"`
	Remote []Entry `json:"remote"`
}
type Step struct {
	Action string `json:"action"`
	Path   string `json:"path"`
	From   string `json:"from,omitempty"`
	Local  *Entry `json:"local,omitempty"`
	Remote *Entry `json:"remote,omitempty"`
	Reason string `json:"reason,omitempty"`
}
