package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/DataDog/zstd"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var (
	flInstance = flag.Int64("i", 0, "instance number")
	flLookPath = flag.String("p", "", "path to look in")
	flOutPath  = flag.String("o", ".", "path to write to")
)

func main() {
	flag.Parse()
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	if *flInstance < 1625665917 {
		log.Info().Int64("flInstance", *flInstance).Msg("too small")
		return
	}

	ar := openArchive()
	if ar == nil {
		log.Fatal().Msg("archive not found")
		return
	}
	defer ar.Close()
	log.Info().Msg("opened archive")

	extractInstanceFromArchive(ar, *flInstance, *flOutPath)

	log.Info().Msg("done")
}

func extractInstanceFromArchive(ar io.ReadCloser, iid int64, todir string) {
	var err error
	d := fmt.Sprint(iid)
	tr := tar.NewReader(ar)
	log.Info().Msg("reading tar")
	outbuf := bytes.Buffer{}
	outw := tar.NewWriter(&outbuf)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			log.Info().Msg("archive end reached")
			break
		}
		n := path.Clean(h.Name)
		if !strings.HasPrefix(strings.TrimPrefix(n, "/"), d) {
			continue
		}
		log.Info().Str("path", n).Msg("Found file belonging to the instance, extracting...")
		b, err := io.ReadAll(tr)
		if err != nil {
			log.Info().Err(err).Msg("failed to read")
			return
		}
		err = outw.WriteHeader(h)
		if err != nil {
			log.Info().Err(err).Msg("failed to write header")
			return
		}
		_, err = outw.Write(b)
		if err != nil {
			log.Info().Err(err).Msg("failed to write to mem buf")
			return
		}
	}
	err = os.MkdirAll(todir, 0766)
	log.Err(err).Str("path", todir).Msg("out dir create")
	wrbuf, err := zstd.Compress(nil, outbuf.Bytes())
	log.Err(err).Int("raw", len(outbuf.Bytes())).Int("zst", len(wrbuf)).Msg("output tar compressed")
	outfn := path.Join(todir, fmt.Sprintf("%v.tar.zst", iid))
	err = os.WriteFile(outfn, wrbuf, 0644)
	log.Err(err).Str("path", outfn).Msg("tar write")
}

func openArchive() *nestedReaderCloser {
	var archivePath string
	var f *os.File
	var err error

	archivePath = path.Join(*flLookPath, fmt.Sprint(archiveInstanceIdToWeek(*flInstance))+".tar")
	log.Info().Str("path", archivePath).Msg("checking uncompressed tar")

	f, err = os.Open(archivePath)
	if err == nil {
		return &nestedReaderCloser{
			rc1: f,
		}
	}
	if !errors.Is(err, os.ErrNotExist) {
		log.Warn().Err(err).Msg("strange error checking for file")
	}

	archivePath = path.Join(*flLookPath, fmt.Sprint(archiveInstanceIdToWeek(*flInstance))+".tar.zst")
	log.Info().Str("path", archivePath).Msg("checking compressed tar")

	f, err = os.Open(archivePath)
	if err == nil {
		return &nestedReaderCloser{
			rc1: f,
			rc2: zstd.NewReader(f),
		}
	}
	if !errors.Is(err, os.ErrNotExist) {
		log.Warn().Err(err).Msg("strange error checking for file")
	}
	return nil
}

type nestedReaderCloser struct {
	rc1 io.ReadCloser
	rc2 io.ReadCloser
}

func (nrc nestedReaderCloser) Close() error {
	if nrc.rc2 != nil {
		nrc.rc2.Close()
	}
	if nrc.rc1 != nil {
		nrc.rc1.Close()
	}
	return nil
}

func (nrc nestedReaderCloser) Read(p []byte) (n int, err error) {
	if nrc.rc2 != nil {
		return nrc.rc2.Read(p)
	}
	if nrc.rc1 != nil {
		return nrc.rc1.Read(p)
	}
	panic("both nrc readers nil")
}

func archiveInstanceIdToWeek(iid int64) int64 {
	return iid / (7 * 24 * 60 * 60)
}
