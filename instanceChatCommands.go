package main

import (
	"slices"
	"strings"
)

type chatCommandExecutor struct {
	hash         string
	name         string
	ip           string
	publicKey    []byte
	publicKeyB64 string
}

func instanceChatCommandHandle(inst *instance, msg string, invhash, invb64pkey string, invpkey []byte, invname, invip string) {
	e := chatCommandExecutor{
		hash:         invhash,
		name:         invname,
		ip:           invip,
		publicKey:    invpkey,
		publicKeyB64: invb64pkey,
	}
	cmd, args := popWord(msg)
	if cmd == "/stat" || cmd == "/stats" {
		instWriteFmt(inst, `chat bcast Game history of Autohoster is available at the website: https://wz2100-autohost.net/games (with detailed statistics, charts and replay for download)`)
	} else if cmd == "/hostmsg" {
		processLinkingMessage(inst, args, e)
	} else if cmd == "/votekick" {
		voteKickOnCommand(inst, args, e)
	} else if cmd == "/set" {
		instanceChatCommandHandlerSet(inst, args, e)
	} else if cmd == "/poke" {
		instanceChatCommandHandlerPoke(inst, args, e)
	} else if cmd == "/help" {
		instanceChatCommandHandlerHelp(inst, args, e)
	}
}

func instanceChatCommandHandlerHelp(inst *instance, args string, e chatCommandExecutor) {
	instWriteFmt(inst, `chat direct %s /votekick (Player ID: first 3 symbols) - initiates votekick for identity`, e.publicKeyB64)
	instWriteFmt(inst, `chat direct %s /set ... - [admin] configure the room preferences`, e.publicKeyB64)
}

func popWord(msg string) (part, rem string) {
	part, rem, _ = strings.Cut(msg, " ")
	rem = strings.TrimSpace(rem)
	return
}

func isHashInstanceAdmin(inst *instance, playerhash string) bool {
	return slices.Contains(inst.Admins, playerhash)
}
