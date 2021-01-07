package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
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
	AuthorUsername string `json:"author_username"`
	Action         string `json:"action_name"`
	TargetTitle    string `json:"target_title"`
	Note           *Note
	Push           *Push `json:"push_data"`
}

func main() {
	projectId := 138
	token := os.Getenv("GITLAB_TOKEN")
	url := fmt.Sprintf("https://gitlab.ppro.com/api/v4/projects/%d/events?private_token=%s", projectId, token)
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Fatal(err)
	}
	var events []Event
	if err = json.Unmarshal(body, &events); err != nil {
		log.Fatal(err)
	}

	GREEN := "\x1b[32m"
	RESET := "\x1b[0m"
	GRAY := "\x1b[38;5;250m"

	for _, event := range events {
		fmt.Printf("🧍 %s%s%s %s%s: %s\n", GREEN, event.AuthorUsername, GRAY, event.Action, RESET, event.TargetTitle)
		if event.Note != nil {
			resolved := ""
			if event.Note.Resolved {
				resolved = "👌"
			}
			fmt.Printf("💬 %s %s", event.Note.Body, resolved)
		} else if event.Push != nil {
			fmt.Printf("⬆️  🌿 %s: %s", event.Push.Ref, event.Push.CommitTitle)
		}
		fmt.Println("\n──────────────────────────")
	}
}
