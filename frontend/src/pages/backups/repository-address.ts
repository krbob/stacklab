export function sftpRepositoryAddress(user: string, host: string, port: string, directory: string): string {
  const address = host.includes(':') && !host.startsWith('[') ? `[${host}]` : host
  const path = directory.split('/').map(encodeURIComponent).join('/')
  return `sftp://${encodeURIComponent(user)}@${address}${port && port !== '22' ? `:${port}` : ''}/${path}`
}
