// Package schema 定义 API 请求/响应类型。
package schema

import (
	"net/http"
	"strconv"
)

// ListResponse 封装分页列表结果。
type ListResponse[T any] struct {
	Items  []T `json:"items"`
	Total  int `json:"total"`
	Offset int `json:"offset"`
	Limit  int `json:"limit"`
}

// ErrorResponse 表示 API 错误响应。
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// Pagination 从查询参数中提取 offset 和 limit，并应用默认值。
func Pagination(r *http.Request) (offset, limit int) {
	offset = queryInt(r, "offset", 0)
	limit = queryInt(r, "limit", 20)
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	return offset, limit
}

func queryInt(r *http.Request, key string, defaultVal int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return defaultVal
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return v
}
