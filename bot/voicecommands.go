package bot

import (
	"fmt"
	"log"
	"strings"
	"unicode"
)

// Voice-command triggers. The bot only acts when addressed by name (a
// "саня"-family token) followed by a play verb; everything after the verb is
// the song query. Stems are matched as prefixes to tolerate Russian inflection
// and Whisper's spelling wobble (сыграй/сыграйте, врубай/вруби, ...).
var (
	scNameStems  = []string{"сан", "тан", "зан"}                                                       // саня, сань, саню, саныч, ...
	scPlayStems  = []string{"сыгра", "игра", "вруб", "включ", "постав", "запуст", "плей"} // сыграй, играй, врубай, включи, поставь, запусти
	scSkipStems  = []string{"пропуст", "скип", "следующ", "дальше", "некст", "пропус"}              // пропусти, скип, следующая, дальше, некст
	scStopStems  = []string{"стоп", "останов", "хват", "выключ", "выруб"}                          // стоп, останови, хватит, выключи
	scLeaveStems = []string{"уйд", "уход", "выйд", "покин", "отключ", "свал", "съеби"}             // уйди, уходи, выйди, покинь, отключись, свали
)

// scRepeatStems matches "repeat" verbs (повтори / зацикли / репит).
var scRepeatStems = []string{"повтор", "зацикл", "репит"}

type voiceIntent int

const (
	intentNone voiceIntent = iota
	intentPlay
	intentSkip
	intentStop
	intentLeave
	intentRepeat
)

// classifyVerb maps a token to a command intent (play is checked first).
func classifyVerb(tok string) voiceIntent {
	switch {
	case hasAnyPrefix(tok, scPlayStems):
		return intentPlay
	case hasAnyPrefix(tok, scSkipStems):
		return intentSkip
	case hasAnyPrefix(tok, scStopStems):
		return intentStop
	case hasAnyPrefix(tok, scRepeatStems):
		return intentRepeat
	case hasAnyPrefix(tok, scLeaveStems):
		return intentLeave
	default:
		return intentNone
	}
}

// tokenize lowercases, strips punctuation, and splits into words.
func tokenize(s string) []string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return ' '
	}, s)
	return strings.Fields(s)
}

func hasAnyPrefix(tok string, stems []string) bool {
	for _, st := range stems {
		if strings.HasPrefix(tok, st) {
			return true
		}
	}
	return false
}

// handleVoiceCommand parses a transcript and, if it is a play command addressed
// to the bot, dispatches it. Returns true if it handled a command.
func (b *Bot) handleVoiceCommand(guildID, voiceChannelID, userID, transcript string) bool {
	tokens := tokenize(transcript)
	if len(tokens) == 0 {
		return false
	}

	// Must be addressed by name.
	nameIdx := -1
	for i, tok := range tokens {
		if hasAnyPrefix(tok, scNameStems) {
			nameIdx = i
			break
		}
	}
	if nameIdx == -1 {
		return false
	}

	// Find the first known command verb after the name.
	verbIdx, intent := -1, intentNone
	for i := nameIdx + 1; i < len(tokens); i++ {
		if it := classifyVerb(tokens[i]); it != intentNone {
			verbIdx, intent = i, it
			break
		}
	}
	if intent == intentNone {
		return false // addressed, but no recognized command
	}

	announceID := ""
	if ch, ok := announceChannels.Load(guildID); ok {
		announceID, _ = ch.(string)
	}
	report := func(m string) {
		if announceID != "" {
			_, _ = b.s.ChannelMessageSend(announceID, m)
		}
	}

	switch intent {
	case intentPlay:
		query := strings.TrimSpace(strings.Join(tokens[verbIdx+1:], " "))
		if query == "" {
			report("🎤 Что сыграть?")
			return true
		}
		mention := ""
		if userID != "" {
			mention = fmt.Sprintf("<@%s> ", userID)
		}
		log.Printf("[voice] play %q (user %q)", query, userID)
		report(fmt.Sprintf("🎤 %sищу `%s`", mention, query))
		go b.playQuery(guildID, voiceChannelID, query, announceID)

	case intentSkip:
		log.Printf("[voice] skip (user %q)", userID)
		if err := b.SkipLavalink(guildID); err != nil {
			report("Сейчас нечего пропускать.")
		} else {
			report("⏭️ Пропускаю")
		}

	case intentStop:
		log.Printf("[voice] stop (user %q)", userID)
		if err := b.StopLavalink(guildID); err != nil {
			report("Не получилось остановить: " + err.Error())
		} else {
			report("⏹️ Остановлено")
		}

	case intentRepeat:
		log.Printf("[voice] repeat (user %q)", userID)
		if b.ToggleRepeat(guildID) {
			report("🔁 Повтор текущего трека включён")
		} else {
			report("🔁 Повтор выключен")
		}

	case intentLeave:
		log.Printf("[voice] leave (user %q)", userID)
		b.leaveVoice(guildID, "👋 Вышел из канала")
	}

	return true
}
