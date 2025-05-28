package main

import (
	"strconv"
	"time"
)

func instanceChatCommandHandlerPoke(inst *instance, args string, e chatCommandExecutor) {
	slotnum, err := strconv.Atoi(args)
	if err != nil || slotnum < 0 || slotnum > inst.Settings.PlayerCount {
		instWriteFmt(inst, `chat bcast ⚠ Poke requires numerical player slot as argument!`)
		return
	}
	select {
	case inst.pokeRequests <- slotnum:
		inst.logger.Printf("poke initiated")
	default:
		instWriteFmt(inst, `chat bcast ⚠ Failed to process poke!`)
		return
	}
}

func pokeHosterRunner(inst *instance, exitchan chan struct{}) {
	lastPoke := time.Now()
	pokeCurrentSlot := -1
	pokeCountdown := -1
	pokeTimer := time.NewTimer(1 * time.Second)
	pokeTimer.Stop()
	select {
	case <-pokeTimer.C:
	default:
	}
	slotDataIP := ""
	for {
		if instanceState(inst.state.Load()) > instanceStateInLobby {
			inst.logger.Printf("poker for %d exited because not in lobby state", inst.Pid)
			pokeTimer.Stop()
			select {
			case <-pokeTimer.C:
			default:
			}
			return
		}
		select {
		case <-exitchan:
			inst.logger.Printf("poker for %d exited to exitchan", inst.Pid)
			pokeTimer.Stop()
			select {
			case <-pokeTimer.C:
			default:
			}
			return
		case requestedSlot := <-inst.pokeRequests:
			if pokeCurrentSlot != -1 {
				instWriteFmt(inst, `chat bcast ⚠ Poke for slot %d is ongoing!`, pokeCurrentSlot)
				continue
			}
			cooldownSeconds := tryCfgGetD(tryGetIntGen("pokeCooldownSeconds"), 0, inst.cfgs...)
			if cooldownSeconds == 0 {
				instWriteFmt(inst, `chat bcast ⚠ Poke is disabled in this room!`)
				continue
			}
			sinceLastPoke := time.Since(lastPoke)
			if sinceLastPoke < time.Duration(cooldownSeconds)*time.Second {
				pokeEta := (time.Duration(cooldownSeconds)*time.Second - sinceLastPoke).Round(time.Second)
				instWriteFmt(inst, `chat bcast ⚠ Poke is on cooldown! (available in %s)`, pokeEta.String())
				continue
			}
			pokeCountdown = tryCfgGetD(tryGetIntGen("pokeCountdownSeconds"), 15, inst.cfgs...)
			if pokeCountdown < 2 {
				instWriteFmt(inst, `chat bcast ⚠ Poke is disabled in this room! (via countdown)`)
				continue
			}
			slotDataIP = roomStatusPlayerSlotToProperty(inst.RoomStatus.DupSubTree(), requestedSlot, "ip")
			if slotDataIP == "" {
				instWriteFmt(inst, `chat bcast ⚠ Poke failed to locate player.`)
				continue
			}
			pokeCurrentSlot = requestedSlot
			pokeTimer.Stop()
			select {
			case <-pokeTimer.C:
			default:
			}
			pokeTimer.Reset(time.Second)
			lastPoke = time.Now()
		case cancelIP := <-inst.pokeCancels:
			if pokeCurrentSlot == -1 {
				continue
			}
			if cancelIP == slotDataIP {
				instWriteFmt(inst, `chat bcast ⚠ Poke has been cancelled for slot %d.`, pokeCurrentSlot)
				pokeCurrentSlot = -1
				pokeCountdown = -1
				pokeTimer.Stop()
				select {
				case <-pokeTimer.C:
				default:
				}
				slotDataIP = ""
			}
		case <-pokeTimer.C:
			if pokeCurrentSlot < 0 || pokeCurrentSlot > 9 {
				inst.logger.Println("poke timer poked slot", pokeCurrentSlot, "???????")
				continue
			}
			if slotDataIP == "" {
				instWriteFmt(inst, `chat bcast ⚠ Poke lost target.`)
				inst.logger.Println("poke timer poked empty ip")
				continue
			}
			pk := roomStatusPlayerSlotToProperty(inst.RoomStatus.DupSubTree(), pokeCurrentSlot, "pk")
			if pk == "" {
				instWriteFmt(inst, `chat bcast ⚠ Poke lost target.`)
				pokeCurrentSlot = -1
				pokeCountdown = -1
				pokeTimer.Stop()
				select {
				case <-pokeTimer.C:
				default:
				}
				slotDataIP = ""
				continue
			}
			if pokeCountdown == 0 {
				slotValidateIP := roomStatusPlayerSlotToProperty(inst.RoomStatus.DupSubTree(), pokeCurrentSlot, "ip")
				if slotValidateIP != slotDataIP {
					instWriteFmt(inst, `chat bcast ⚠ Poke lost target.`)
					pokeCurrentSlot = -1
					pokeCountdown = -1
					if pokeTimer.Stop() {
						select {
						case <-pokeTimer.C:
						default:
						}
					}
					slotDataIP = ""
					continue
				}
				instWriteFmt(inst, `ban ip %s You got kicked for afk. You can rejoin immediately. If you feel like it is being abused, contact administrators.`, slotDataIP)
				instWriteFmt(inst, `unban ip %s`, slotDataIP)
				pokeCurrentSlot = -1
				pokeCountdown = -1
				pokeTimer.Stop()
				select {
				case <-pokeTimer.C:
				default:
				}
				slotDataIP = ""
				continue
			}
			if pokeCountdown == 15 || pokeCountdown == 10 || pokeCountdown == 5 || pokeCountdown == 3 || pokeCountdown == 2 || pokeCountdown == 1 {
				instWriteFmt(inst, `chat bcast ⚠ Poke will kick slot %d for being afk in %d.`, pokeCurrentSlot, pokeCountdown)
			}
			instWriteFmt(inst, `chat direct %s ⚠ Poke will kick YOU for being afk in %d seconds!`, pk, pokeCountdown)
			pokeTimer.Stop()
			select {
			case <-pokeTimer.C:
			default:
			}
			pokeTimer.Reset(time.Second)
			pokeCountdown--
		}
	}
}
