package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDialogueContext_MissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		requiredFields  []string
		collectedFields map[string]string
		want            []string
	}{
		{
			name:            "全部缺失",
			requiredFields:  []string{"name", "phone", "address"},
			collectedFields: map[string]string{},
			want:            []string{"name", "phone", "address"},
		},
		{
			name:           "部分收集",
			requiredFields: []string{"name", "phone", "address"},
			collectedFields: map[string]string{
				"name": "张三",
			},
			want: []string{"phone", "address"},
		},
		{
			name:           "全部已收集",
			requiredFields: []string{"name", "phone"},
			collectedFields: map[string]string{
				"name":  "张三",
				"phone": "13800138000",
			},
			want: nil,
		},
		{
			name:            "无必填字段",
			requiredFields:  nil,
			collectedFields: map[string]string{},
			want:            nil,
		},
		{
			name:            "collectedFields为nil",
			requiredFields:  []string{"name"},
			collectedFields: nil,
			want:            []string{"name"},
		},
		{
			name:           "收集了多余字段不影响结果",
			requiredFields: []string{"name"},
			collectedFields: map[string]string{
				"name":  "张三",
				"extra": "多余字段",
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := &DialogueContext{
				RequiredFields:  tt.requiredFields,
				CollectedFields: tt.collectedFields,
			}

			got := ctx.MissingFields()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDialogueContext_HasAllRequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		requiredFields  []string
		collectedFields map[string]string
		want            bool
	}{
		{
			name:           "全部已收集",
			requiredFields: []string{"name", "phone"},
			collectedFields: map[string]string{
				"name":  "张三",
				"phone": "13800138000",
			},
			want: true,
		},
		{
			name:           "存在缺失字段",
			requiredFields: []string{"name", "phone"},
			collectedFields: map[string]string{
				"name": "张三",
			},
			want: false,
		},
		{
			name:            "无必填字段",
			requiredFields:  nil,
			collectedFields: map[string]string{},
			want:            true,
		},
		{
			name:            "空值也算已收集",
			requiredFields:  []string{"name"},
			collectedFields: map[string]string{"name": ""},
			want:            true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := &DialogueContext{
				RequiredFields:  tt.requiredFields,
				CollectedFields: tt.collectedFields,
			}

			got := ctx.HasAllRequiredFields()
			assert.Equal(t, tt.want, got)
		})
	}
}
