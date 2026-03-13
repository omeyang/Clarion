package schema

import (
	"net/http"
	"net/url"
	"testing"
)

// FuzzPagination 验证分页参数解析在任意输入下不会 panic，
// 且输出始终满足约束：offset >= 0，1 <= limit <= 100。
func FuzzPagination(f *testing.F) {
	f.Add("", "")
	f.Add("0", "20")
	f.Add("-1", "-5")
	f.Add("abc", "xyz")
	f.Add("999999999999999999999", "100")
	f.Add("0", "0")
	f.Add("0", "101")
	f.Add("0", "200")
	f.Add("-100", "50")

	f.Fuzz(func(t *testing.T, offsetStr, limitStr string) {
		q := url.Values{}
		if offsetStr != "" {
			q.Set("offset", offsetStr)
		}
		if limitStr != "" {
			q.Set("limit", limitStr)
		}
		r := &http.Request{URL: &url.URL{RawQuery: q.Encode()}}

		offset, limit := Pagination(r)

		if offset < 0 {
			t.Errorf("offset 应 >= 0，实际 %d（输入 offset=%q）", offset, offsetStr)
		}
		if limit <= 0 || limit > 100 {
			t.Errorf("limit 应在 [1, 100]，实际 %d（输入 limit=%q）", limit, limitStr)
		}
	})
}
