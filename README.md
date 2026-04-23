# Voice Agent — Ultra-Low-Latency Real-Time AI

A real-time voice agent pipeline built for minimal end-to-end latency.  
**Stack:** Go 1.22 backend · Next.js 14 frontend · Twilio Media Streams

---

## Architecture

```
Twilio (WebSocket) ──▶ STT (Soniox) ──▶ LLM (Groq) ──▶ TTS (Cartesia) ──▶ Twilio
                                 ↕
                       Telemetry / Events (SSE)
                                 ↕
                         Next.js dashboard
```

**Latency target:** < 800 ms STT-final → TTS-first-chunk

### Key design decisions

| Component | Choice | Reason |
|---|---|---|
| STT | Soniox WebSocket | Real-time partial + final tokens |
| LLM | Groq (`moonshotai/gpt-oss-20b`) | ~950 tokens/s on LPU |
| TTS | Cartesia WebSocket | Sentence-level streaming |
| Dispatcher | Speculative with barge-in | Priority ladder Tier 0/1/2 |

---

## Prerequisites

- Go 1.22+
- Node.js 18+
- A Twilio account with a phone number
- API keys: [Soniox](https://soniox.com), [Groq](https://console.groq.com), [Cartesia](https://cartesia.ai)

---

## Quickstart

```bash
cp .env.example .env
# Fill in your API keys in .env

# Backend
cd backend && go run ./cmd/server

# Frontend (separate terminal)
cd frontend && npm install && npm run dev
```

Backend listens on `:8080`, frontend on `:3000`.

Point your Twilio phone number's voice webhook to `https://<your-host>/twiml`.

---

## Environment variables

See [.env.example](.env.example).

| Variable | Required | Description |
|---|---|---|
| `SONIOX_API_KEY` | ✓ | Soniox STT API key |
| `CARTESIA_API_KEY` | ✓ | Cartesia TTS API key |
| `CARTESIA_VOICE_ID` | ✓ | Cartesia voice UUID (alias: `CARTESIA_FEMALE`) |
| `GROQ_API_KEY` | ✓ | Groq LLM API key |
| `GROQ_MODEL` | — | Default: `moonshotai/gpt-oss-20b` |
| `HTTP_PORT` | — | Default: `8080` |

---

## Telemetry

The backend emits structured latency logs:

```
stt: connected          latency_ms=<ws handshake>
stt: first_final_token  latency_ms=<time to first final transcript>
llm_duration_ms         <LLM stream duration>
e2e_latency_ms          <STT final → TTS first chunk>
```

Events are also streamed to the frontend via SSE at `/events`.

---

## Deploy (Fly.io)

```bash
fly launch --no-deploy   # first time only
fly secrets set SONIOX_API_KEY=... CARTESIA_API_KEY=... CARTESIA_VOICE_ID=... GROQ_API_KEY=...
fly deploy
```

---

## Project structure

```
backend/
  cmd/server/        # entrypoint
  config/            # env loading
  internal/
    dispatcher/      # speculative audio dispatcher
    events/          # SSE hub
    llm/             # Groq streaming client
    session/         # call session lifecycle
    stt/             # Soniox WebSocket client
    tools/           # tool call simulator
    transport/       # Twilio WebSocket transport
    tts/             # Cartesia WebSocket client
  proto/sonioxpb/    # legacy gRPC stub (unused)
frontend/
  app/               # Next.js app router
  components/        # VoiceSession, TranscriptFeed, AudioPlayer
  hooks/             # useEventsSocket (SSE)
```

---

## License

MIT
