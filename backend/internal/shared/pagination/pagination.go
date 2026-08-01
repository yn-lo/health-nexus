// Package pagination 提供分页参数解析和响应封装。
package pagination

import (
	"net/http"
	"strconv"

	apperrors "health-nexus/internal/shared/errors"
)

const (
	defaultPage     = 1
	defaultPageSize = 20
	maxPageSize     = 100
)

// Params 分页参数。
type Params struct {
	Page     int
	PageSize int
}

// Parse 从 query 参数解析分页。
func Parse(r *http.Request) (Params, error) {
	page, err := parsePositiveInt(r, "page", defaultPage)
	if err != nil {
		return Params{}, err
	}
	pageSize, err := parsePositiveInt(r, "page_size", defaultPageSize)
	if err != nil {
		return Params{}, err
	}
	if pageSize > maxPageSize {
		pageSize = maxPageSize
	}
	return Params{Page: page, PageSize: pageSize}, nil
}

// Offset 计算数据库 OFFSET。
func (p Params) Offset() int { return (p.Page - 1) * p.PageSize }

// Result 分页响应结构。
type Result[T any] struct {
	Items    []T   `json:"items"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// NewResult 构造分页响应。
func NewResult[T any](items []T, total int64, p Params) Result[T] {
	if items == nil {
		items = []T{}
	}
	return Result[T]{Items: items, Total: total, Page: p.Page, PageSize: p.PageSize}
}

func parsePositiveInt(r *http.Request, key string, def int) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, apperrors.Validation("VALIDATION_INVALID_PAGE", key+" 参数无效")
	}
	return n, nil
}
