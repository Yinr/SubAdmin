export type ApiOptions = RequestInit & { timeoutMs?: number }

type AuthErrorCallback = () => void
let onAuthError: AuthErrorCallback | null = null

export function setOnAuthError(cb: AuthErrorCallback) {
  onAuthError = cb
}

export class AuthError extends Error {
  constructor(message: string) {
    super(message)
    this.name = 'AuthError'
  }
}

export async function api<T>(path: string, options: ApiOptions = {}): Promise<T> {
  const { timeoutMs, signal, ...fetchOptions } = options
  const controller = new AbortController()
  const timeout = typeof timeoutMs === 'number' && timeoutMs > 0 ? window.setTimeout(() => controller.abort(), timeoutMs) : null
  if (signal) {
    if (signal.aborted) controller.abort()
    else signal.addEventListener('abort', () => controller.abort(), { once: true })
  }
  try {
    const res = await fetch(path, {
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', ...(fetchOptions.headers || {}) },
      ...fetchOptions,
      signal: controller.signal,
    })
    const data = await res.json().catch(() => ({}))
    if (res.status === 401) {
      onAuthError?.()
      throw new AuthError(data.error || '登录已失效')
    }
    if (!res.ok) throw new Error(data.error || '请求失败')
    return data as T
  } catch (error) {
    if (error instanceof DOMException && error.name === 'AbortError') {
      throw new Error('请求超时，请稍后重试或缩小筛选范围')
    }
    if (error instanceof TypeError) {
      throw new Error('网络连接中断，请刷新后重试')
    }
    throw error
  } finally {
    if (timeout !== null) window.clearTimeout(timeout)
  }
}

export async function textResource(path: string): Promise<string> {
  const res = await fetch(path, { credentials: 'same-origin' })
  if (!res.ok) throw new Error('加载失败')
  return res.text()
}
