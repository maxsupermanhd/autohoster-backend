package main

import (
	gamereport "autohoster-backend/gameReport"
	"context"
	"flag"
	"log"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v4/pgxpool"
)

var (
	gid        = flag.Int("gid", -1, "game id")
	connString = flag.String("connString", "", "database connection string")
	dbpool     *pgxpool.Pool
)

func main() {
	flag.Parse()
	if *gid < 1 {
		log.Println("gid is wrong")
		return
	}
	log.Println("connecting to db")
	dbpool = noerr(pgxpool.Connect(context.Background(), *connString))
	log.Println("fetching")
	var grfrom []gamereport.GameReportGraphFrame
	must(dbpool.QueryRow(context.Background(), `select graphs from games where id = $1`, *gid).Scan(&grfrom))
	log.Println("fetched ", len(grfrom))
	grto := []gamereport.GameReportGraphFrame{}
	lastPrint := time.Now()
	for i, v := range grfrom {
		if time.Since(lastPrint) > time.Second {
			log.Println("processing ", i, len(grto), (float64(i)/float64(len(grfrom)))*100, (float64(len(grto))/float64(len(grfrom)))*100)
		}
		f := true
		for _, v2 := range grto {
			if v2.GameTime == v.GameTime {
				f = false
			}
		}
		if f {
			grto = append(grto, v)
		}
	}
	log.Println("updating")
	tag := noerr(dbpool.Exec(context.Background(), `update games set graphs = $1::json where id = $2`, grto, *gid))
	log.Println(tag.String())
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
