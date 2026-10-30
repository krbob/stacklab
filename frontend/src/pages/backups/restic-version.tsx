export function ResticVersion({ version }: { version?: string }) {
  const parsed = version?.match(/^(\d+)\.(\d+)\.(\d+)$/)
  const major = parsed ? Number(parsed[1]) : undefined
  const minor = parsed ? Number(parsed[2]) : undefined
  const unsupported = major === 0 && minor !== undefined && minor < 14
  const legacy = major === 0 && minor !== undefined && minor >= 14 && minor <= 16
  return <p role={unsupported ? 'alert' : undefined} className={`text-xs leading-5 ${unsupported ? 'text-[var(--warning)]' : 'text-[var(--muted)]'}`}>
    Restic {parsed ? version : 'version unknown'}.
    {unsupported && ' This version is below the supported minimum (0.14.0). Upgrade restic before running backups or recovery.'}
    {legacy && ' This version provides limited retry diagnostics. If checks remain inconclusive, preserve existing copies, inspect the native check output and consider upgrading restic before repeating the required check.'}
    {major === 0 && minor === 14 && ' Cancellation during lock creation can leave a fresh repository lock. Stacklab retains its 30-minute unlock safeguard; upgrading restic can avoid this interruption (validated with 0.18).'}
  </p>
}
