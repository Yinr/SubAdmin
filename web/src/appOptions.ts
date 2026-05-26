export const platformOptions = [
  { value: 'anthropic', label: 'Anthropic' },
  { value: 'openai', label: 'OpenAI' },
  { value: 'gemini', label: 'Gemini' },
  { value: 'antigravity', label: 'Antigravity' },
]

export const accountTypeOptions = [
  { value: 'oauth', label: 'OAuth' },
  { value: 'setup-token', label: 'Setup Token' },
  { value: 'apikey', label: 'API Key' },
  { value: 'bedrock', label: 'AWS Bedrock' },
  { value: 'upstream', label: '对接上游' },
  { value: 'service-account', label: 'Service Account' },
]

export const accountStatusOptions = [
  { value: 'active', label: '正常' },
  { value: 'inactive', label: '停用' },
  { value: 'error', label: '错误' },
  { value: 'rate_limited', label: '限流中' },
  { value: 'temp_unschedulable', label: '临时不可调度' },
  { value: 'unschedulable', label: '不可调度' },
]

export const privacyModeOptions = [
  { value: '__unset__', label: '未设置' },
  { value: 'training_off', label: '已关闭训练数据共享' },
  { value: 'training_set_cf_blocked', label: '被 Cloudflare 拦截，训练可能仍开启' },
  { value: 'training_set_failed', label: '关闭训练数据共享失败' },
  { value: 'privacy_set', label: '已关闭遥测和营销邮件' },
  { value: 'privacy_set_failed', label: '隐私设置失败' },
]
