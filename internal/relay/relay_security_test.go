package relay

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codeswitch/services"

	"github.com/gin-gonic/gin"
)

func withRelayToken(t *testing.T, token string) {
	t.Helper()
	previous := services.RelayToken()
	if err := services.SetRelayToken(token); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = services.SetRelayToken(previous)
	})
}

func TestRelayTokenMiddlewareRejectsMissingCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withRelayToken(t, strings.Repeat("a", 32))
	router := gin.New()
	router.Use(relayTokenMiddleware())
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/test", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("缺少 Relay Token 应返回 401，实际 %d", recorder.Code)
	}
}

func TestRelayTokenMiddlewareRemovesClientCredential(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token := strings.Repeat("b", 32)
	withRelayToken(t, token)
	router := gin.New()
	router.Use(relayTokenMiddleware())
	router.GET("/test", func(c *gin.Context) {
		if c.GetHeader("Authorization") != "" || c.GetHeader("X-Api-Key") != "" || c.GetHeader("X-Goog-Api-Key") != "" {
			c.Status(http.StatusBadRequest)
			return
		}
		if c.Query("key") != "" || c.Query("keep") != "1" {
			c.Status(http.StatusBadRequest)
			return
		}
		c.Status(http.StatusNoContent)
	})

	request := httptest.NewRequest(http.MethodGet, "/test?key="+token+"&keep=1", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Api-Key", token)
	request.Header.Set("X-Goog-Api-Key", token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("通过验证后应移除客户端凭据，实际状态 %d", recorder.Code)
	}
}

func TestRelayTokenMiddlewareAcceptsLegacyManagedToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	withRelayToken(t, strings.Repeat("d", 32))
	router := gin.New()
	router.Use(relayTokenMiddleware())
	router.GET("/test", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	for _, token := range []string{"code-switch-r", "code-switch-r-proxy"} {
		request := httptest.NewRequest(http.MethodGet, "/test", nil)
		request.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNoContent {
			t.Fatalf("旧版托管 Token %q 应在迁移期间通过，实际状态 %d", token, recorder.Code)
		}
	}
}

func TestProviderRelayRestartRestoresOldListenerWhenTargetPortOccupied(t *testing.T) {
	withRelayToken(t, strings.Repeat("c", 32))
	oldProbe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	oldAddr := oldProbe.Addr().String()
	_ = oldProbe.Close()

	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()

	service := NewProviderRelayService(services.NewProviderService(), nil, nil, nil, nil, oldAddr)
	if err := service.Start(); err != nil {
		t.Fatal(err)
	}
	defer service.Stop()

	if err := service.Restart(occupied.Addr().String()); err == nil {
		t.Fatal("目标端口被占用时 Restart 应失败")
	}
	if service.Addr() != oldAddr {
		t.Fatalf("失败后应恢复旧地址，实际 %s", service.Addr())
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		connection, dialErr := net.DialTimeout("tcp", oldAddr, 100*time.Millisecond)
		if dialErr == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("旧监听未恢复: %v", dialErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
