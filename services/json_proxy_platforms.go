package services

// 各 JSON 型平台的差异描述。新增同类平台只需在这里加一个描述符，
// 不必再复制一份三四百行的 settings 服务。

// claudeProxyPlatform Claude Code：配置字段在 env 子对象里
var claudeProxyPlatform = jsonProxyPlatform{
	platform:        "claude",
	configDir:       claudeSettingsDir,
	configFile:      claudeSettingsFileName,
	backupFile:      claudeBackupFileName,
	authPlaceholder: claudeAuthTokenValue,
	urlSuffix:       "", // Claude 直接用代理根地址
	logPrefix:       "[ClaudeSettingsService]",
	access: jsonProxyFieldAccess{
		baseURLKey:   "ANTHROPIC_BASE_URL",
		authTokenKey: "ANTHROPIC_AUTH_TOKEN",
		container:    nestedEnvContainer,
		// env 变空且启用前本就不存在 env 时，删掉整个 env 对象，
		// 避免在用户配置里留下一个空壳
		afterWrite: func(payload map[string]any, container map[string]any, state *ProxyState) {
			if len(container) == 0 && state != nil && !state.EnvExisted {
				delete(payload, "env")
				return
			}
			payload["env"] = container
		},
		containerExisted: func(payload map[string]any) bool {
			env, _ := payload["env"].(map[string]any)
			return env != nil
		},
	},
}

// reasonixProxyPlatform Reasonix：配置字段在顶层
var reasonixProxyPlatform = jsonProxyPlatform{
	platform:        reasonixPlatform,
	configDir:       reasonixSettingsDir,
	configFile:      reasonixSettingsFileName,
	backupFile:      reasonixBackupFileName,
	authPlaceholder: reasonixAuthPlaceholder,
	urlSuffix:       "/reasonix",
	logPrefix:       "[ReasonixSettingsService]",
	access: jsonProxyFieldAccess{
		baseURLKey:   "baseUrl",
		authTokenKey: "apiKey",
		container:    topLevelContainer,
		afterWrite:   nil, // 扁平结构无需收尾
		// 扁平结构下字段始终"存在"于顶层，没有需要清理的容器
		containerExisted: func(map[string]any) bool { return true },
	},
}

// nestedEnvContainer 取出（或创建）env 子对象
func nestedEnvContainer(payload map[string]any, create bool) map[string]any {
	env, _ := payload["env"].(map[string]any)
	if env == nil {
		if !create {
			return nil
		}
		env = make(map[string]any)
	}
	return env
}

// topLevelContainer 顶层对象本身就是容器
func topLevelContainer(payload map[string]any, create bool) map[string]any {
	return payload
}
