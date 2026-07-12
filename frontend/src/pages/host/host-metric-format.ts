import { formatBytes } from '@/pages/host-page-utils'

export function formatLoadAverage(values: number[]): string {
  return values.map((value) => value.toFixed(2)).join(' / ')
}

export function formatTemperature(value: number): string {
  return `${value.toFixed(1)} °C`
}

export function maskPublicIP(value: string): string {
  return value.includes(':') ? '****:****:****' : '***.***.***.***'
}

export function formatRate(bytesPerSecond: number): string {
  return `${formatBytes(Math.max(0, Math.round(bytesPerSecond)))}/s`
}
