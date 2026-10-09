package main

import (
    "strings"
    "time"
    "unicode"
    "unicode/utf8"
)

// beginTTSReply / endTTSReply delimit one assistant turn using the server's
// tts.start and tts.stop messages. Subtitles are emitted per sentence fragment,
// not per full answer, so one dashboard event must collect all fragments.
func (d *Dashboard) beginTTSReply() {
    d.mu.Lock()
    d.ttsInProgress = true
    d.ttsEventIndex = -1
    d.mu.Unlock()
}

func (d *Dashboard) endTTSReply() {
    d.mu.Lock()
    d.ttsInProgress = false
    d.ttsEventIndex = -1
    d.mu.Unlock()
}

func firstIsDigit(text string) bool {
    ch, _ := utf8.DecodeRuneInString(text)
    return unicode.IsDigit(ch)
}

// A TTS sentence break after a decimal comma/dot must not turn "97,"
// and "58 triệu" into "97, 58 triệu". Other chunks are separated by spaces.
func joinTTSChunks(previous, fragment string) string {
    a, b := strings.TrimSpace(previous), strings.TrimSpace(fragment)
    if a == "" { return b }
    if b == "" { return a }
    if firstIsDigit(b) && (strings.HasSuffix(a, ".") || strings.HasSuffix(a, ",")) {
        return a + b
    }
    if strings.ContainsRune(".,;:!?%)", []rune(b)[0]) || strings.HasSuffix(a,"(") {
        return a + b
    }
    return a + " " + b
}

func (d *Dashboard) trimEventHistoryLocked() {
    if excess := len(d.events)-120; excess > 0 {
        d.events = d.events[excess:]
        if d.ttsInProgress {
            d.ttsEventIndex -= excess
            if d.ttsEventIndex < 0 { d.ttsEventIndex = -1 }
        }
    }
}

// appendTTSSentence updates one history row in-place for the whole TTS turn.
// This preserves the existing /api/status event JSON shape for old clients.
func (d *Dashboard) appendTTSSentence(fragment string) {
    fragment = strings.TrimSpace(fragment)
    if fragment == "" { return }
    d.mu.Lock()
    defer d.mu.Unlock()
    if d.ttsInProgress && d.ttsEventIndex >= 0 && d.ttsEventIndex < len(d.events) &&
       d.events[d.ttsEventIndex].Kind == "tts" {
        e := &d.events[d.ttsEventIndex]
        e.Text = joinTTSChunks(e.Text, fragment)
        return
    }
    d.events = append(d.events, dashboardEvent{
        Kind: "tts", Text: fragment, Time: time.Now().Format("15:04:05"),
    })
    d.trimEventHistoryLocked()
    if d.ttsInProgress {
        d.ttsEventIndex = len(d.events)-1
    }
}
