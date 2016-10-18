package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"

	"google.golang.org/api/youtube/v3"

	"github.com/google/google-api-go-client/googleapi/transport"
)

const parallelism = 4
const maxResults = 20
const skipNFirst = 0

var dledMutex = sync.Mutex{}

func main() {
	flag.Parse()

	client := &http.Client{
		Transport: &transport.APIKey{Key: developerKey},
	}

	service, err := youtube.New(client)
	if err != nil {
		log.Fatalf("Error creating new YouTube client: %v", err)
	}

	index := make(map[string]bool)
	makeIndex("dled.txt", &index)

	fails := make(map[string]bool)
	makeIndex("fails.txt", &fails)

	dled, err := os.OpenFile("dled.txt", os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Fatal(err)
	}

	queryWg := &sync.WaitGroup{}
	videoWg := &sync.WaitGroup{}

	queryChan := make(chan string, parallelism)
	videoChan := make(chan string, parallelism)
	spots := make(chan bool, parallelism)

	i := 0
	for i < parallelism {
		spots <- true
		i++
	}

	go func() {
		for query := range queryChan {
			log.Printf("Query: %s", query)

			search(service, query, videoChan, videoWg)
			queryWg.Done()
		}
		close(videoChan)
	}()

	go func() {
		for id := range videoChan {
			<-spots
			go download(id, videoWg, spots, dled, index, fails)
		}
	}()

	readFile("todl.txt", queryChan, queryWg)
	close(queryChan)

	queryWg.Wait()
	videoWg.Wait()

	err = dled.Close()
	if err != nil {
		log.Fatal(err)
	}

}

func readFile(fileName string, res chan string, wg *sync.WaitGroup) {

	file, err := os.Open(fileName)
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}
		log.Print(text)
		wg.Add(1)
		res <- text
	}

	err = scanner.Err()
	if err != nil {
		log.Fatal(err)
	}

	err = file.Close()
	if err != nil {
		log.Fatal(err)
	}

}

func search(service *youtube.Service, query string, res chan string, wg *sync.WaitGroup) {

	// Make the API call to YouTube.
	call := service.Search.List("id").
		Q(query).
		MaxResults(maxResults)
	response, err := call.Do()
	if err != nil {
		log.Fatalf("Error making search API call: %v", err)
	}

	for i, item := range response.Items {
		if i < skipNFirst {
			continue
		}
		switch item.Id.Kind {
		case "youtube#video":
			wg.Add(1)
			res <- item.Id.VideoId
		}
	}

}

func download(id string, wg *sync.WaitGroup, spots chan bool, dled *os.File, index map[string]bool, fails map[string]bool) {
	if !fails[id] && !index[id] {

		url := fmt.Sprintf("https://www.youtube.com/watch?v=%s", id)
		out, err := exec.Command("nice", "--adjustment", "20", "youtube-dl", "-f", "bestaudio", "-x", "--audio-format", "m4a", "--postprocessor-args", "-strict experimental", url).Output()
		if err != nil {
			log.Fatalf("youtube-dl has failed: %s\n%s\n%s", url, string(out), err)
		}
		fmt.Printf("Video: %s\n%s\n", id, string(out))

		dledMutex.Lock()

		_, err = dled.WriteString(fmt.Sprintln(id))
		if err != nil {
			dledMutex.Unlock()
			log.Fatal(err)
		}

		err = dled.Sync()
		if err != nil {
			dledMutex.Unlock()
			log.Fatal(err)
		}

		dledMutex.Unlock()
	}

	wg.Done()

	spots <- true

}

func makeIndex(filename string, index *map[string]bool) {

	file, err := os.Open(filename)
	if err != nil {
		log.Fatal(err)
	}

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		(*index)[scanner.Text()] = true
	}

	err = scanner.Err()
	if err != nil {
		log.Fatal(err)
	}

	err = file.Close()
	if err != nil {
		log.Fatal(err)
	}

}
