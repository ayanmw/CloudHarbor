package foldersync

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"
	"web_browser_baidu_onekey_upload/internal/directoryanalysis"
)

type CachedAnalysisSource struct {
	Scope      string
	Generation string
	ReadAt     time.Time
	Inputs     []directoryanalysis.Input
}
type cachedAnalysisReader interface {
	CachedAnalysis(context.Context, []string) (CachedAnalysisSource, error)
}

func (m *Manager) AnalyzeCachedDirectories(ctx context.Context, left, right string) (Analysis, error) {
	for _, root := range []string{left, right} {
		if root == "" || !strings.HasPrefix(root, "/") || path.Clean(root) != root || strings.ContainsAny(root, "\\\x00") || len(root) > 4096 {
			return Analysis{}, fmt.Errorf("网盘目录路径无效")
		}
	}
	if left == right || left == "/" || right == "/" || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/") {
		return Analysis{}, fmt.Errorf("请选择互不重叠的两个网盘目录")
	}
	reader, ok := m.remote.(cachedAnalysisReader)
	if !ok {
		return Analysis{}, fmt.Errorf("缓存比较来源不可用")
	}
	source, err := reader.CachedAnalysis(ctx, []string{left, right})
	if err != nil {
		return Analysis{}, err
	}
	generation, err := strconv.ParseInt(source.Generation, 10, 64)
	if err != nil || generation < 0 || source.Scope == "" || source.ReadAt.IsZero() || len(source.Inputs) != 2 || source.Inputs[0].Root != left || source.Inputs[1].Root != right {
		return Analysis{}, fmt.Errorf("缓存来源范围或代次证据无效")
	}
	report, err := directoryanalysis.Compare(source.Inputs[0], source.Inputs[1])
	if err != nil {
		return Analysis{}, err
	}
	header := Analysis{ID: newID(), SourceKind: "cloud_cache", CacheGeneration: source.Generation, SourceScope: source.Scope, SourceRecorded: source.ReadAt.UTC(), Created: time.Now().UTC(), LeftRoot: left, RightRoot: right, Historical: true, Summary: report, RowCount: strconv.Itoa(len(report.Rows)), GroupCount: strconv.Itoa(len(report.Groups))}
	return m.saveAnalysis(ctx, header, report)
}
