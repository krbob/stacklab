export function sftpRepositoryAddress(user: string, host: string, port: string, directory: string): string {
  const address = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host
  const path = directory.split('/').map(encodeURIComponent).join('/')
  return `sftp://${encodeURIComponent(user)}@${address}${port && port !== '22' ? `:${port}` : ''}/${path}`
}

export function validSFTPHost(host: string) {
  if (!host || /[\s/@]/.test(host)) return false
  if (!host.includes(':')) return !host.includes('[') && !host.includes(']')
  try {
    const raw = host.replace(/^\[|\]$/g, '')
    return new URL(`http://[${raw}]`).hostname.startsWith('[')
  } catch { return false }
}
