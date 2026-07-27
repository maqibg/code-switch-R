package services

import (
	"errors"
	"net/http"
)

// errClientWriteFailed 标记"复制响应失败的原因在客户端侧"，而不是上游。
//
// 区分这两者对 provider 记账很关键：客户端写失败不是 provider 的问题，
// 但上游断流是——把上游断流记成成功会让反复半途断流的 provider 永远达不到拉黑阈值。
var errClientWriteFailed = errors.New("client write failed")

// clientTrackingWriter 包装 gin 的 ResponseWriter，记录第一次写客户端失败。
//
// 为什么不用错误文案判断：上游库 xrequest 用 "error streaming response"（读上游失败）
// 和 "error writing response"（写客户端失败）区分两侧，但依赖第三方库的错误文案很脆，
// 库一改措辞判断就静默失效。这里直接在写入点记录事实。
//
// 必须实现 http.Flusher：xrequest 通过 `w.(http.Flusher)` 断言决定是否逐块 flush，
// 包装后若丢掉 Flusher，SSE 流式响应会被缓冲，客户端看不到增量输出。
type clientTrackingWriter struct {
	http.ResponseWriter
	writeErr error
}

func newClientTrackingWriter(w http.ResponseWriter) *clientTrackingWriter {
	return &clientTrackingWriter{ResponseWriter: w}
}

func (w *clientTrackingWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	}
	return n, err
}

func (w *clientTrackingWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// clientWriteFailed 报告是否发生过写客户端失败
func (w *clientTrackingWriter) clientWriteFailed() bool {
	return w != nil && w.writeErr != nil
}

// classifyCopyError 给复制响应的错误标注来源。
// 写客户端失败的错误会被包上 errClientWriteFailed，其余按上游问题处理。
func classifyCopyError(err error, writer *clientTrackingWriter) error {
	if err == nil {
		return nil
	}
	if writer.clientWriteFailed() {
		return errors.Join(errClientWriteFailed, err)
	}
	return err
}
