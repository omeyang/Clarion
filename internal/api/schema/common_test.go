package schema

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		wantOffset int
		wantLimit  int
	}{
		{
			name:       "默认值",
			query:      "",
			wantOffset: 0,
			wantLimit:  20,
		},
		{
			name:       "自定义值",
			query:      "offset=10&limit=50",
			wantOffset: 10,
			wantLimit:  50,
		},
		{
			name:       "limit为零时使用默认值",
			query:      "offset=5&limit=0",
			wantOffset: 5,
			wantLimit:  20,
		},
		{
			name:       "负数limit使用默认值",
			query:      "limit=-1",
			wantOffset: 0,
			wantLimit:  20,
		},
		{
			name:       "limit超过上限截断为100",
			query:      "limit=200",
			wantOffset: 0,
			wantLimit:  100,
		},
		{
			name:       "负数offset截断为0",
			query:      "offset=-5&limit=10",
			wantOffset: 0,
			wantLimit:  10,
		},
		{
			name:       "非法offset使用默认值",
			query:      "offset=abc&limit=10",
			wantOffset: 0,
			wantLimit:  10,
		},
		{
			name:       "非法limit使用默认值",
			query:      "offset=5&limit=xyz",
			wantOffset: 5,
			wantLimit:  20,
		},
		{
			name:       "limit恰好为100",
			query:      "limit=100",
			wantOffset: 0,
			wantLimit:  100,
		},
		{
			name:       "limit恰好为1",
			query:      "limit=1",
			wantOffset: 0,
			wantLimit:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			url := "/test"
			if tt.query != "" {
				url += "?" + tt.query
			}
			r := httptest.NewRequest(http.MethodGet, url, nil)

			offset, limit := Pagination(r)
			assert.Equal(t, tt.wantOffset, offset)
			assert.Equal(t, tt.wantLimit, limit)
		})
	}
}

func TestQueryInt(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		query      string
		key        string
		defaultVal int
		want       int
	}{
		{
			name:       "参数存在且合法",
			query:      "page=5",
			key:        "page",
			defaultVal: 1,
			want:       5,
		},
		{
			name:       "参数不存在返回默认值",
			query:      "",
			key:        "page",
			defaultVal: 1,
			want:       1,
		},
		{
			name:       "参数值非法返回默认值",
			query:      "page=abc",
			key:        "page",
			defaultVal: 1,
			want:       1,
		},
		{
			name:       "参数值为空字符串返回默认值",
			query:      "page=",
			key:        "page",
			defaultVal: 1,
			want:       1,
		},
		{
			name:       "参数值为负数",
			query:      "page=-3",
			key:        "page",
			defaultVal: 1,
			want:       -3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			url := "/test"
			if tt.query != "" {
				url += "?" + tt.query
			}
			r := httptest.NewRequest(http.MethodGet, url, nil)

			got := queryInt(r, tt.key, tt.defaultVal)
			assert.Equal(t, tt.want, got)
		})
	}
}
