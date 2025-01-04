package main

import (
	"archive/tar"
	gamereport "autohoster-backend/gameReport"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/DataDog/zstd"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
)

var (
	archiveDirPath = flag.String("archivesDir", "./run/archive/", "path to directory with archives")
	gid            = flag.Int("gid", -1, "game id")
	instanceId     = flag.Int64("instance", -1, "instance id")
	connString     = flag.String("connString", "", "database connection string")
	dbpool         *pgxpool.Pool
)

func main() {
	flag.Parse()
	if *gid < 1 {
		log.Println("gid is wrong")
		return
	}
	if *instanceId < 1 {
		log.Println("instance is wrong")
		return
	}
	dbpool = noerr(pgxpool.Connect(context.Background(), *connString))
	archiveId := archiveInstanceIdToWeek(*instanceId)
	log.Printf("Looking for instance %d in archive %d", *instanceId, archiveId)
	archiveNameNormal := fmt.Sprintf("%d.tar", archiveId)
	archivePathNormal := path.Join(*archiveDirPath, archiveNameNormal)
	archiveNameCompressed := fmt.Sprintf("%d.tar.zst", archiveId)
	archivePathCompressed := path.Join(*archiveDirPath, archiveNameCompressed)
	st, err := os.Stat(archivePathNormal)
	if err == nil && !st.IsDir() {
		log.Printf("Found %q", archivePathNormal)
		processArchive(archivePathNormal)
		return
	}
	st, err = os.Stat(archivePathCompressed)
	if err == nil && !st.IsDir() {
		log.Printf("Found %q", archivePathCompressed)
		processCompressedArchive(archivePathCompressed)
		return
	}
	log.Println("archive ", archivePathNormal, " not found")
}

func processArchive(p string) {
	f := noerr(os.Open(p))
	processTar(f)
	f.Close()
}

func processCompressedArchive(p string) {
	f := noerr(os.Open(p))
	r := zstd.NewReader(f)
	processTar(r)
	r.Close()
	f.Close()
}

func processTar(f io.Reader) {
	r := tar.NewReader(f)
	for {
		h, err := r.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				log.Println("end of archive reached")
				return
			}
			must(err)
		}
		if !strings.HasPrefix(h.Name, "/"+strconv.Itoa(int(*instanceId))) && !strings.HasPrefix(h.Name, strconv.Itoa(int(*instanceId))) {
			continue
		}
		processInstanceGameLog(r, h)
		processInstanceReplay(r, h)
	}
}

func processInstanceReplay(r *tar.Reader, h *tar.Header) {
	fb := path.Base(h.Name)
	if !strings.HasSuffix(fb, "_multiplay_p13.wzrp") {
		return
	}
	buf := noerr(io.ReadAll(r))
	if len(buf) != int(h.Size) {
		log.Printf("tar read %q i %d != size %d", h.Name, len(buf), h.Size)
	}
	log.Printf("processing game replay %q of size %d", h.Name, h.Size)
	processGameReplay(buf)
}

func processInstanceGameLog(r *tar.Reader, h *tar.Header) {
	fb := path.Base(h.Name)
	if !strings.HasPrefix(fb, "gamelog_") {
		return
	}
	if !strings.HasSuffix(fb, ".log") {
		return
	}
	buf := noerr(io.ReadAll(r))
	if len(buf) != int(h.Size) {
		log.Printf("tar read %q i %d != size %d", h.Name, len(buf), h.Size)
	}
	log.Printf("processing game log %q of size %d", h.Name, h.Size)
	processGameLog(string(buf))
}

func processGameReplay(buf []byte) {
	replayCompressed := noerr(zstd.CompressLevel(nil, buf, 19))
	tag := noerr(dbpool.Exec(context.Background(), `update games set replay = $1 where id = $2`, replayCompressed, *gid))
	log.Println("replay ", tag.String())
}

func processGameLog(g string) {
	gs := strings.Split(g, "\n")
	for i, v := range gs {
		if strings.HasPrefix(v, "__REPORTextended__") && strings.HasSuffix(v, "__ENDREPORTextended__") {
			v = strings.TrimPrefix(v, "__REPORTextended__")
			v = strings.TrimSuffix(v, "__ENDREPORTextended__")
			rpt := gamereport.GameReportExtended{}
			log.Printf("Extended report found at line %d, len %d", i, len(v))
			must(json.Unmarshal([]byte(v), &rpt))
			submitGameEnd(rpt)
		}
	}
}

func submitGameEnd(report gamereport.GameReportExtended) {
	err := dbpool.BeginFunc(context.Background(), func(tx pgx.Tx) error {
		for _, v := range report.PlayerData {
			_, err := dbpool.Exec(context.Background(), `update players set usertype = $1, props = $2 where game = $3 and position = $4`,
				v.Usertype, v.GameReportPlayerStatistics, *gid, v.Position)
			if err != nil {
				log.Printf("Failed to finalize player at position %d: %s (gid %d)", v.Position, err.Error(), *gid)
				return err
			}
		}
		_, err := dbpool.Exec(context.Background(), `update games set research_log = $1, time_ended = TO_TIMESTAMP($2::double precision / 1000), game_time = $3 where id = $4`,
			report.ResearchComplete, report.EndDate, report.GameTime, *gid)
		if err != nil {
			log.Printf("Failed to finalize game: %s (gid %d)", err.Error(), *gid)
		}
		return err
	})
	if err != nil {
		log.Printf("Failed to finalize: %s (gid %d)", err.Error(), *gid)
	}
}

func archiveInstanceIdToWeek(num int64) int64 {
	return num / (7 * 24 * 60 * 60)
}

func must(err error) {
	if err != nil {
		debug.PrintStack()
		log.Fatal(err)
	}
}

func noerr[T any](ret T, err error) T {
	must(err)
	return ret
}
