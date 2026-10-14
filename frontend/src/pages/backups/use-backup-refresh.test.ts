import { act, renderHook } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import type { BackupOverview } from '@/lib/backup-api'
import { useBackupRefresh } from './use-backup-refresh'

const overview: BackupOverview = { destinations: [], plans: [], runs: [], protection: [], restic_available: true, rclone_available: true, helper_configured: true, helper_ready: true, staging_directory: '/staging' }

describe('Backup resource invalidation', () => {
  it('refreshes only the affected destination and never lists snapshots for access or policy jobs', () => {
    const refetch = vi.fn()
    const { result, rerender } = renderHook(({ data }) => useBackupRefresh(data, refetch), { initialProps: { data: overview } })
    act(() => result.current.track('access'))
    act(() => result.current.finished('access'))
    expect(result.current.snapshotRevisions).toEqual({})
    act(() => result.current.track('policy', { policy: true }))
    act(() => result.current.finished('policy'))
    expect(result.current.snapshotRevisions).toEqual({})
    expect(result.current.policyRevision).toBe(1)
    act(() => result.current.track('prune', { destinationIds: ['nas'], snapshots: true }))
    act(() => result.current.finished('prune'))
    rerender({ data: { ...overview } })
    expect(result.current.snapshotRevisions).toEqual({ nas: 1 })
    expect(result.current.restoreRevisions).toEqual({})
    act(() => result.current.finished('prune'))
    expect(result.current.snapshotRevisions).toEqual({ nas: 1 })
  })

  it('invalidates externally removed checkpoints but ignores routine inventory timestamps', () => {
    const copy = { destination_id: 'nas', activity: 'idle' as const, state: 'current' as const, snapshot_id: 'saved', snapshot_status: 'present' as const, snapshot_checked_at: '2026-10-14T00:00:00Z' }
    const protection = { plan_id: 'plan', plan_name: 'Plan', state: 'healthy' as const, activity: 'idle' as const, expected_copies: 1, current_copies: 1, copies: [copy] }
    const data = { ...overview, protection: [protection] }
    const refetch = vi.fn()
    const { result, rerender } = renderHook(({ value }) => useBackupRefresh(value, refetch), { initialProps: { value: data as BackupOverview } })
    rerender({ value: { ...data, protection: [{ ...protection, copies: [{ ...copy, snapshot_checked_at: '2026-10-14T00:01:00Z' }] }] } })
    expect(result.current.snapshotRevisions).toEqual({})
    rerender({ value: { ...data, protection: [{ ...protection, copies: [{ ...copy, snapshot_status: 'missing' }] }] } })
    expect(result.current.snapshotRevisions).toEqual({ nas: 1 })
    rerender({ value: { ...data, protection: [{ ...protection, copies: [{ ...copy, snapshot_status: 'missing', snapshot_checked_at: '2026-10-14T00:02:00Z' }] }] } })
    expect(result.current.snapshotRevisions).toEqual({ nas: 1 })
  })
})


describe('Completion and evidence reconciliation', () => {
  const copy: BackupOverview['runs'][number]['copies'][number] = { destination_id: 'nas', state: 'succeeded', snapshot_id: 'saved', verification: 'recovery_objects_verified', retention: 'succeeded' }
  const run: BackupOverview['runs'][number] = { id: 'run', job_id: 'local', plan_id: 'plan', plan_name: 'Plan', state: 'running', capture_state: 'succeeded', phase: 'upload', created_at: '2026-10-14T00:00:00Z', expires_at: '2026-10-15T00:00:00Z', copies: [copy, { ...copy, destination_id: 'cloud', state: 'running', retention: 'pending' }] }
  it('lists a settled local copy once before its sibling completes and does not repeat at job completion', () => {
    const { result, rerender } = renderHook(({ data }) => useBackupRefresh(data, vi.fn()), { initialProps: { data: overview } })
    act(() => result.current.track('local', { destinationIds: ['nas', 'cloud'], snapshots: true }))
    rerender({ data: { ...overview, runs: [{ ...run, copies: [{ ...copy, retention: 'pending' }, run.copies[1]] }] } })
    expect(result.current.snapshotRevisions).toEqual({})
    rerender({ data: { ...overview, runs: [run] } })
    expect(result.current.snapshotRevisions).toEqual({ nas: 1 })
    act(() => result.current.finished('local'))
    expect(result.current.snapshotRevisions).toEqual({ nas: 1 })
    rerender({ data: { ...overview, runs: [{ ...run, state: 'succeeded', copies: [copy, { ...copy, destination_id: 'cloud' }] }] } })
    expect(result.current.snapshotRevisions).toEqual({ nas: 1, cloud: 1 })
  })

  it('reconciles a local prune completion with its newly observed timestamp once', () => {
    const { result, rerender } = renderHook(({ data }) => useBackupRefresh(data, vi.fn()), { initialProps: { data: overview } })
    act(() => result.current.track('prune', { destinationIds: ['nas'], snapshots: true }))
    act(() => result.current.finished('prune'))
    const destination = { id: 'nas', name: 'NAS', kind: 'local', repository: '/nas', credentials_configured: true, status: 'available', metadata_check: { status: 'unknown', scope: 'unknown' }, data_check: { status: 'unknown', scope: 'unknown' }, last_prune_at: '2026-10-14T01:00:00Z' } as BackupOverview['destinations'][number]
    rerender({ data: { ...overview, destinations: [destination] } })
    expect(result.current.snapshotRevisions).toEqual({ nas: 1 })
    rerender({ data: { ...overview, destinations: [{ ...destination }] } })
    expect(result.current.snapshotRevisions).toEqual({ nas: 1 })
  })
})
