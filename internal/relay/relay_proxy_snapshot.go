package relay

import (
	"github.com/gin-gonic/gin"

	"codeswitch/services"
)

const relayProxySnapshotContextKey = "codeswitch.relay.proxysnapshot"

// relayProxySnapshot 每逻辑请求读取一次全局代理配置（P5）。
//
// 原实现每个 attempt 各调一次 GetProviderProxyConfig，底下是一次 os.Stat；
// 快照后同一请求内的所有 attempt（含 Codex 续写轮次）共享一份配置。
// 副作用是请求进行中修改代理设置不再影响已开始的请求——这正是
// 请求级快照的语义：一次请求内配置一致。
type relayProxySnapshot struct {
	global services.ProxyConfig
	err    error
}

func (prs *ProviderRelayService) snapshotProxyConfig(c *gin.Context) {
	if c == nil || prs.appSettings == nil {
		return
	}
	global, err := prs.appSettings.GetGlobalProxyConfig()
	c.Set(relayProxySnapshotContextKey, &relayProxySnapshot{global: global, err: err})
}

func relayProxySnapshotFromContext(c *gin.Context) *relayProxySnapshot {
	if c == nil {
		return nil
	}
	value, exists := c.Get(relayProxySnapshotContextKey)
	if !exists {
		return nil
	}
	snapshot, _ := value.(*relayProxySnapshot)
	return snapshot
}

// providerProxyConfigFor 取本请求的代理配置（provider 级开关叠加在全局配置上）。
// 无快照时（独立于 dispatch 的调用路径）回退为直接读取。
func (prs *ProviderRelayService) providerProxyConfigFor(c *gin.Context, enabled bool) (services.ProxyConfig, error) {
	if snapshot := relayProxySnapshotFromContext(c); snapshot != nil {
		if snapshot.err != nil {
			return services.ProxyConfig{}, snapshot.err
		}
		config := snapshot.global
		config.Enabled = enabled
		return config, nil
	}
	return prs.appSettings.GetProviderProxyConfig(enabled)
}
