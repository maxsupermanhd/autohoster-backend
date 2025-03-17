package main

import (
	"maps"
	"slices"
	"sync"
	"time"
)

type spamCounterMessage struct {
	msg  string
	when time.Time
}

var (
	chatSpamCountersLock = sync.Mutex{}
	chatSpamCounters     = map[string][]spamCounterMessage{}
	chatSpamMutesLock    = sync.Mutex{}
	chatSpamMutes        = map[string]time.Time{}
)

func chatSpamIsMuted(inst *instance, ip string) bool {
	mDur := time.Duration(tryCfgGetD(tryGetIntGen("antiSpamMuteSeconds"), 60*30, inst.cfgs...)) * time.Second
	if mDur == 0 {
		return false
	}
	now := time.Now()
	chatSpamMutesLock.Lock()
	defer chatSpamMutesLock.Unlock()
	maps.DeleteFunc(chatSpamMutes, func(k string, v time.Time) bool {
		return now.Sub(v) >= mDur
	})
	_, ok := chatSpamMutes[ip]
	return ok
}

func chatSpamHit(inst *instance, content, ip, key64 string) {
	tWindowDur := time.Duration(tryCfgGetD(tryGetIntGen("antiSpamWindowSeconds"), 3, inst.cfgs...)) * time.Second
	tWindowHits := tryCfgGetD(tryGetIntGen("antiSpamWindowHits"), 4, inst.cfgs...)

	if tWindowDur == 0 || tWindowHits == 0 {
		return
	}

	chatSpamCountersLock.Lock()
	defer chatSpamCountersLock.Unlock()

	msg := spamCounterMessage{msg: content, when: time.Now()}

	s, ok := chatSpamCounters[ip]
	if !ok {
		chatSpamCounters[ip] = []spamCounterMessage{msg}
		return
	}

	now := time.Now()
	s = slices.DeleteFunc(append(s, msg), func(m spamCounterMessage) bool {
		return now.Sub(m.when) > tWindowDur
	})

	chatSpamCounters[ip] = s

	hits := 0
	for _, v := range s {
		m := v.msg
		if len(m) <= 3 {
			hits++
			continue
		}
		freq := map[rune]int{}
		for _, r := range m {
			freq[r]++
		}
		if len(freq) == 1 {
			hits++
		}
	}

	if hits >= tWindowHits {
		chatSpamMutesLock.Lock()
		chatSpamMutes[ip] = time.Now()
		chatSpamMutesLock.Unlock()
		instWriteFmt(inst, `set chat mute %s`, key64)
		instWriteFmt(inst, `chat direct %s %s`, key64, "⚠ You were automatically muted for chat spam.")
		instWriteFmt(inst, `chat bcast ⚠ Please do not spam, it does not help anyone.`)
	}

}
