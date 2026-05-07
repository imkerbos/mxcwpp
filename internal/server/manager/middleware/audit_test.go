package middleware

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractResource(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		wantType     string
		wantID       string
	}{
		{
			name:     "标准资源路径",
			path:     "/api/v1/hosts/abc123",
			wantType: "hosts",
			wantID:   "abc123",
		},
		{
			name:     "带 batch 操作",
			path:     "/api/v1/alerts/batch/resolve",
			wantType: "alerts",
			wantID:   "",
		},
		{
			name:     "带 statistics 路径",
			path:     "/api/v1/hosts/statistics",
			wantType: "hosts",
			wantID:   "",
		},
		{
			name:     "带 whitelist 路径",
			path:     "/api/v1/alerts/whitelist",
			wantType: "alerts",
			wantID:   "",
		},
		{
			name:     "带 resolve 路径",
			path:     "/api/v1/alerts/resolve",
			wantType: "alerts",
			wantID:   "",
		},
		{
			name:     "带 ignore 路径",
			path:     "/api/v1/alerts/ignore",
			wantType: "alerts",
			wantID:   "",
		},
		{
			name:     "仅资源类型无 ID",
			path:     "/api/v1/policies",
			wantType: "policies",
			wantID:   "",
		},
		{
			name:     "数字 ID",
			path:     "/api/v1/tasks/42",
			wantType: "tasks",
			wantID:   "42",
		},
		{
			name:     "空路径",
			path:     "",
			wantType: "",
			wantID:   "",
		},
		{
			name:     "无 api 前缀 — TrimPrefix 后首段变为空",
			path:     "/health",
			wantType: "",
			wantID:   "health",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotID := extractResource(tt.path)
			assert.Equal(t, tt.wantType, gotType)
			assert.Equal(t, tt.wantID, gotID)
		})
	}
}
