package main

const AppVersion = "v2.6.58"

type VersionService struct {
	version string
}

func NewVersionService() *VersionService {
	return &VersionService{version: AppVersion}
}

func (vs *VersionService) CurrentVersion() string {
	return vs.version
}

// GetUpdatePolicy 保留给现有前端调用，应用只支持便携更新。
func (vs *VersionService) GetUpdatePolicy() string {
	return "portable"
}
