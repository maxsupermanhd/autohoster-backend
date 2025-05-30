package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	nonAlphanumericRegex = regexp.MustCompile(`[^a-zA-Z0-9 ]+`)
)

func spawnRunner(inst *instance) {
	if inst == nil {
		log.Println("Runner started with nil instance!")
		return
	}

	err := createPipes(inst)
	if err != nil {
		return
	}

	args := []string{
		inst.BinPath,
		"--configdir=" + inst.ConfDir,
		"--portmapping=0",
		"--nosound",
		"--autohost=preset.json",
		"--headless",
		"--gameport=" + fmt.Sprint(inst.Settings.GamePort),
		"--enablelobbyslashcmd",
		"--startplayers=" + fmt.Sprint(tryCfgGetD(tryGetIntGen("startPlayers"), inst.Settings.PlayerCount, inst.cfgs...)),
		"--gamelog-output=log,cmdinterface",
		"--gamelog-outputkey=playerposition",
		"--gamelog-frameinterval=" + fmt.Sprint(inst.Settings.FrameInterval),
		"--gametimelimit=" + fmt.Sprint(inst.Settings.TimeLimit),
		"--host-chat-config=quickchat",
		"--async-join-approve",
		"--enablecmdinterface=stdin",
		"--host-chat-config=quickchat",
	}
	inst.logger.Printf("Starting %q with args %#+v", inst.BinPath, args)
	pr, err := os.StartProcess(inst.BinPath, args, &os.ProcAttr{
		Dir: inst.ConfDir,
		Files: []*os.File{
			inst.stdin,
			inst.stdout,
			inst.stderr,
		},
		Sys: &syscall.SysProcAttr{
			Setsid: true,  // without it ctrl+c will be sent to wz
			Noctty: false, // if enabled it will fail with fork/exec : inappropriate ioctl for device
		},
	})
	if err != nil {
		inst.logger.Printf("Failed to start: %s", err.Error())
		return
	}
	inst.Pid = pr.Pid

	err = os.WriteFile(path.Join(inst.ConfDir, "cmdline"), append([]byte(strings.Join(args, "\x00")), 0), 0644)
	if err != nil {
		inst.logger.Println("Error writing cmdline file:", err)
	}
	err = os.WriteFile(path.Join(inst.ConfDir, "pid"), []byte(fmt.Sprint(inst.Pid)), 0644)
	if err != nil {
		inst.logger.Println("Error writing pid file:", err)
	}

	pr.Release()
	inst.logger.Printf("Started with pid %d", inst.Pid)

	inst.logger.Println("Reopening pipes...")
	err = inst.stdin.Close()
	if err != nil {
		inst.logger.Println("Error closing stdin pipe:", err)
	}
	err = inst.stdout.Close()
	if err != nil {
		inst.logger.Println("Error closing stdout pipe:", err)
	}
	err = inst.stderr.Close()
	if err != nil {
		inst.logger.Println("Error closing stderr pipe:", err)
	}
	err = openPipes(inst)
	if err != nil {
		inst.logger.Println("Error reopening pipes:", err)
	}

	instanceRunner(inst)
}

func instanceRunner(inst *instance) {
	defer func() {
		inst.logger.Printf("atomic state store: %d", int64(instanceStateExited))
		inst.state.Store(int64(instanceStateExited))
	}()
	inst.wg.Add(1)
	defer inst.wg.Done()

	err := recoverSave(inst)
	if err != nil {
		inst.logger.Printf("Failed to save instance recovery json: %s", err.Error())
	}
	var wg sync.WaitGroup

	exitchan := make(chan struct{})
	pidcheckchan := make(chan struct{})
	msgchan := make(chan string, 8192)

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			time.Sleep(1 * time.Second)
			// inst.logger.Printf("Checking pid %d", inst.Pid)
			// if inst.recovered {
			if !isPidAlive(inst.Pid) {
				inst.logger.Printf("pid %d closed", inst.Pid)
				close(pidcheckchan)
				return
			}
			// } else {
			// No way of unblocking Wait call without process exit it seems
			// pr, err := os.FindProcess(int(inst.Pid))
			// if err != nil {
			// 	inst.logger.Printf("Failed to find process: %s\n", err.Error())
			// 	continue
			// }
			// st, err := pr.Wait()
			// if err != nil {
			// 	inst.logger.Printf("Failed to wait process: %s\n", err.Error())
			// 	continue
			// }
			// if st.Exited() {
			// 	inst.logger.Printf("pid %d closed", inst.Pid)
			// 	close(pidcheckchan)
			// 	return
			// }
			// }
			// inst.logger.Printf("pid %d alive", inst.Pid)
			select {
			case <-exitchan:
				inst.logger.Printf("pid checker for %d exited", inst.Pid)
				return
			default:
			}
		}
	}()
	wg.Add(1)
	go func() {
		pokeHosterRunner(inst, exitchan)
		wg.Done()
	}()
	if inst.Origin == "requested" && instanceState(inst.state.Load()) <= instanceStateInLobby {
		wg.Add(1)
		go func() {
			runnerRoutineRequestedWatchdog(inst, exitchan)
			wg.Done()
		}()
	}
	wg.Add(1)
	go func() {
		defer inst.logger.Println("stderr reader exited")
		defer wg.Done()
		bufSize := 1024 * 1024 * 64
		buf := make([]byte, bufSize)

		for {
			s := bufio.NewScanner(inst.stderr)
			s.Buffer(buf, bufSize)
			for s.Scan() {
				msgchan <- s.Text()
			}
			if s.Err() == nil || errors.Is(s.Err(), os.ErrClosed) {
				return
			} else if errors.Is(s.Err(), os.ErrDeadlineExceeded) {
				err = inst.stderr.SetDeadline(time.Now().Add(1 * time.Minute))
				if err != nil {
					inst.logger.Println("failed to set deadline for stderr in scanner routine:", err)
					discordPostError(`failed to set deadline for stderr in scanner routine: %s\n%s`, err.Error(), string(debug.Stack()))
				}
				continue
			} else {
				inst.logger.Printf("stderr scanner exited with error %s", s.Err().Error())
				discordPostError(`failed to set deadline for stderr in scanner routine: %s\n%s`, err.Error(), string(debug.Stack()))
				return
			}
		}
	}()
	wg.Add(1)
	go func() {
		defer inst.logger.Println("stdout reader exited")
		defer wg.Done()
		bufSize := 1024 * 1024 * 64
		buf := make([]byte, bufSize)

		for {
			s := bufio.NewScanner(inst.stdout)
			s.Buffer(buf, bufSize)
			for s.Scan() {
				msgchan <- s.Text()
			}
			if s.Err() == nil || errors.Is(s.Err(), os.ErrClosed) {
				return
			} else if errors.Is(s.Err(), os.ErrDeadlineExceeded) {
				err = inst.stdout.SetDeadline(time.Now().Add(1 * time.Minute))
				if err != nil {
					inst.logger.Println("failed to set deadline for stdout in scanner routine:", err)
					discordPostError(`failed to set deadline for stdout in scanner routine: %s\n%s`, err.Error(), string(debug.Stack()))
				}
				continue
			} else {
				inst.logger.Printf("stdout scanner exited with error %s", s.Err().Error())
				discordPostError(`failed to set deadline for stdout in scanner routine: %s\n%s`, err.Error(), string(debug.Stack()))
				return
			}
		}
	}()

	pidCheckFailed := false
	shutdownOrdered := false
msgloop:
	for {
		if (pidCheckFailed || shutdownOrdered) && len(msgchan) == 0 {
			break
		}
		select {
		case <-pidcheckchan:
			inst.logger.Println("Pid check failed, closing off instance runtime")
			pidCheckFailed = true
		case cmd := <-inst.commands:
			switch cmd.command {
			case icNone:
			case icBroadcast:
				s, ok := cmd.data.(string)
				if !ok {
					inst.logger.Printf("wrong icBroadcast data type! (%t)", cmd.data)
					continue
				}
				instWriteFmt(inst, "chat bcast %s", nonAlphanumericRegex.ReplaceAllString(s, ""))
			case icShutdown:
				inst.logger.Println("exit sent")
				instWriteFmt(inst, "shutdown now")
				shutdownOrdered = true
				inst.logger.Printf("atomic state store: %d", int64(instanceStateExiting))
				inst.state.Store(int64(instanceStateExiting))
				err := recoverSave(inst)
				if err != nil {
					inst.logger.Printf("Failed to save instance recovery json: %s", err.Error())
				}
			case icRunnerStop:
				inst.logger.Println("runner stopping")
				inst.logger.Printf("atomic state store: %d", int64(instanceStateExiting))
				inst.state.Store(int64(instanceStateExiting))
				break msgloop
			default:
				inst.logger.Printf("unhandled command %#+v", cmd)
			}
		case msg := <-msgchan:
			if processHosterMessage(inst, msg) {
				inst.logger.Printf(": %q", msg)
			}
		}
	}
	inst.logger.Println("Runner cleaning up runtime...")
	close(exitchan)

	closePipes(inst)

	if !inst.recovered {
		var waitStatus syscall.WaitStatus
		var rusage syscall.Rusage
		wret, err := syscall.Wait4(inst.Pid, &waitStatus, syscall.WNOHANG, &rusage)
		if err != nil {
			inst.logger.Println("Failed to wait4:", err)
		}
		if wret != 0 && wret != inst.Pid {
			inst.logger.Printf("wait4 returned wrong pid_t, got %d but called for pid %d!", wret, inst.Pid)
		}
	}
	inst.logger.Println("Waiting for subroutines...")
	wg.Wait()
	if !pidCheckFailed && !shutdownOrdered {
		inst.logger.Println("Runner exits without archival")
		inst.logger.Printf("atomic state store: %d", int64(instanceStateExited))
		inst.state.Store(int64(instanceStateExited))
		return
	}
	if inst.GameId > 0 {
		inst.logger.Println("Runner stores replay")
		sendReplayToStorage(inst)
	}
	if len(inst.StagingGraphs) > 0 {
		inst.logger.Println("Runner flushes staging graphs")
		flushStagingGraphs(inst)
	}
	inst.logger.Println("Runner archives itself")
	err = archiveInstance(inst.ConfDir)
	if err != nil {
		inst.logger.Printf("Runner failed to archive itself: %s", err.Error())
	}
	inst.logger.Println("Runner exits")
	inst.logger.Printf("atomic state store: %d", int64(instanceStateExited))
	inst.state.Store(int64(instanceStateExited))
}

func runnerRoutineRequestedWatchdog(inst *instance, exitchan chan struct{}) {
	for {
		select {
		case <-exitchan:
			inst.logger.Printf("requested room watchdog exited for exitchan")
			return
		default:
		}
		if instanceState(inst.state.Load()) >= instanceStateInGame {
			inst.logger.Printf("requested room watchdog exited for ingame")
			return
		}
		if inst.Id+600 < time.Now().Unix() {
			// too early to kill it
			return
		}
		rs := inst.RoomStatus.DupSubTree()
		foundPlayer := false
		for slotNum := range 10 {
			if roomStatusPlayerSlotToPropertyString(rs, slotNum, "type") == "player" {
				foundPlayer = true
			}
		}
		if !foundPlayer {
			inst.logger.Printf("requested room watchdog found no players in the room and proceeds to kill it")
			discordPostError("instance %d was killed by request watchdog", inst.Id)
			select {
			case inst.commands <- instanceCommand{
				command: icShutdown,
				data:    nil,
			}:
				inst.logger.Printf("requested room watchdog sent shutdown")
			default:
				inst.logger.Printf("requested room watchdog failed to send shutdown signal")
			}
			return
		}
		time.Sleep(3 * time.Second)
	}
}

func DbLogAction(f string, args ...any) (string, error) {
	ecode := "A-" + genRandomString(14)
	msg := ecode + " " + fmt.Sprintf(f, args...)
	err := addEventLog(msg)
	return ecode, err
}

func addEventLog(msg string) error {
	tag, err := dbpool.Exec(context.Background(), `insert into eventlog (msg) values ($1)`, msg)
	if err != nil {
		return err
	}
	if !tag.Insert() {
		return errors.New("not insert return tag")
	}
	if tag.RowsAffected() != 1 {
		return errors.New("rows affected != 1")
	}
	return nil
}

func instWriteFmt(inst *instance, format string, args ...any) {
	str := "\n" + fmt.Sprintf(format, args...) + "\n"
	n, err := inst.stdin.WriteString(str)
	if err != nil {
		inst.logger.Printf("Failed to write string %q to the stdin: %s", str, err.Error())
	}
	if n != len(str) {
		inst.logger.Printf("Write to stdin n %d does not match %d", n, len(str))
	}
}

// reason := "" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░█▓░░░░░░░░░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░▓████░░░░░░░░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░██████▒░░░░░░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░▓████████████▓░░░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░▓█████████████████░░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░███████████████████░░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░█████████████████████░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░██████████████████████░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░██████████████████████░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░████████████████████░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░▓█████████████████████░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░▒█████████████████████░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░████████████████████░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░███████████████████░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░██████████████████░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░███████████████████▓░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░█████████████████░█░█░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░░████████████████▒█░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░█████░░░░░░░░░░░████████████████░░█░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░███████▒░░░░░░░░░░████████████████░░░░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░▒████████░░░░░░░░░█████████████████░░░░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░██████████████████████████████████████░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░▓██████████████████████████████████▒░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░██████████████████████████████░░░░░░░░░\\n" +
// 	"░░░░░░░░░░░░░░░░░░░░░░░░███████████████████████████████▒░░░░░░░\\n"
