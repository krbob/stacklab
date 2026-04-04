// WebSocket frame types derived from docs/api/websocket-protocol.md

// --- Client → Server ---

export interface WsClientCommand {
  type: string
  request_id: string
  stream_id: string
  payload: Record<string, unknown>
}

// --- Server → Client ---

export interface WsServerFrame {
  type: string
  request_id?: string
  stream_id?: string
  payload?: Record<string, unknown>
  error?: {
    code: string
    message: string
  }
}

export interface WsHelloPayload {
  connection_id: string
  protocol_version: number
  heartbeat_interval_ms: number
  features: {
    host_shell: boolean
  }
}

// --- Log events ---

export interface LogEntry {
  timestamp: string
  service_name: string
  container_id: string
  stream: 'stdout' | 'stderr'
  line: string
}

// --- Stats frames ---

export interface StatsContainerFrame {
  container_id: string
  service_name: string
  cpu_percent: number
  memory_bytes: number
  memory_limit_bytes: number
  network_rx_bytes_per_sec: number
  network_tx_bytes_per_sec: number
}

export interface StatsFrame {
  timestamp: string
  stack_totals: {
    cpu_percent: number
    memory_bytes: number
    memory_limit_bytes: number
    network_rx_bytes_per_sec: number
    network_tx_bytes_per_sec: number
  }
  containers: StatsContainerFrame[]
}

// --- Job events ---

export interface JobEvent {
  job_id: string
  stack_id: string | null
  action: string
  state: string
  event: string
  message: string
  data?: string | null
  step?: {
    index: number
    total: number
    action: string
    target_stack_id?: string
  } | null
  timestamp: string
}

// --- Terminal ---

export interface TerminalOpenedPayload {
  session_id: string
  container_id: string
  shell: string
}

export interface TerminalOutputPayload {
  session_id: string
  data: string
}

export interface TerminalExitedPayload {
  session_id: string
  exit_code: number
  reason: string
}
