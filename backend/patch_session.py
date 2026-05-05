import re

with open("internal/session/session.go", "r") as f:
    code = f.read()

# 1. Fix date injection in GetSettings / NewHistory
# Wait, I'll inject the date directly where history is initialized.
new_history_block = """
	// Snapshot settings at session start.
	currentSettings := GetSettings()
	
	// Inject current date
	now := time.Now()
	days := []string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"}
	months := []string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"}
	dateStr := fmt.Sprintf("%s %d %s %d", days[now.Weekday()], now.Day(), months[now.Month()-1], now.Year())
	timeStr := now.Format("15:04")
	promptWithDate := currentSettings.Prompt + "\\n\\n# DATE ET HEURE ACTUELLES\\nNous sommes le " + dateStr + " et il est " + timeStr + ". Utilise cette information si l'utilisateur parle de dates."
	
	history := llm.NewHistory(promptWithDate)
"""

code = re.sub(
    r"// Snapshot settings at session start\.\n\tcurrentSettings := GetSettings\(\)\n\thistory := llm\.NewHistory\(currentSettings\.Prompt\)",
    new_history_block,
    code
)


# 2. Add atomic import
code = re.sub(
    r'"sync"',
    '"sync"\n\t"sync/atomic"',
    code
)

# 3. Use atomic.Int64 for ttfaMs
code = re.sub(
    r"var ttfaMs int64\n\t\t\tstartIdx := len\(history\.Snapshot\(\)\)\n\n\t\t\t// TTS goroutine: consume sentenceCh → Cartesia → dispatcher\.\n\t\t\tgo s\.runTTSTurn\(ctx, sentenceCh, chunkID, tSTTFinal, &ttfaMs\)",
    """var ttfaMs atomic.Int64
			startIdx := len(history.Snapshot())

			// TTS goroutine: consume sentenceCh → Cartesia → dispatcher.
			go s.runTTSTurn(ctx, sentenceCh, chunkID, tSTTFinal, &ttfaMs)""",
    code
)

# 4. Modify progressive streaming logic to capture TTFT and broadcast MetricsPayload properly
code = re.sub(
    r"			// Intercept stream to broadcast tokens immediately\n\t\t\tgo func\(\) \{\n\t\t\t\tdefer close\(sentenceCh\)\n\t\t\t\tfor text := range rawCh \{\n\t\t\t\t\tsentenceCh <- text\n\t\t\t\t\ts\.hub\.BroadcastFinal\(chunkID, text, \"assistant\"\)\n\t\t\t\t\}\n\t\t\t\}\(\)\n\n\t\t\t// LLM stream — sends sentences to rawCh; blocks until done\.\n\t\t\ttLLMStart := time\.Now\(\)\n\t\t\tif err := s\.llm\.StreamLoop\(ctx, history, rawCh\); err != nil && ctx\.Err\(\) == nil \{\n\t\t\t\ts\.log\.Error\(\"session: LLM error\", \"err\", err, \"session_id\", s\.ID\)\n\t\t\t\}\n\t\t\tclose\(rawCh\)\n\t\t\t\n\t\t\t// Wait a bit to let TTS update ttfaMs if very fast\n\t\t\ttime\.Sleep\(10 \* time\.Millisecond\)\n\n\t\t\ts\.log\.Info\(\"session: LLM turn complete\",\n\t\t\t\t\"llm_duration_ms\", time\.Since\(tLLMStart\)\.Milliseconds\(\),\n\t\t\t\t\"session_id\", s\.ID,\n\t\t\t\)\n\n\t\t\t// Record the assistant turn from the last history entry\.\n\t\t\ts\.recordAssistantTurn\(history, chunkID, startIdx, ttfaMs\)",
    """			// Intercept stream to broadcast tokens immediately
			go func() {
				defer close(sentenceCh)
				var fullText string
				var firstToken bool
				var ttftMs int64
				for text := range rawCh {
					if !firstToken {
						firstToken = true
						ttftMs = time.Since(tSTTFinal).Milliseconds()
					}
					sentenceCh <- text
					fullText += text
					s.hub.BroadcastPlaying(chunkID, fullText, "assistant") // Progressive broadcast
				}
				
				// Wait a bit for TTS to set TTFA
				for i := 0; i < 50; i++ {
					if ttfaMs.Load() != 0 {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				finalTtfa := ttfaMs.Load()
				
				s.hub.BroadcastMetrics(events.MetricsPayload{
					TTFTMs: ttftMs,
					TTFAMs: finalTtfa,
					E2EMs:  finalTtfa,
				})
				
				s.hub.BroadcastFinal(chunkID, fullText, "assistant")
			}()

			// LLM stream — sends sentences to rawCh; blocks until done.
			tLLMStart := time.Now()
			if err := s.llm.StreamLoop(ctx, history, rawCh); err != nil && ctx.Err() == nil {
				s.log.Error("session: LLM error", "err", err, "session_id", s.ID)
			}
			close(rawCh)

			s.log.Info("session: LLM turn complete",
				"llm_duration_ms", time.Since(tLLMStart).Milliseconds(),
				"session_id", s.ID,
			)

			// Record the assistant turn from the last history entry.
			s.recordAssistantTurn(history, chunkID, startIdx, &ttfaMs)""",
    code
)

# 5. Fix runTTSTurn to accept atomic and set it
code = re.sub(
    r"func \(s \*Session\) runTTSTurn\(ctx context\.Context, sentenceCh <-chan string, chunkID string, tSTTFinal time\.Time, ttfaMs \*int64\) \{",
    "func (s *Session) runTTSTurn(ctx context.Context, sentenceCh <-chan string, chunkID string, tSTTFinal time.Time, ttfaMs *atomic.Int64) {",
    code
)

code = re.sub(
    r"if ttfaMs != nil \{\n\t\t\*ttfaMs = ms\n\t\}",
    """if ttfaMs != nil {
		ttfaMs.Store(ms)
	}""",
    code
)

# 6. Modify recordAssistantTurn to accept atomic pointer
code = re.sub(
    r"func \(s \*Session\) recordAssistantTurn\(history \*llm\.History, chunkID string, startIdx int, ttfaMs int64\) \{",
    "func (s *Session) recordAssistantTurn(history *llm.History, chunkID string, startIdx int, ttfaMs *atomic.Int64) {",
    code
)

code = re.sub(
    r"if ttfaMs != 0 \{\n\t\t\t\tlatMs := ttfaMs\n\t\t\t\tlat = &latMs\n\t\t\t\}",
    """latVal := ttfaMs.Load()
			if latVal != 0 {
				lat = &latVal
			}""",
    code
)

# Also fix the initial TTSTurn call for opening sentence
code = re.sub(
    r"go s\.runTTSTurn\(ctx, sentenceCh, chunkID, time\.Now\(\), nil\)",
    """var dummyTtfa atomic.Int64
		go s.runTTSTurn(ctx, sentenceCh, chunkID, time.Now(), &dummyTtfa)""",
    code
)

with open("internal/session/session.go", "w") as f:
    f.write(code)

