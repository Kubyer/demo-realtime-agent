import re

with open("internal/session/session.go", "r") as f:
    code = f.read()

# 1. Config
code = re.sub(
    r"CartesiaWSURL     string\n\tCalendlyAPIKey  string\n\tDB              \*sql\.DB",
    "CartesiaWSURL     string\n\tGradiumAPIKey     string\n\tCalendlyAPIKey    string\n\tDB                *sql.DB",
    code
)

# 2. Add recorder to Session
code = re.sub(
    r"calls      CallStorer\n\tlog        \*slog\.Logger\n}",
    "calls      CallStorer\n\trecorder   *Recorder\n\tlog        *slog.Logger\n}",
    code
)

# 3. Add recorder init
code = re.sub(
    r"settings := GetSettings\(\)\n\n\tsim :=",
    "settings := GetSettings()\n\trecorder := NewRecorder(cfg.Source)\n\n\tsim :=",
    code
)

# 4. Remove Google, add Gradium, Cartesia, Elevenlabs
code = re.sub(
    r"var ttsClient tts\.Client\n\tif settings\.VoiceProvider == \"cartesia\" \{\n\t\tttsClient = tts\.NewCartesiaClient\(cfg\.CartesiaWSURL, cfg\.CartesiaAPIKey, settings\.VoiceID, log\)\n\t\} else \{\n\t\tttsClient = tts\.NewElevenLabsClient\(cfg\.ElevenLabsAPIKey, settings\.VoiceID, settings\.VoiceModel, log\)\n\t\}",
    """var ttsClient tts.Client
	if settings.VoiceProvider == "cartesia" {
		ttsClient = tts.NewCartesiaClient(cfg.CartesiaWSURL, cfg.CartesiaAPIKey, settings.VoiceID, log)
	} else if settings.VoiceProvider == "gradium" {
		ttsClient = tts.NewGradiumClient(cfg.GradiumAPIKey, settings.VoiceID, log)
	} else {
		ttsClient = tts.NewElevenLabsClient(cfg.ElevenLabsAPIKey, settings.VoiceID, settings.VoiceModel, log)
	}""",
    code
)

# 5. Populate s.recorder
code = re.sub(
    r"calls:\      calls,\n\t\tlog:\        log,\n\t\}",
    "calls:      calls,\n\t\trecorder:   recorder,\n\t\tlog:        log,\n\t}",
    code
)

# 6. Save recording
code = re.sub(
    r"s\.hub\.BroadcastSessionEnd\(s\.ID\)\n\t\ts\.calls\.End\(s\.ID\)\n\t\}\(\)",
    "s.hub.BroadcastSessionEnd(s.ID)\n\t\ts.calls.End(s.ID)\n\t\ts.recorder.Save(s.ID)\n\t}()",
    code
)

# 7. Record user
code = re.sub(
    r"audioCh, err := s\.transport\.ReadStream\(ctx\)\n\tif err != nil \{\n\t\ts\.log\.Error\(\"session: ReadStream failed\", \"err\", err, \"session_id\", s\.ID\)\n\t\treturn\n\t\}",
    """audioCh, err := s.transport.ReadStream(ctx)
	if err != nil {
		s.log.Error("session: ReadStream failed", "err", err, "session_id", s.ID)
		return
	}
	var recordCh <-chan []byte
	audioCh, recordCh = teeAudio(ctx, audioCh)
	go func() {
		for chunk := range recordCh {
			s.recorder.WriteUser(chunk)
		}
	}()""",
    code
)

# 8. AppendTurn -> TurnEntry
code = re.sub(r"calls\.AppendTurn\(id, \"tool_call\", payload\)", "calls.AppendTurn(id, TurnEntry{Role: \"tool_call\", Text: payload, AudioStartMs: recorder.OffsetMs()})", code)
code = re.sub(r"calls\.AppendTurn\(id, \"tool_result\", payload\)", "calls.AppendTurn(id, TurnEntry{Role: \"tool_result\", Text: payload, AudioStartMs: recorder.OffsetMs()})", code)
code = re.sub(r"s\.calls\.AppendTurn\(s\.ID, \"assistant\", currentSettings\.OpeningSentence\)", "s.calls.AppendTurn(s.ID, TurnEntry{Role: \"assistant\", Text: currentSettings.OpeningSentence, AudioStartMs: s.recorder.OffsetMs()})", code)
code = re.sub(r"s\.calls\.AppendTurn\(s\.ID, \"user\", result\.Text\)", "s.calls.AppendTurn(s.ID, TurnEntry{Role: \"user\", Text: result.Text, AudioStartMs: s.recorder.OffsetMs()})", code)

# 9. runTTSTurn signature
code = re.sub(r"func \(s \*Session\) runTTSTurn\(ctx context\.Context, sentenceCh <-chan string, chunkID string, tSTTFinal time\.Time\) \{", "func (s *Session) runTTSTurn(ctx context.Context, sentenceCh <-chan string, chunkID string, tSTTFinal time.Time, ttfaMs *int64) {", code)
code = re.sub(r"go s\.runTTSTurn\(ctx, sentenceCh, chunkID, time\.Now\(\)\)", "go s.runTTSTurn(ctx, sentenceCh, chunkID, time.Now(), nil)", code)
code = re.sub(r"go s\.runTTSTurn\(ctx, sentenceCh, chunkID, tSTTFinal\)", "go s.runTTSTurn(ctx, sentenceCh, chunkID, tSTTFinal, &ttfaMs)", code)

# 10. Update runTTSTurn to write assistant and set ttfaMs
code = re.sub(
    r"tTTSConnected := time\.Now\(\)\n\ts\.log\.Info\(\"session: TTS connected\",",
    """tTTSConnected := time.Now()
	ms := tTTSConnected.Sub(tSTTFinal).Milliseconds()
	s.hub.BroadcastMetric(chunkID, "tts_latency", float64(ms))
	if ttfaMs != nil {
		import_atomic_or_something_nah_ill_just_use_pointers_if_i_can = true
		*ttfaMs = ms
	}
	
	var recordCh <-chan []byte
	audioCh, recordCh = teeAudio(ctx, audioCh)
	go func() {
		for chunk := range recordCh {
			s.recorder.WriteAssistant(chunk)
		}
	}()

	s.log.Info("session: TTS connected", """,
    code
)

# 11. LLM Stream interceptor and ttfaMs logic
code = re.sub(
    r"			// sentenceCh carries text from the LLM to the TTS pipeline\.\n\t\t\tsentenceCh := make\(chan string, 4\)\n\t\t\tchunkID := s\.hub\.NextChunkID\(\)\n\n\t\t\t// TTS goroutine: consume sentenceCh → Cartesia → dispatcher\.\n\t\t\tgo s\.runTTSTurn\(ctx, sentenceCh, chunkID, tSTTFinal, &ttfaMs\)\n\n\t\t\t// Filler audio \(priority 2\) dispatched immediately while LLM thinks\.\n\t\t\tgo s\.playFiller\(ctx, chunkID\+\"-filler\"\)\n\n\t\t\t// LLM stream — sends sentences to sentenceCh; blocks until done\.\n\t\t\ttLLMStart := time\.Now\(\)\n\t\t\tif err := s\.llm\.StreamLoop\(ctx, history, sentenceCh\); err != nil && ctx\.Err\(\) == nil \{\n\t\t\t\ts\.log\.Error\(\"session: LLM error\", \"err\", err, \"session_id\", s\.ID\)\n\t\t\t\}\n\t\t\ts\.log\.Info\(\"session: LLM turn complete\",\n\t\t\t\t\"llm_duration_ms\", time\.Since\(tLLMStart\)\.Milliseconds\(\),\n\t\t\t\t\"session_id\", s\.ID,\n\t\t\t\)\n\t\t\tclose\(sentenceCh\)\n\n\t\t\t// Record the assistant turn from the last history entry\.\n\t\t\ts\.recordAssistantTurn\(history\)",
    """			// sentenceCh carries text from the LLM to the TTS pipeline.
			sentenceCh := make(chan string, 4)
			rawCh := make(chan string, 16)
			chunkID := s.hub.NextChunkID()
			
			var ttfaMs int64
			startIdx := len(history.Snapshot())

			// TTS goroutine: consume sentenceCh → Cartesia → dispatcher.
			go s.runTTSTurn(ctx, sentenceCh, chunkID, tSTTFinal, &ttfaMs)

			// Filler audio (priority 2) dispatched immediately while LLM thinks.
			go s.playFiller(ctx, chunkID+"-filler")

			// Intercept stream to broadcast tokens immediately
			go func() {
				defer close(sentenceCh)
				for text := range rawCh {
					sentenceCh <- text
					s.hub.BroadcastFinal(chunkID, text) // Progressive broadcast
				}
			}()

			// LLM stream — sends sentences to rawCh; blocks until done.
			tLLMStart := time.Now()
			if err := s.llm.Stream(ctx, history, rawCh); err != nil && ctx.Err() == nil {
				s.log.Error("session: LLM error", "err", err, "session_id", s.ID)
			}
			close(rawCh)
			
			// Wait a bit to let TTS update ttfaMs if very fast
			time.Sleep(10 * time.Millisecond)

			s.log.Info("session: LLM turn complete",
				"llm_duration_ms", time.Since(tLLMStart).Milliseconds(),
				"session_id", s.ID,
			)

			// Record the assistant turn from the last history entry.
			s.recordAssistantTurn(history, chunkID, startIdx, ttfaMs)""",
    code
)

# 12. Update recordAssistantTurn
code = re.sub(
    r"func \(s \*Session\) recordAssistantTurn\(history \*llm\.History\) \{\n\tmsgs := history\.Snapshot\(\)\n\tfor i := len\(msgs\) - 1; i >= 0; i-- \{\n\t\tm := msgs\[i\]\n\t\tif m\.Role == openai\.ChatMessageRoleAssistant && m\.Content != \"\" \{\n\t\t\ts\.calls\.AppendTurn\(s\.ID, \"assistant\", m\.Content\)\n\t\t\treturn\n\t\t\}\n\t\}\n\}",
    """func (s *Session) recordAssistantTurn(history *llm.History, chunkID string, startIdx int, ttfaMs int64) {
	msgs := history.Snapshot()
	for i := len(msgs) - 1; i >= startIdx; i-- {
		m := msgs[i]
		if m.Role == openai.ChatMessageRoleAssistant && m.Content != "" {
			var lat *int64
			if ttfaMs != 0 {
				latMs := ttfaMs
				lat = &latMs
			}
			s.calls.AppendTurn(s.ID, TurnEntry{Role: "assistant", Text: m.Content, AudioStartMs: s.recorder.OffsetMs(), TTSLatency: lat})
			return
		}
	}
}""",
    code
)

with open("internal/session/session.go", "w") as f:
    f.write(code)

