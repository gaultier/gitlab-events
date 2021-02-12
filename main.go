package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/mattn/go-isatty"
)

var (
	verbose    = flag.Bool("verbose", false, "Verbose")
	token      = flag.String("token", "", "Gitlab API token (private, do not share with others)")
	gitlabURL  = flag.String("url", "gitlab.com", "Gitlab URL. Might be different from gitlab.com when self-hosting.")
	jsonOutput = flag.Bool("json", false, "Output json for scripts to consume")
)

type Project struct {
	ID                int64
	PathWithNamespace string `json:"path_with_namespace"`
	Name              string
}

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
	ID             int64
	CreatedAt      string `json:"created_at"`
	AuthorUsername string `json:"author_username"`
	Action         string `json:"action_name"`
	TargetTitle    string `json:"target_title"`
	Note           *Note
	Push           *Push `json:"push_data"`
	Project        *Project
}

var (
	_GreenColor = "\x1b[32m"
	_ResetColor = "\x1b[0m"
	_GrayColor  = "\x1b[38;5;250m"
)

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

func fetchProjectByID(projectID int64) (Project, error) {
	url := fmt.Sprintf("https://%s/api/v4/projects/%d?simple=true&private_token=%s", *gitlabURL, projectID, *token)
	project := Project{}

	resp, err := http.Get(url)
	if err != nil {
		return project, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return project, err
	}
	log.Printf("Body: %s\n", body)

	if err = json.Unmarshal(body, &project); err != nil {
		// Could happen on 504 or such which returns html instead of json
		return project, err
	}

	log.Printf("JSON project: %+v\n", project)

	return project, nil
}

func watchProject(project *Project) {
	seenIDs := make(map[int64]bool)

	url := fmt.Sprintf("https://%s/api/v4/projects/%d/events?private_token=%s", *gitlabURL, project.ID, *token)

	for {
		events, err := fetchProjectEvents(url)
		if err != nil {
			log.Printf("Error when fetching events for project %d: %s", project.ID, err)
			time.Sleep(1 * time.Second)
		}

		for i := len(events) - 1; i >= 0; i-- {
			event := events[i]

			if seenIDs[event.ID] == true {
				// Already seen, skip
				continue
			}
			seenIDs[event.ID] = true

			if *jsonOutput {
				event.Project = project
				eventJSON, err := json.Marshal(event)
				if err != nil {
					log.Printf("Failed to marshal event to JSON: %s", err)
				} else {
					fmt.Println(string(eventJSON))
				}

				continue
			}

			fmt.Printf("%s%s %s%s %s%s%s %s%s: %s", _GreenColor, project.PathWithNamespace, _GrayColor, event.CreatedAt, _GreenColor, event.AuthorUsername, _GrayColor, event.Action, _ResetColor, event.TargetTitle)
			if event.Note != nil {
				resolved := ""
				if event.Note.Resolved {
					resolved = "✔"
				}
				noteLen := int64(math.Min(float64(len(event.Note.Body)), 300))
				fmt.Printf("\n💬 %s %s%s%s", event.Note.Body[:noteLen], _GreenColor, resolved, _ResetColor)
			} else if event.Push != nil {
				fmt.Printf("\n⬆️  %s: %s", event.Push.Ref, event.Push.CommitTitle)
			}

			fmt.Print("\n\n")
		}

		time.Sleep(5 * time.Second)
	}
}

func main() {
	flag.Parse()

	if !*verbose {
		log.SetOutput(ioutil.Discard)
	}

	projectIDsStr := flag.Args()
	if len(projectIDsStr) == 0 {
		fmt.Fprintln(os.Stderr, "Missing project id(s) to watch")
		os.Exit(1)
	}

	if !isatty.IsTerminal(os.Stdout.Fd()) {
		_GreenColor = ""
		_ResetColor = ""
		_GrayColor = ""
	}

	for _, projectIDStr := range projectIDsStr {
		projectID, err := strconv.ParseInt(projectIDStr, 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Invalid project id %s: %s\n", projectIDStr, err)
			os.Exit(1)
		}
		project, err := fetchProjectByID(projectID)

		go watchProject(&project)
	}

	// Wait indefinitely, the real work is done by the goroutines
	done := make(chan bool)
	<-done
}
