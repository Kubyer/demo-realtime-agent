'use client';

import { useEffect, useReducer, useRef, useCallback } from 'react';

export type Status = 'playing' | 'cancelled' | 'final';

export interface Chunk {
  chunkId: string;
  text: string;
  status: Status;
  ts: number;
}

export interface TranscriptState {
  chunks: Chunk[];
  connected: boolean;
  sessionId: string | null;
}

type Action =
  | { type: 'CONNECTED' }
  | { type: 'DISCONNECTED' }
  | { type: 'UPSERT_CHUNK'; chunk: Chunk }
  | { type: 'SESSION_START'; sessionId: string }
  | { type: 'SESSION_END' };

function reducer(state: TranscriptState, action: Action): TranscriptState {
  switch (action.type) {
    case 'CONNECTED':
      return { ...state, connected: true };
    case 'DISCONNECTED':
      return { ...state, connected: false };
    case 'SESSION_START':
      return { ...state, chunks: [], sessionId: action.sessionId };
    case 'SESSION_END':
      return { ...state, sessionId: null };
    case 'UPSERT_CHUNK': {
      const idx = state.chunks.findIndex(c => c.chunkId === action.chunk.chunkId);
      if (idx === -1) {
        return { ...state, chunks: [...state.chunks, action.chunk] };
      }
      const updated = [...state.chunks];
      updated[idx] = action.chunk;
      return { ...state, chunks: updated };
    }
    default:
      return state;
  }
}

const INITIAL: TranscriptState = { chunks: [], connected: false, sessionId: null };

export function useEventsSocket(url: string) {
  const [state, dispatch] = useReducer(reducer, INITIAL);
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const connect = useCallback(() => {
    const ws = new WebSocket(url);
    wsRef.current = ws;

    ws.onopen = () => dispatch({ type: 'CONNECTED' });

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
                ts: msg.ts,
              },
            });
            break;
          case 'session_start':
            dispatch({ type: 'SESSION_START', sessionId: msg.chunk_id });
            break;
          case 'session_end':
            dispatch({ type: 'SESSION_END' });
            break;
        }
      } catch {
        // Malformed frame — ignore.
      }
    };

    ws.onerror = () => { /* onclose fires next; log there */ };

    ws.onclose = () => {
      dispatch({ type: 'DISCONNECTED' });
      // Exponential backoff reconnect: 2s, 4s, 8s … capped at 30s.
      const delay = Math.min(30_000, 2_000 * Math.pow(2, 0));
      reconnectRef.current = setTimeout(connect, delay);
    };
  }, [url]);

  useEffect(() => {
    connect();
    return () => {
      wsRef.current?.close();
      if (reconnectRef.current) clearTimeout(reconnectRef.current);
    };
  }, [connect]);

  return state;
}
