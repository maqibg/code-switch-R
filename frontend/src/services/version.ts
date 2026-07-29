/**
 * 版本信息 API 封装
 *
 * 走 frontend/bindings 生成的类型化函数，不用 Call.ByName：
 * 后者靠字符串拼服务名，Go 侧签名变化时编译期发现不了。
 * VersionService 在根包，绑定路径是 bindings/codeswitch/versionservice。
 */
import * as VersionService from '../../bindings/codeswitch/versionservice'

export const fetchCurrentVersion = async (): Promise<string> => {
  const version = await VersionService.CurrentVersion()
  return version ?? ''
}
