package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/mattn/go-isatty"
)

type Note struct {
	Type     string
	Body     string
	Resolved bool
}

type Push struct {
	Action      string
	RefType     string `json:"ref_type"`
	Ref         string
	CommitTitle string `json:"commit_title"`
}

type Event struct {
	Id             int64
	CreatedAt      string `json:"created_at"`
	AuthorUsername string `json:"author_username"`
	Action         string `json:"action_name"`
	TargetTitle    string `json:"target_title"`
	Note           *Note
	Push           *Push `json:"push_data"`
}

var seenIds []int64

func idSeen(id int64) bool {
	if len(seenIds) == 0 {
		return false
	}

	for _, seenId := range seenIds {
		if seenId == id {
			return true
		}
	}
	return false

}

func fetchProjectEvents(url string) ([]Event, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	log.Printf("Body: %s\n", body)

	var events []Event
	if err = json.Unmarshal(body, &events); err != nil {
		// Could happen on 504 or such which returns html instead of json
		return nil, err
	}

	log.Printf("JSON events: %+v\n", events)

	return events, nil
}

var (
	verbose = flag.Bool("verbose", false, "Verbose")
	token   = flag.String("token", "", "Gitlab API token (private, do not share with others)")
)

func main() {
	flag.Parse()

	if !*verbose {
		log.SetOutput(ioutil.Discard)
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "Missing token")
		os.Exit(1)
	}

	projectIds := flag.Args()
	if len(projectIds) == 0 {
		fmt.Fprintln(os.Stderr, "Missing project id(s) to watch")
		os.Exit(1)
	}

	projectId := projectIds[0]
	if projectId == "" {
		fmt.Fprintln(os.Stderr, "Emtpy project id")
		os.Exit(1)
	}

	url := fmt.Sprintf("https://gitlab.ppro.com/api/v4/projects/%s/events?private_token=%s", projectId, *token)

	GREEN := "\x1b[32m"
	RESET := "\x1b[0m"
	GRAY := "\x1b[38;5;250m"
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		GREEN = ""
		RESET = ""
		GRAY = ""
	}

	for {
		events, err := fetchProjectEvents(url)
		if err != nil {
			time.Sleep(1 * time.Second)
		}

		for i := len(events) - 1; i >= 0; i-- {
			event := events[i]

			if idSeen(event.Id) {
				continue
			}
			seenIds = append(seenIds, event.Id)

			fmt.Printf("%s%s %s%s%s %s%s: %s", GRAY, event.CreatedAt, GREEN, event.AuthorUsername, GRAY, event.Action, RESET, event.TargetTitle)
			if event.Note != nil {
				resolved := ""
				if event.Note.Resolved {
					resolved = "✔"
				}
				fmt.Printf("\n💬 %s %s%s%s", event.Note.Body, GREEN, resolved, RESET)
			} else if event.Push != nil {
				fmt.Printf("\n⬆️  %s: %s", event.Push.Ref, event.Push.CommitTitle)
			}

			fmt.Print("\n\n")
		}

		time.Sleep(5 * time.Second)
	}
}
