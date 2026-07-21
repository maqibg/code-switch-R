export const PI_API_OPTIONS = [
  { id: 'openai-completions', endpointExample: '/v1/chat/completions', gatewaySupported: true },
  { id: 'openai-responses', endpointExample: '/v1/responses', gatewaySupported: true },
  { id: 'openai-codex-responses', endpointExample: '/backend-api/codex/responses', gatewaySupported: true },
  { id: 'anthropic-messages', endpointExample: '/v1/messages', gatewaySupported: true },
  { id: 'google-generative-ai', endpointExample: '/v1beta/models/{model}:streamGenerateContent', gatewaySupported: true },
  { id: 'mistral-conversations', endpointExample: '/v1/chat/completions', gatewaySupported: false },
  { id: 'azure-openai-responses', endpointExample: '/openai/v1/responses', gatewaySupported: false },
  { id: 'bedrock-converse-stream', endpointExample: '/model/{modelId}/converse-stream', gatewaySupported: false },
  { id: 'google-vertex', endpointExample: '/v1/projects/{project}/locations/{location}/publishers/google/models/{model}:streamGenerateContent', gatewaySupported: false },
] as const

export type PiAPIOption = (typeof PI_API_OPTIONS)[number]

const piAPIIDs = new Set<string>(PI_API_OPTIONS.map((option) => option.id))

export const isPiAPIID = (value: string): boolean => piAPIIDs.has(value)

export const formatPiAPIOption = (option: PiAPIOption): string =>
  `${option.id} — ${option.endpointExample}${option.gatewaySupported ? ' · 网关支持' : ' · 仅 Pi 直连'}`
