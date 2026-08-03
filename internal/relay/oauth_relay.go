package relay

import "codeswitch/services"

func (prs *ProviderRelayService) resolveProviderCredential(provider services.Provider, platform string) (services.Provider, map[string]string, error) {
	if prs.oauthAccountService == nil {
		return provider, nil, nil
	}
	return services.ResolveProviderCredential(prs.oauthAccountService, provider, platform)
}
