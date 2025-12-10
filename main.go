package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/todylcom/sevenzip"
)

type StripDate struct {
	time.Time
}

func (j StripDate) MarshalJSON() ([]byte, error) {
	formatted := j.Format("2006-01-02")
	return []byte(`"` + formatted + `"`), nil
}

type ComicStrip struct {
	Date StripDate `json:"date"`
	Year string    `json:"year"`
	URL  string    `json:"url"`
}

var stripsByYear map[string][]ComicStrip
var stripsByPath map[string]*sevenzip.File
var yearsList []string

func scanComics(arc *sevenzip.ReadCloser) {
	stripsByPath = make(map[string]*sevenzip.File)
	stripsByYear = make(map[string][]ComicStrip)
	yearSet := make(map[string]bool)

	for _, f := range arc.File {
		info := f.FileInfo()
		path := f.Name

		if !info.Mode().IsRegular() {
			continue
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".jpg" && ext != ".gif" {
			log.Printf("Skipping unmatched file in archive %s", path)
			continue
		}

		stripsByPath[path] = f

		dir, file := filepath.Split(path)
		dir = filepath.Clean(dir)
		year := filepath.Base(dir)
		if len(year) != 4 {
			log.Printf("Skipping file in archive %s, year folder format mismatch", path)
			continue
		}

		if len(file) < 10 || file[4] != '-' || file[7] != '-' {
			log.Printf("Skipping file in archive %s, date format mismatch", path)
			continue
		}

		dateStr := file[:10]
		t, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			log.Printf("Skipping file in archive %s, malformed date format", path)
			continue
		}

		if fmt.Sprintf("%d", t.Year()) != year {
			log.Printf("Skipping file in archive %s, year folder does not match date", path)
			continue
		}

		stripsByYear[year] = append(stripsByYear[year], ComicStrip{
			Date: StripDate{t},
			Year: year,
			URL:  "/comics/" + year + "/" + url.PathEscape(strings.Split(path, "/")[1]),
		})
		yearSet[year] = true
	}

	yearsList = make([]string, 0, len(yearSet))

	for y := range yearSet {
		yearsList = append(yearsList, y)
	}
	sort.Sort(sort.StringSlice(yearsList))

	for _, strips := range stripsByYear {
		sort.Slice(strips, func(i, j int) bool {
			return strips[i].Date.Before(strips[j].Date.Time)
		})
	}
}

func serveApp(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	if path == "/" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(indexHTML)
		return
	}

	if path == "/main.css" {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Write(mainCSS)
		return
	}
	http.NotFound(w, r)
}

func serveYearsAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(yearsList); err != nil {
		log.Printf("Error encoding years API data: %v", err)
		http.Error(w, "Error encoding data", http.StatusInternalServerError)
	}
}

func serveStripsAPI(w http.ResponseWriter, r *http.Request) {
	year := strings.TrimPrefix(r.URL.Path, "/api/strips/")

	if strips, ok := stripsByYear[year]; ok {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(strips); err != nil {
			log.Printf("Error encoding strips API data for %s: %v", year, err)
			http.Error(w, "Error encoding data", http.StatusInternalServerError)
		}
		return
	}
	http.NotFound(w, r)
}

func serveComics(w http.ResponseWriter, r *http.Request) {
	reqStrip := strings.TrimPrefix(r.URL.Path, "/comics/")
	file, found := stripsByPath[reqStrip]
	if found {
		f, err := file.Open()
		if err != nil {
			log.Printf("Unable top open comic strip %s: %v", reqStrip, err)
			http.Error(w, "Unable top open comic strip", http.StatusInternalServerError)
			return
		}
		defer f.Close()

		_, err = io.Copy(w, f)
		if err != nil {
			log.Printf("Unable top serve comic strip %s: %v", reqStrip, err)
			http.Error(w, "Unable top serve comic strip", http.StatusInternalServerError)
			return
		}
	} else {
		http.NotFound(w, r)
	}
}

func main() {
	var dilbertArc string
	var port uint

	flag.StringVar(&dilbertArc, "archive", "Dilbert_1989-2023_complete.7z", "Path to dilbert archive")
	flag.UintVar(&port, "port", 8080, "Port to listen on")
	flag.Parse()

	arc, err := sevenzip.OpenReader(dilbertArc)
	if err != nil {
		log.Printf("Unable to open archive: %v", err)
		os.Exit(1)
	}
	defer arc.Close()

	scanComics(arc)

	if len(yearsList) == 0 {
		log.Println("No comic strips were found in archive")
		os.Exit(1)
	}

	http.HandleFunc("/api/years", serveYearsAPI)

	http.HandleFunc("/api/strips/", serveStripsAPI)

	http.HandleFunc("/comics/", serveComics)

	http.HandleFunc("/", serveApp)

	log.Printf("Serving %d comic strips at port %d", len(stripsByPath), port)
	if err := http.ListenAndServe(":"+strconv.FormatUint(uint64(port), 10), nil); err != nil {
		log.Printf("Failed to start webserver: %v", err)
		os.Exit(1)
	}
}

//go:embed frontend/src/index.html
var indexHTML []byte

//go:embed frontend/src/main.css
var mainCSS []byte
