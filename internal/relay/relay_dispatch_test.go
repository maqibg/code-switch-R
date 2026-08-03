package relay

import (
	"codeswitch/services"
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// 失败分类是三套 handler 合并后的唯一判定入口，逐类锁死。
//
// 合并前这段逻辑在三处各写一份且已分叉：判定顺序不同（有的先看客户端中断、
// 有的先看响应是否已提交。
func TestClassifyDispatchFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want dispatchAction
	}{
		{
			name: "普通失败：记账并继续换 provider",
			err:  errors.New("HTTP 500"),
			want: dispatchAction{Outcome: dispatchExhausted, RecordFailure: true},
		},
		{
			name: "客户端请求被拒绝：直接 400，不记账不换",
			err:  services.NewClientRequestRejectedError("不支持的 role"),
			want: dispatchAction{Outcome: dispatchClientRejected, Stop: true},
		},
		{
			name: "响应已提交：停止但仍记账",
			err:  fmt.Errorf("%w: 上游断流", errResponseCommitted),
			want: dispatchAction{Outcome: dispatchStopped, RecordFailure: true, Stop: true},
		},
		{
			name: "响应已提交且客户端断开：停止且不记账",
			err:  fmt.Errorf("%w: %w: broken pipe", errResponseCommitted, errClientAbort),
			want: dispatchAction{Outcome: dispatchStopped, Stop: true},
		},
		{
			name: "客户端断开：停止且不记账",
			err:  fmt.Errorf("%w: context canceled", errClientAbort),
			want: dispatchAction{Outcome: dispatchStopped, Stop: true},
		},
		{
			name: "跳过 provider：不记账、不停止",
			err:  fmt.Errorf("%w: 模型映射失败", errSkipProvider),
			want: dispatchAction{SkipProvider: true},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyDispatchFailure(tc.err)
			if got != tc.want {
				t.Errorf("分类结果不符\n实际: %+v\n期望: %+v", got, tc.want)
			}
		})
	}
}

// 行为变更：客户端断开且响应未提交时统一停止调度。
//
// 降级模式原先在这种情况下会继续尝试下一个 provider（固定模式则停止）。
// 客户端已经走了，继续转发没有接收方，只是白烧上游配额。
func TestClientAbortStopsDispatchInBothModes(t *testing.T) {
	action := classifyDispatchFailure(fmt.Errorf("%w: context canceled", errClientAbort))
	if !action.Stop {
		t.Error("客户端断开必须停止调度，不能继续尝试下一个 provider")
	}
	if action.RecordFailure {
		t.Error("客户端断开不是 provider 的问题，不应记失败")
	}
}

func TestClientRequestContextErrorDetectsDisconnectedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ginContext, _ := gin.CreateTestContext(httptest.NewRecorder())
	requestContext, cancel := context.WithCancel(context.Background())
	ginContext.Request = httptest.NewRequest("POST", "/v1/messages", nil).WithContext(requestContext)
	if clientRequestContextError(ginContext) != nil {
		t.Fatal("请求未取消时不应报告客户端中断")
	}
	cancel()
	if !errors.Is(clientRequestContextError(ginContext), context.Canceled) {
		t.Fatal("请求取消后必须识别为客户端中断")
	}
}

// Level 分组：未配置或零值按 1 处理，Level 列表升序
func TestGroupProvidersByLevel(t *testing.T) {
	levels, groups := groupProvidersByLevel([]services.Provider{
		{Name: "A", Level: 3},
		{Name: "B"},           // 未配置 → 1
		{Name: "C", Level: 0}, // 零值 → 1
		{Name: "D", Level: 2},
		{Name: "E", Level: 1},
	})

	want := []int{1, 2, 3}
	if len(levels) != len(want) {
		t.Fatalf("Level 列表应为 %v，实际 %v", want, levels)
	}
	for i := range want {
		if levels[i] != want[i] {
			t.Fatalf("Level 应升序 %v，实际 %v", want, levels)
		}
	}
	if len(groups[1]) != 3 {
		t.Errorf("Level 1 应有 3 个（含未配置与零值），实际 %d", len(groups[1]))
	}
	if len(groups[2]) != 1 || len(groups[3]) != 1 {
		t.Errorf("Level 2/3 各应有 1 个，实际 %d / %d", len(groups[2]), len(groups[3]))
	}
	// 同 Level 内保持输入顺序
	if groups[1][0].Name != "B" || groups[1][1].Name != "C" || groups[1][2].Name != "E" {
		t.Errorf("同 Level 应保持输入顺序，实际 %v", []string{
			groups[1][0].Name, groups[1][1].Name, groups[1][2].Name,
		})
	}
}
