import { describe, expect, it } from 'vitest'
import { PI_API_OPTIONS } from './piApiOptions'

describe('PI_API_OPTIONS', () => {
  it('包含 Pi 0.80.6 的 9 种内置 API 及典型请求路径', () => {
    expect(Object.fromEntries(PI_API_OPTIONS.map((option) => [option.id, option.endpointExample]))).toEqual({
      'openai-completions': '/v1/chat/completions',
      'openai-responses': '/v1/responses',
      'openai-codex-responses': '/backend-api/codex/responses',
      'anthropic-messages': '/v1/messages',
      'google-generative-ai': '/v1beta/models/{model}:streamGenerateContent',
      'mistral-conversations': '/v1/chat/completions',
      'azure-openai-responses': '/openai/v1/responses',
      'bedrock-converse-stream': '/model/{modelId}/converse-stream',
      'google-vertex': '/v1/projects/{project}/locations/{location}/publishers/google/models/{model}:streamGenerateContent',
    })
  })
})
