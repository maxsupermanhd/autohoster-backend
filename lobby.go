package main

import (
	"log"
	"sort"
	"time"

	"github.com/maxsupermanhd/go-wz/lobby"
)

func routineLobbyKeepalive(closechan <-chan struct{}) {
	interval := time.Duration(cfg.GetDSInt(5, "lobbyPollInterval")) * time.Second
	for {
		resp, err := lobby.LobbyLookup()
		if err != nil {
			log.Printf("Failed to lookup lobby: %s", err.Error())
		}
		log.Printf("Lobby has %d rooms", len(resp.Rooms))
		populateLobby(resp.Rooms)
		select {
		case <-closechan:
			return
		case <-time.After(interval):
		}
	}
}

func populateLobby(lr []lobby.LobbyRoom) {
	if !cfg.GetDSBool(false, "allowSpawn") {
		log.Println("Room spawning disabled")
		return
	}
	maxlobby := cfg.GetDSInt(8, "spawnCutoutLobbyRooms")
	if len(lr) >= maxlobby {
		log.Printf("Queue processing paused, too many rooms in lobby (%d >= %d)", len(lr), maxlobby)
		return
	}
	maxrunning := cfg.GetDSInt(18, "spawnCutoutRunningRooms")
	runningRooms := 0
	instancesLock.Lock()
	for _, v := range instances {
		if v.state.Load() == int64(instanceStateInGame) {
			runningRooms++
		}
	}
	instancesLock.Unlock()
	if runningRooms >= maxrunning {
		log.Printf("Queue processing paused, too many running rooms (%d >= %d)", runningRooms, maxlobby)
		return
	}

	queuesK, ok := cfg.GetKeys("queues")
	if !ok {
		log.Println("Queue processing paused, queues not defined in config")
		return
	}

	sort.Strings(queuesK)

	for _, queueName := range queuesK {
		if cfg.GetDSBool(false, "queues", queueName, "disabled") {
			continue
		}
		li := isQueueInLobby(queueName)
		if li != 0 {
			rerollMinutes := cfg.GetDInt(0, "queues", queueName, "idleRerollMinutes")
			if rerollMinutes == 0 {
				log.Printf("Queue %q in lobby with instance id %v", queueName, li)
				continue
			}
			instanceAliveFor := time.Since(time.Unix(li, 0))
			rerollDuration := time.Minute * time.Duration(rerollMinutes)
			if instanceAliveFor >= rerollDuration {
				if sendShutdownIfRerollable(li) {
					log.Printf("Queue %q in lobby with instance id %v (reroll ordered)", queueName, li)
				} else {
					log.Printf("Queue %q in lobby with instance id %v (reroll blocked)", queueName, li)
				}
			} else {
				log.Printf("Queue %q in lobby with instance id %v (reroll in %s)", queueName, li, (rerollDuration - instanceAliveFor).Round(time.Second))
			}
			continue
		}
		log.Printf("Queue %q is missing from lobby, spawning new one...", queueName)
		gi, err := generateInstance(cfg.DupSubTree("queues", queueName))
		if err != nil {
			log.Printf("Failed to generate instance: %s", err.Error())
			giid := int64(-1)
			if gi != nil {
				giid = gi.Id
				releaseInstance(gi)
			}
			discordPostError("%s Lobby queue failed to generate instance %d: %s", time.Now(), giid, err.Error())
			continue
		}
		gi.QueueName = queueName
		gi.Origin = "queue"
		// log.Printf("Generated instance: %s", spew.Sdump(gi))
		go spawnRunner(gi)
	}
}
