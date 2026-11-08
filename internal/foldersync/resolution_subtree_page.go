package foldersync

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// Project in SQLite so normal history reads never expand all descendant rows
// into Go objects. The original immutable JSON remains the export evidence.
const resolutionSummarySQL = `CASE WHEN json_type(body,'$.subtree_review.items')='array' THEN json_set(body,'$.subtree_review.item_count',CAST(json_array_length(body,'$.subtree_review.items') AS TEXT),'$.subtree_review.paged',json('true'),'$.subtree_review.items',json('[]')) ELSE body END`

func resolutionSummary(r Resolution) Resolution {
	if r.SubtreeReview != nil && !r.SubtreeReview.Paged {
		review := *r.SubtreeReview
		review.ItemCount = strconv.Itoa(len(review.Items))
		review.Items = []SubtreeImpact{}
		review.Paged = true
		r.SubtreeReview = &review
	}
	return r
}
func (m *Manager) ResolutionSummary(ctx context.Context, id string) (Resolution, error) {
	var r Resolution
	var body string
	err := m.db.QueryRowContext(ctx, `SELECT `+resolutionSummarySQL+` FROM sync_resolutions WHERE id=?`, id).Scan(&body)
	if err != nil {
		return r, err
	}
	err = json.Unmarshal([]byte(body), &r)
	return r, err
}

type SubtreePage struct {
	DecisionID string          `json:"decision_id"`
	Root       string          `json:"root"`
	Total      string          `json:"total"`
	Offset     int             `json:"offset"`
	Limit      int             `json:"limit"`
	More       bool            `json:"has_more"`
	Items      []SubtreeImpact `json:"items"`
}

func (m *Manager) ResolutionSubtreePage(ctx context.Context, id string, offset int) (SubtreePage, error) {
	page := SubtreePage{DecisionID: id, Offset: offset, Limit: 100, Items: []SubtreeImpact{}}
	if offset < 0 {
		return page, fmt.Errorf("子项分页无效")
	}
	header, err := m.ResolutionSummary(ctx, id)
	if err != nil {
		return page, err
	}
	if header.SubtreeReview == nil {
		return page, sql.ErrNoRows
	}
	page.Root = header.SubtreeReview.Root
	page.Total = header.SubtreeReview.ItemCount
	if _, err = strconv.Atoi(page.Total); err != nil {
		return page, fmt.Errorf("子项总数证据无效")
	}
	rows, err := m.db.QueryContext(ctx, `SELECT j.value FROM sync_resolutions d,json_each(d.body,'$.subtree_review.items') j WHERE d.id=? ORDER BY CAST(j.key AS INTEGER) LIMIT 101 OFFSET ?`, id, offset)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	for rows.Next() {
		var body string
		var item SubtreeImpact
		if err = rows.Scan(&body); err != nil {
			return page, err
		}
		if err = json.Unmarshal([]byte(body), &item); err != nil {
			return page, err
		}
		page.Items = append(page.Items, item)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	page.More = len(page.Items) > 100
	if page.More {
		page.Items = page.Items[:100]
	}
	return page, nil
}
func (m *Manager) HandleResolutionSubtree(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.WriteHeader(405)
		return
	}
	query := r.URL.Query()
	offset := 0
	var err error
	if text := query.Get("offset"); text != "" {
		offset, err = strconv.Atoi(text)
	}
	if err != nil || offset < 0 || len(query["offset"]) > 1 {
		http.Error(w, "子项分页无效", 400)
		return
	}
	page, err := m.ResolutionSubtreePage(r.Context(), r.PathValue("decision"), offset)
	if err != nil {
		if err == sql.ErrNoRows {
			http.NotFound(w, r)
		} else {
			http.Error(w, "固定子项清单读取失败", 500)
		}
		return
	}
	reply(w, page)
}
