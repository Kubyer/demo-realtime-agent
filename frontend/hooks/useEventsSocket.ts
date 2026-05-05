'use client';

import { useEffect, useReducer, useRef, useCallback } from 'react';

export type Status = 'playing' | 'cancelled' | 'final';

export interface Chunk {
  chunkId: string;
  text: string;
  status: Status;
  role: 'user' | 'assistant';
  ts: number;
  metrics?: Metrics;
}

export interface Metrics {
  ttft_ms: number;
  ttfa_ms: number;
  e2e_ms: number;
}

export interface TranscriptState {
  chunks: Chunk[];
  connected: boolean;
  sessionId: string | null;
  latestMetrics: Metrics | null;
}

type Action =
  | { type: 'CONNECTED' }
  | { type: 'DISCONNECTED' }
  | { type: 'UPSERT_CHUNK'; chunk: Chunk }
  | { type: 'SESSION_START'; sessionId: string }
  | { type: 'SESSION_END' }
  | { type: 'METRICS'; metrics: Metrics };

function reducer(state: TranscriptState, action: Action): TranscriptState {
  switch (action.type) {
    case 'CONNECTED':
      return { ...state, connected: true };
    case 'DISCONNECTED':
      return { ...state, connected: false };
    case 'SESSION_START':
      return { ...state, chunks: [], sessionId: action.sessionId, latestMetrics: null };
    case 'SESSION_END':
      return { ...state, sessionId: null };
    case 'METRICS': {
      // Attach metrics to the last assistant chunk in the transcript.
      const lastIdx = state.chunks.reduceRight(
        (found, c, i) => (found === -1 && c.role === 'assistant' ? i : found),
        -1,
      );
      if (lastIdx === -1) return { ...state, latestMetrics: action.metrics };
      const withMetrics = [...state.chunks];
      withMetrics[lastIdx] = { ...withMetrics[lastIdx], metrics: action.metrics };
      return { ...state, latestMetrics: action.metrics, chunks: withMetrics };
    }
    case 'UPSERT_CHUNK': {
      const idx = state.chunks.findIndex(c => c.chunkId === action.chunk.chunkId);
      if (idx === -1) {
        // Skip empty-text chunks that would show as "…" — they carry no info
        if (!action.chunk.text) return state;
        return { ...state, chunks: [...state.chunks, action.chunk] };
      }
      const updated = [...state.chunks];
      // Preserve non-empty text from the previous state (e.g. cancelled events come with no text)
      updated[idx] = {
        ...action.chunk,
        text: action.chunk.text || state.chunks[idx].text,
      };
      return { ...state, chunks: updated };
    }
    default:
      return state;
  }
}

const INITIAL: TranscriptState = {
  chunks: [],
  connected: false,
  sessionId: null,
  latestMetrics: null,
};

// ---------------------------------------------------------------------------
// Tool event types — exposed separately from the main transcript state.
// ---------------------------------------------------------------------------

export interface ToolEvent {
  id: string;       // unique per event
  kind: 'call' | 'result';
  name: string;
  payload: string;  // raw JSON string
  ts: number;       // unix millis
}

export function useEventsSocket(url: string) {
  const [state, dispatch] = useReducer(reducer, INITIAL);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const attemptRef = useRef(0);
  const mountedRef = useRef(true);

  // Tool events are kept in a ref+callback pattern so callers can subscribe
  // without forcing a full re-render on every transcript chunk.
  const toolEventsRef = useRef<ToolEvent[]>([]);
  const toolSeqRef = useRef(0);

  // We expose a stable getter so LiveCallView can read the latest list.
  const getToolEvents = useCallback(() => toolEventsRef.current, []);

  // Listeners that want to be notified when toolEvents change.
  const toolListenersRef = useRef<Set<() => void>>(new Set());
  const onToolEvent = useCallback((fn: () => void) => {
    toolListenersRef.current.add(fn);
    return () => toolListenersRef.current.delete(fn);
  }, []);

  const notifyToolListeners = useCallback(() => {
    toolListenersRef.current.forEach(fn => fn());
  }, []);

  const connect = useCallback(() => {
    if (!mountedRef.current) return;
    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => {
      attemptRef.current = 0;
      dispatch({ type: 'CONNECTED' });
    };

    ws.onmessage = (ev) => {
      try {
        const msg = JSON.parse(ev.data as string);
        switch (msg.type) {
          case 'transcript':
            dispatch({
              type: 'UPSERT_CHUNK',
              chunk: {
                chunkId: msg.chunk_id,
                text: msg.text ?? '',
                status: msg.status,
                role: msg.role ?? 'assistant',
                ts: msg.ts,
              },
            });
            break;
          case 'session_start':
            dispatch({ type: 'SESSION_START', sessionId: msg.chunk_id });
            // Clear tool events on new session.
            toolEventsRef.current = [];
            notifyToolListeners();
            break;
          case 'session_end':
            dispatch({ type: 'SESSION_END' });
            break;
          case 'metrics':
            if (msg.metrics) {
              dispatch({ type: 'METRICS', metrics: msg.metrics });
            }
            break;
          case 'tool_call':
          case 'tool_result':
            if (msg.tool_call) {
              const te: ToolEvent = {
                id: `te-${++toolSeqRef.current}`,
                kind: msg.type === 'tool_call' ? 'call' : 'result',
                name: msg.tool_call.name ?? '',
                payload: msg.type === 'tool_call'
                  ? (msg.tool_call.arguments ?? '')
                  : (msg.tool_call.result ?? ''),
                ts: msg.ts ?? Date.now(),
              };
              toolEventsRef.current = [...toolEventsRef.current, te];
              notifyToolListeners();
            }
            break;
        }
      } catch {
        // Malformed frame — ignore.
      }
    };

    ws.onerror = () => { /* onclose fires next */ };

    ws.onclose = () => {
      dispatch({ type: 'DISCONNECTED' });
      if (!mountedRef.current) return;
      // Exponential backoff: 2s, 4s, 8s … capped at 30s.
      attemptRef.current += 1;
      const delay = Math.min(30_000, 2_000 * Math.pow(2, attemptRef.current - 1));
      reconnectRef.current = setTimeout(connect, delay);
    };
  }, [url, notifyToolListeners]);

  useEffect(() => {
    mountedRef.current = true;
    connect();
    return () => {
      mountedRef.current = false;
      wsRef.current?.close();
      if (reconnectRef.current) clearTimeout(reconnectRef.current);
    };
  }, [connect]);

  return { ...state, getToolEvents, onToolEvent };
}
