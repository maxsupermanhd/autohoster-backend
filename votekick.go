package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"log"
	"maps"
	"sync"
	"time"
)

type voteKickVote struct {
	targetHash string
	targetIP   string
	when       time.Time
}

var (
	voteKickLock         sync.Mutex
	voteKickVotes        = map[string]voteKickVote{}
	voteKickRestrictions = map[string]time.Time{}
)

func voteKickOnCommand(inst *instance, fromPkey []byte, fromIP string, targetHash string) {
	req := voteKickGetThreshold(inst)
	if req == 0 {
		instWriteFmt(inst, `chat bcast Votekick is disabled in this room.`)
		return
	}
	if !checkPkeyHasAccount(fromPkey) {
		instWriteFmt(inst, `chat bcast Votekick avaliable only to registered and linked players.`)
		return
	}
	if len(targetHash) < 3 {
		instWriteFmt(inst, `chat bcast Votekick player ID prefix must be at least 3 characters long`)
		return
	}
	if !stringOnlyContainsCaseInsensitive(targetHash, "0123456789abcdef") {
		instWriteFmt(inst, `chat bcast Votekick provided player ID prefix is invalid`)
		return
	}
	ip := roomLookupHash(inst, targetHash)
	if ip == "" {
		instWriteFmt(inst, `chat bcast Votekick player not found`)
		return
	}
	if ip == "multiple" {
		instWriteFmt(inst, `chat bcast Provide longer votekick player ID, collision detected`)
		return
	}
	vlt := voteKickGetVoteLifetime(inst, 600)
	bandur := voteKickGetBanDuration(inst, 600)
	voteKickLock.Lock()
	defer voteKickLock.Unlock()

	maps.DeleteFunc(voteKickVotes, func(k string, v voteKickVote) bool {
		return time.Until(v.when) <= 0
	})

	v, ok := voteKickVotes[fromIP]
	if !ok {
		voteKickVotes[fromIP] = voteKickVote{
			targetIP:   ip,
			targetHash: targetHash,
			when:       time.Now(),
		}
	} else {
		vt := time.Until(v.when.Add(vlt))
		if vt > 0 {
			vtr := vt.Round(time.Second).String()
			if vtr == "0s" {
				vtr = "1s"
			}
			instWriteFmt(inst, `chat bcast Please wait for %s to cast your next vote`, vtr)
			return
		}
		voteKickVotes[fromIP] = voteKickVote{
			targetIP:   ip,
			targetHash: targetHash,
			when:       time.Now(),
		}
	}

	hits := voteKickCheckIPNOLOCK(ip, vlt)
	instWriteFmt(inst, `chat bcast Votekick of player %s: votes %d/%d`, targetHash, hits, req)
	if hits >= req {
		instWriteFmt(inst, `ban ip %s You got votekicked. You will be able to join back in %s. If you feel like it is being abused, contact administrators.`, ip, bandur)
		instWriteFmt(inst, `unban ip %s`, ip)
		voteKickRestrictions[ip] = time.Now().Add(bandur)
	}
}

func voteKickGetVoteLifetime(inst *instance, d int) time.Duration {
	return time.Duration(tryCfgGetD(tryGetIntGen("votekick", "voteLifetimeSeconds"), d, inst.cfgs...)) * time.Second
}

func voteKickGetBanDuration(inst *instance, d int) time.Duration {
	return time.Duration(tryCfgGetD(tryGetIntGen("votekick", "banDuration"), d, inst.cfgs...)) * time.Second
}

func voteKickGetThreshold(inst *instance) int {
	return tryCfgGetD(tryGetIntGen("votekick", "voteSuccessThreshold"), 0, inst.cfgs...)
}

func voteKickCheckIPNOLOCK(targetIP string, voteDur time.Duration) int {
	hits := 0
	for _, v := range voteKickVotes {
		if v.targetIP == targetIP && time.Until(v.when.Add(voteDur)) > 0 {
			hits++
		}
	}
	return hits
}

func voteKickCheckRestricted(ip string) time.Duration {
	voteKickLock.Lock()
	defer voteKickLock.Unlock()
	maps.DeleteFunc(voteKickRestrictions, func(k string, v time.Time) bool {
		return time.Until(v) <= 0
	})
	t, ok := voteKickRestrictions[ip]
	if !ok {
		return 0
	}
	return time.Until(t)
}

func roomLookupHash(inst *instance, target string) (ip string) {
	var pl []struct {
		ip string
		pk string
	}
	err := inst.RoomStatus.GetToStruct(&pl, "players")
	if err != nil {
		log.Printf("Failed to read room status: %s", err.Error())
		return
	}
	for _, p := range pl {
		pk, err := base64.StdEncoding.DecodeString(p.pk)
		if err != nil {
			log.Printf("Failed to decode base64 pk: %s", err.Error())
			continue
		}
		hashBytes := sha256.Sum256(pk)
		if hex.EncodeToString(hashBytes[:]) == target {
			if ip == "" {
				ip = p.ip
			} else {
				ip = "multiple"
				return
			}
		}
	}
	return
}

func checkPkeyHasAccount(pkey []byte) bool {
	var hasAccount int
	err := dbpool.QueryRow(context.Background(),
		`select count(*) from accounts where id = (select account from identities where pkey = $1)`, pkey).Scan(&hasAccount)
	if err != nil {
		log.Printf("Failed to lookup account of pkey %s: %s", hex.EncodeToString(pkey), err.Error())
	}
	return hasAccount == 1
}
