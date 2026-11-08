package intakeapi

import (
	"context"
	"fmt"
	"net/url"

	"web_browser_baidu_onekey_upload/internal/baiduapi"
	"web_browser_baidu_onekey_upload/internal/downloader"
)

func cloudOrder(query url.Values) (baiduapi.DirectoryOrder, error) {
	order := baiduapi.DirectoryOrder{Field: "name"}
	for _, key := range []string{"order", "desc"} {
		if len(query[key]) > 1 {
			return order, fmt.Errorf("duplicate directory order")
		}
	}
	if value, ok := query["order"]; ok {
		order.Field = value[0]
	}
	if value, ok := query["desc"]; ok {
		if value[0] != "0" && value[0] != "1" {
			return order, fmt.Errorf("invalid directory direction")
		}
		order.Descending = value[0] == "1"
	}
	if !order.Valid() {
		return order, fmt.Errorf("invalid directory order")
	}
	return order, nil
}

func (s *Server) listCloudOrdered(ctx context.Context, dir string, offset, limit int, order baiduapi.DirectoryOrder) (downloader.CloudListing, error) {
	if backend, ok := s.backend.(interface {
		ListCloudOrdered(context.Context, string, int, int, baiduapi.DirectoryOrder) (downloader.CloudListing, error)
	}); ok {
		return backend.ListCloudOrdered(ctx, dir, offset, limit, order)
	}
	if order.Field != "name" || order.Descending {
		return downloader.CloudListing{}, downloader.ErrIntakeInvalid
	}
	return s.backend.ListCloud(ctx, dir, offset, limit)
}
