package biz

import "testing"

func TestCompareVersionStrings(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.25.1", "1.25.0", 1},
		{"1.25.0", "1.25.1", -1},
		// epoch 格式
		{"1:1.2.3", "1.2.3", 0},
		{"2:1.0.0", "1.0.0", 0}, // epoch 被去掉后比较主版本
		// release suffix
		{"1.1.1k-1.el7", "1.1.1k", 0},
		{"1.25.1-1ubuntu1", "1.25.0", 1},
		// 不同长度
		{"1.0", "1.0.0", 0},
		{"1.1", "1.0.9", 1},
	}

	for _, tt := range tests {
		result := compareVersionStrings(tt.v1, tt.v2)
		if result != tt.expected {
			t.Errorf("compareVersionStrings(%q, %q) = %d, want %d", tt.v1, tt.v2, result, tt.expected)
		}
	}
}

func TestParseVersionPart(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"123", 123},
		{"1k", 1},
		{"25p1", 25},
		{"0", 0},
		{"abc", 0},
		{"", 0},
	}

	for _, tt := range tests {
		result := parseVersionPart(tt.input)
		if result != tt.expected {
			t.Errorf("parseVersionPart(%q) = %d, want %d", tt.input, result, tt.expected)
		}
	}
}
