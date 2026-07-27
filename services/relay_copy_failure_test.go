package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	relayprotocol "codeswitch/services/protocol"
	"github.com/gin-gonic/gin"
)

func newCopyFailureContext(t *testing.T, written bool) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	if written {
		c.Writer.WriteHeader(http.StatusOK)
		_, _ = c.Writer.Write([]byte("data: partial\n\n"))
		c.Writer.Flush()
	}
	return c, rec
}

// 核心回归：上游断流不得记为 provider 成功。
//
// 旧实现对非转换路径一律 return (true, nil)，理由是"复制失败是客户端问题"，
// 于是反复半途断流的 provider 永远攒不够失败次数，拉黑机制对它完全失效。
func TestUpstreamStreamInterruptionCountsAsProviderFailure(t *testing.T) {
	prs := &ProviderRelayService{}
	execution := &relayForwardExecution{
		RoutePlan: relayprotocol.RoutePlan{NeedsTransform: false},
	}

	for _, written := range []bool{false, true} {
		c, _ := newCopyFailureContext(t, written)
		upstreamErr := errors.New("error streaming response: unexpected EOF")

		success, err := prs.judgeResponseCopyFailure(c, Provider{Name: "BadUpstream"}, execution, upstreamErr)

		if success {
			t.Errorf("written=%v: 上游断流必须记为失败，不能返回成功", written)
		}
		if err == nil {
			t.Fatalf("written=%v: 应返回错误", written)
		}
		if errors.Is(err, errClientAbort) {
			t.Errorf("written=%v: 上游断流不应被归类为客户端中断: %v", written, err)
		}
		// 响应已提交时必须阻止切换 provider，但仍然是失败
		if written && !errors.Is(err, errResponseCommitted) {
			t.Errorf("响应已提交时应包 errResponseCommitted，实际: %v", err)
		}
		if !written && errors.Is(err, errResponseCommitted) {
			t.Errorf("响应未提交时不应包 errResponseCommitted，实际: %v", err)
		}
	}
}

// 写客户端失败不是 provider 的问题，应归类为客户端中断
func TestClientWriteFailureDoesNotCountAsProviderFailure(t *testing.T) {
	prs := &ProviderRelayService{}
	execution := &relayForwardExecution{
		RoutePlan: relayprotocol.RoutePlan{NeedsTransform: false},
	}
	c, _ := newCopyFailureContext(t, true)

	clientErr := errors.Join(errClientWriteFailed, errors.New("broken pipe"))
	success, err := prs.judgeResponseCopyFailure(c, Provider{Name: "GoodUpstream"}, execution, clientErr)

	if success {
		t.Error("复制失败时不应返回成功")
	}
	if !errors.Is(err, errClientAbort) {
		t.Errorf("写客户端失败应归类为客户端中断，实际: %v", err)
	}
}

// 协议转换失败仍按上游问题处理（保持原有语义）
func TestTransformFailureCountsAsProviderFailure(t *testing.T) {
	prs := &ProviderRelayService{}
	execution := &relayForwardExecution{
		RoutePlan: relayprotocol.RoutePlan{NeedsTransform: true},
	}
	c, _ := newCopyFailureContext(t, false)

	success, err := prs.judgeResponseCopyFailure(c, Provider{Name: "P"}, execution, errors.New("不支持的事件类型"))
	if success {
		t.Error("协议转换失败不应返回成功")
	}
	if errors.Is(err, errClientAbort) {
		t.Errorf("协议转换失败不应归类为客户端中断: %v", err)
	}
}

// 客户端主动断开优先于其他判定
func TestClientAbortTakesPrecedence(t *testing.T) {
	prs := &ProviderRelayService{}
	execution := &relayForwardExecution{RoutePlan: relayprotocol.RoutePlan{}}

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = req.WithContext(ctx)

	success, err := prs.judgeResponseCopyFailure(c, Provider{Name: "P"}, execution, errors.New("any"))
	if success {
		t.Error("不应返回成功")
	}
	if !errors.Is(err, errClientAbort) {
		t.Errorf("客户端已断开应归类为 errClientAbort，实际: %v", err)
	}
}

// clientTrackingWriter 必须保留 Flusher 能力，否则 SSE 会被缓冲
func TestClientTrackingWriterPreservesFlusher(t *testing.T) {
	rec := httptest.NewRecorder()
	tracked := newClientTrackingWriter(rec)
	if _, ok := interface{}(tracked).(http.Flusher); !ok {
		t.Fatal("clientTrackingWriter 必须实现 http.Flusher，否则流式响应不会逐块下发")
	}
	if tracked.clientWriteFailed() {
		t.Error("未发生写失败时不应报告失败")
	}
}

func TestClientTrackingWriterRecordsWriteError(t *testing.T) {
	tracked := newClientTrackingWriter(&failingResponseWriter{})
	if _, err := tracked.Write([]byte("x")); err == nil {
		t.Fatal("底层写失败时应返回错误")
	}
	if !tracked.clientWriteFailed() {
		t.Error("应记录写客户端失败")
	}

	wrapped := classifyCopyError(errors.New("copy failed"), tracked)
	if !errors.Is(wrapped, errClientWriteFailed) {
		t.Errorf("发生写失败时应标记 errClientWriteFailed，实际: %v", wrapped)
	}
}

type failingResponseWriter struct{}

func (f *failingResponseWriter) Header() http.Header       { return http.Header{} }
func (f *failingResponseWriter) WriteHeader(int)           {}
func (f *failingResponseWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
