package engine

import (
	"testing"
)

// FuzzMissingFields 验证 MissingFields 在任意字段名组合下不会 panic，
// 且结果一致性：返回的缺失字段必须不在 collected 中。
func FuzzMissingFields(f *testing.F) {
	f.Add("name,phone", "name")
	f.Add("", "")
	f.Add("a,b,c", "a,b,c")
	f.Add("company", "")
	f.Add("x", "x,y,z")

	f.Fuzz(func(t *testing.T, requiredCSV, collectedCSV string) {
		required := splitNonEmpty(requiredCSV)
		collectedKeys := splitNonEmpty(collectedCSV)

		collected := make(map[string]string, len(collectedKeys))
		for _, k := range collectedKeys {
			collected[k] = "v"
		}

		ctx := &DialogueContext{
			RequiredFields:  required,
			CollectedFields: collected,
		}

		missing := ctx.MissingFields()
		hasAll := ctx.HasAllRequiredFields()

		// 一致性检查：missing 为空时 HasAllRequiredFields 应为 true。
		if (len(missing) == 0) != hasAll {
			t.Errorf("MissingFields 长度 %d 与 HasAllRequiredFields=%v 不一致", len(missing), hasAll)
		}

		// 返回的每个缺失字段都不应在 collected 中。
		for _, m := range missing {
			if _, ok := collected[m]; ok {
				t.Errorf("字段 %q 已收集但仍在 missing 列表中", m)
			}
		}
	})
}

// splitNonEmpty 按逗号分割并过滤空字符串。
func splitNonEmpty(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	start := 0
	for i := range len(s) {
		if s[i] == ',' {
			if start < i {
				result = append(result, s[start:i])
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}
