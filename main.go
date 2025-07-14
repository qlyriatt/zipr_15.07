package main

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

type TaskStatus int

const (
	StatusCreated TaskStatus = iota
	StatusPending
	StatusProcessing
	StatusCompleted
	StatusPartial
	StatusError
)

func (s TaskStatus) String() string {
	status := []string{"Created", "Pending", "Processing", "Completed", "Partially Completed", "Error"}
	return status[s]
}

type Task struct {
	links   []string
	status  TaskStatus
	log     string
	archive []byte
}

var (
	ADDRESS   string
	PORT      string
	EXTS      []string
	MAX_LINKS int
	MAX_TASKS int
)

var tasks = make(map[string]*Task)

func taskHandler(w http.ResponseWriter, r *http.Request) {

	if len(tasks) >= MAX_TASKS {
		http.Error(w, "maximum number of active tasks reached", http.StatusServiceUnavailable)
		return
	}

	id := uuid.NewString()
	tasks[id] = &Task{status: StatusPending}

	json.NewEncoder(w).Encode(map[string]string{"id": id, "status": StatusCreated.String()})
}

func linksHandler(w http.ResponseWriter, r *http.Request) {

	defer r.Body.Close()

	var req struct {
		ID    string   `json:"id"`
		Links []string `json:"links"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	task, ok := tasks[req.ID]
	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.status != StatusPending {
		http.Error(w, "task does not accept more links", http.StatusBadRequest)
		return
	}

	// add links to task until MAX_LINKS, ignore the rest
	// dismiss links with unsupported extensions
	for _, l := range req.Links {
		if len(task.links) >= MAX_LINKS {
			break
		}

		for _, e := range EXTS {
			if e == path.Ext(l) {
				task.links = append(task.links, l)
				continue
			}
		}
	}

	// start fetching
	if len(task.links) >= MAX_LINKS {
		json.NewEncoder(w).Encode(map[string]string{"status": StatusProcessing.String()})
		processTask(task)
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": StatusPending.String()})
}

func statusHandler(w http.ResponseWriter, r *http.Request) {

	id := r.URL.Query().Get("id")

	task, ok := tasks[id]
	if !ok {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	if task.status == StatusCompleted {
		json.NewEncoder(w).Encode(map[string]string{
			"status": StatusCompleted.String(),
			"link":   fmt.Sprintf("%v:%v/download/%v", ADDRESS, PORT, id),
		})
		return
	}

	if task.status == StatusPartial {
		json.NewEncoder(w).Encode(map[string]string{
			"status": StatusPartial.String(),
			"log":    task.log,
			"link":   fmt.Sprintf("%v:%v/download/%v", ADDRESS, PORT, id),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{"status": task.status.String()})
}

func downloadHandler(w http.ResponseWriter, r *http.Request) {

	id := path.Base(r.URL.Path)
	task, ok := tasks[id]
	if !ok {
		http.Error(w, "wrong download link", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%v.zip"`, id))
	w.Header().Set("Content-Length", strconv.Itoa(len(task.archive)))

	_, err := w.Write(tasks[path.Base(r.URL.Path)].archive)
	if err != nil {
		log.Println(err)
		http.Error(w, "failure on archive download", http.StatusInternalServerError)
		return
	}

	// delete task after successful download
	delete(tasks, id)
}

func processTask(task *Task) {

	task.status = StatusProcessing

	var wg sync.WaitGroup
	type Fetch struct {
		filename string
		data     []byte
		err      error
	}
	fetches := make(chan Fetch, len(task.links))

	// async fetch from links
	// defers all errors until archive packing to give user info
	for _, link := range task.links {
		wg.Add(1)

		go func(link string) {
			defer wg.Done()

			data, err := fetchFile(link)
			if err != nil {
				fetches <- Fetch{filename: link, err: err}
				return
			}

			u, err := url.Parse(link)
			if err != nil {
				fetches <- Fetch{filename: link, data: data}
				return
			}

			fetches <- Fetch{filename: path.Base(u.Path), data: data}
		}(link)
	}
	wg.Wait()
	close(fetches)

	// add fetched data to archive
	// treat all archive errors as full task failure (StatusError)
	buf := new(bytes.Buffer)
	zipper := zip.NewWriter(buf)
	for fetch := range fetches {
		if fetch.err != nil {
			log.Println(fetch.err)
			task.status = StatusPartial
			task.log += fetch.err.Error()
			continue
		}

		file, err := zipper.Create(fetch.filename)
		if err != nil {
			log.Println(err)
			task.status = StatusError
			continue
		}

		_, err = file.Write(fetch.data)
		if err != nil {
			log.Println(err)
			task.status = StatusError
			continue
		}
	}
	err := zipper.Close()
	if err != nil {
		log.Println(err)
		task.status = StatusError
		return
	}

	if task.status != StatusPartial {
		task.status = StatusCompleted
	}
	task.archive = buf.Bytes()
}

func fetchFile(url string) ([]byte, error) {

	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch %v: code %v", url, resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func main() {

	err := godotenv.Load()
	if err != nil {
		log.Fatal(err)
	}

	ADDRESS = os.Getenv("ADDRESS")
	PORT = os.Getenv("PORT")
	EXTS = strings.Split(os.Getenv("EXT"), ":")
	MAX_LINKS, _ = strconv.Atoi(os.Getenv("MAX_LINKS"))
	MAX_TASKS, _ = strconv.Atoi(os.Getenv("MAX_TASKS"))

	http.HandleFunc("/task", taskHandler)
	http.HandleFunc("/links", linksHandler)
	http.HandleFunc("/status", statusHandler)
	http.HandleFunc("/download/", downloadHandler)

	log.Println("Server is running on " + ADDRESS + ":" + PORT)
	log.Fatal(http.ListenAndServe(ADDRESS+":"+PORT, nil))
}
