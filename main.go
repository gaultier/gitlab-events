package main

import (
	"encoding/json"
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

func main() {
	projectId := os.Getenv("GITLAB_PROJECT")
	if projectId == "" {
		log.Fatalln("Missing GITLAB_PROJECT env var")
	}
	token := os.Getenv("GITLAB_TOKEN")
	if token == "" {
		log.Fatalln("Missing GITLAB_TOKEN env var")
	}

	url := fmt.Sprintf("https://gitlab.ppro.com/api/v4/projects/%s/events?private_token=%s", projectId, token)

	GREEN := "\x1b[32m"
	RESET := "\x1b[0m"
	GRAY := "\x1b[38;5;250m"
	if !isatty.IsTerminal(os.Stdout.Fd()) {
		GREEN = ""
		RESET = ""
		GRAY = ""
	}

	for {
		resp, err := http.Get(url)
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}

		body, err := ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			time.Sleep(1 * time.Second)
			continue
		}
		var events []Event
		if err = json.Unmarshal(body, &events); err != nil {
			log.Fatal(err)
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
				fmt.Printf("\n💬 %s %s", event.Note.Body, resolved)
			} else if event.Push != nil {
				fmt.Printf("\n⬆️  %s: %s", event.Push.Ref, event.Push.CommitTitle)
			}

			fmt.Print("\n\n")
		}

		time.Sleep(5 * time.Second)
	}
}
