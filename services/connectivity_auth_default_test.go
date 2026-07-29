package services

import "testing"

// 该测试原在 providerrelay_models_test.go;relay 拆包后被测函数
// defaultConnectivityAuthType 留在 services,测试跟着留下。
func TestDefaultConnectivityAuthType(t *testing.T) {
	cases := []struct {
		platform string
		want     string
	}{
		{platform: "claude", want: "bearer"},
		{platform: "claude-code", want: "bearer"},
		{platform: "codex", want: "bearer"},
		{platform: "reasonix", want: "bearer"},
		{platform: "custom", want: "bearer"},
	}

	for _, tc := range cases {
		if got := defaultConnectivityAuthType(tc.platform); got != tc.want {
			t.Fatalf("%s 默认认证方式期望 %s，实际 %s", tc.platform, tc.want, got)
		}
	}
}

// TestCustomModelsHandler 测试自定义 CLI 工具的 /v1/models 端点
