package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const url = "http://localhost:8080"

const usage = `Usage:
	task
	links <id> <link1> <link2> ...
	status <id>`

func main() {

	fmt.Println(usage)
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {

		inputs := strings.Split(strings.TrimSpace(scanner.Text()), " ")
		if len(inputs) == 0 {
			continue
		}

		switch inputs[0] {
		case "task":
			task()
		case "links":
			if len(inputs) < 3 {
				fmt.Println(usage)
				continue
			}
			links(inputs[1], inputs[2:])
		case "status":
			if len(inputs) < 2 {
				fmt.Println(usage)
				continue
			}
			status(inputs[1])
		case "download":
			if len(inputs) < 2 {
				fmt.Println(usage)
				continue
			}
			download(inputs[1])
		}

		fmt.Print("\n> ")
	}
}

func task() {
	resp, err := http.Get(url + "/task")
	if err != nil {
		fmt.Println(err)
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bytes, _ := io.ReadAll(resp.Body)
		fmt.Println(string(bytes))
		return
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Println(result)
}

func links(id string, links []string) {

	body, err := json.Marshal(map[string]any{
		"id":    id,
		"links": links,
	})
	if err != nil {
		fmt.Println(err)
		return
	}

	resp, err := http.Post(url+"/links", "application/json", bytes.NewReader(body))
	if err != nil {
		fmt.Println("Error making request:", err)
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bytes, _ := io.ReadAll(resp.Body)
		fmt.Println(string(bytes))
		return
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Println(result)
}

func status(id string) {
	resp, err := http.Get(url + "/status?id=" + id)
	if err != nil {
		fmt.Println(err)
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bytes, _ := io.ReadAll(resp.Body)
		fmt.Println(string(bytes))
		return
	}

	var result map[string]string
	json.NewDecoder(resp.Body).Decode(&result)

	fmt.Println(result)
}

func download(id string) {
	resp, err := http.Get(url + "/download/" + id)
	if err != nil {
		fmt.Println(err)
		return
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bytes, _ := io.ReadAll(resp.Body)
		fmt.Println(string(bytes))
		return
	}

	file, err := os.Create(id + ".zip")
	if err != nil {
		fmt.Println(err)
		return
	}

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Println("saved to ./" + id + ".zip")
}
