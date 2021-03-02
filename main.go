package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"text/template"
	"time"

	"github.com/mattn/go-isatty"
)

var (
	verbose    = flag.Bool("verbose", false, "Verbose")
	token      = flag.String("token", "", "Gitlab API token (private, do not share with others)")
	gitlabURL  = flag.String("url", "gitlab.com", "Gitlab URL. Might be different from gitlab.com when self-hosting.")
	jsonOutput = flag.Bool("json", false, "Output json for scripts to consume")
)

var (
	_GreenColor                  = "\x1b[32m"
	_ResetColor                  = "\x1b[0m"
	_GrayColor                   = "\x1b[38;5;250m"
	_NewOrModifiedEvents []Event = nil
	_EventChecksumsByID          = make(map[int64][]byte)
	_EventsMutex                 = &sync.Mutex{}
	_Hasher                      = sha1.New()
)

const (
	eventTemplate = `
{{.Green}}{{.Event.Project.PathWithNamespace}}{{.Gray}} {{.Event.UpdatedAt}} ({{.TimeSince}}){{.Green}} {{.Event.AuthorUsername}}{{.Gray}}: {{.Event.Action}}{{.Reset}} {{trunc .Event.TargetTitle 100}}
{{- if eq .Event.Action "commented on" }}
{{- if eq .Event.Note.Type "DiffNote" }}
📃 {{.Gray}}{{.Event.Note.Position.NewPath}}:{{.Event.Note.Position.NewLine}}{{.Reset}}
{{- end}}
💬 {{trunc .Event.Note.Body 400 -}}
{{- if .Event.Note.Resolved -}} {{.Green}} ✔{{.Reset -}}{{- end}}
{{- else if or (eq .Event.Action "pushed to") (eq .Event.Action "pushed new") }}
⬆️  {{.Event.Push.Ref}} {{.Event.Push.CommitTitle}} ({{.Event.Push.CommitCount}} commits)
{{- end}}
{{ .URL }}
`
)

func truncateString(s string, maxLen int) string {
	length := len(s)
	if length <= maxLen {
		return s
	} else {
		return fmt.Sprintf("%s%s...%s", s[:maxLen], _GrayColor, _ResetColor)
	}
}

type TemplateInput struct {
	Green, Reset, ProjectPathWithNamespace, Gray, URL, TimeSince string
	Event                                                        Event
}

type Project struct {
	ID                int64
	PathWithNamespace string `json:"path_with_namespace"`
	Name              string
}

type Position struct {
	NewPath string `json:"new_path"`
	NewLine int64  `json:"new_line"`
}

type Note struct {
	Type        string
	Body        string
	Resolved    bool
	NoteableIID int64 `json:"noteable_iid"`
	Position    *Position
}

type Push struct {
	Action      string
	RefType     string `json:"ref_type"`
	Ref         string
	CommitTitle string `json:"commit_title"`
	CommitCount int64  `json:"commit_count"`
}

type Event struct {
	ID             int64
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	AuthorUsername string `json:"author_username"`
	Action         string `json:"action_name"`
	TargetTitle    string `json:"target_title"`
	TargetIID      int64  `json:"target_iid"`
	TargetType     string `json:"target_type"`
	Note           *Note
	Push           *Push `json:"push_data"`
	Project        *Project
	json           []byte
}

func addEvents(events *[]Event) {
	_EventsMutex.Lock()
	defer _EventsMutex.Unlock()

	for _, event := range *events {
		hash := _Hasher.Sum(event.json)

		existingHash, found := _EventChecksumsByID[event.ID]
		if !found {
			_NewOrModifiedEvents = append(_NewOrModifiedEvents, event)
			_EventChecksumsByID[event.ID] = hash
			continue
		}

		if !bytes.Equal(hash, existingHash) { // Updated
			_NewOrModifiedEvents = append(_NewOrModifiedEvents, event)
			_EventChecksumsByID[event.ID] = hash
		}
	}
}

func fetchProjectEvents(url string, project *Project) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	log.Printf("%s", body)

	var events []Event
	if err = json.Unmarshal(body, &events); err != nil {
		// Could happen on 504 or such which returns html instead of json
		return err
	}

	for i := range events {
		events[i].json, _ = json.Marshal(&events[i])
		events[i].Project = project
		if events[i].UpdatedAt == "" {
			events[i].UpdatedAt = events[i].CreatedAt
		}
	}
	addEvents(&events)

	return nil
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

	if err = json.Unmarshal(body, &project); err != nil {
		// Could happen on 504 or such which returns html instead of json
		return project, err
	}

	return project, nil
}

func watchProject(project *Project) {
	url := fmt.Sprintf("https://%s/api/v4/projects/%d/events?private_token=%s", *gitlabURL, project.ID, *token)

	for {
		if err := fetchProjectEvents(url, project); err != nil {
			log.Printf("Error when fetching events for project %d: %s", project.ID, err)
			time.Sleep(1 * time.Second)
		}

		time.Sleep(5 * time.Second)
	}
}

func formatTimeSinceShort(d time.Duration) string {
	s := int64(d.Seconds())
	m := int64(d.Minutes())
	h := int64(d.Hours())

	if h >= 30*24*12 {
		return fmt.Sprintf("%d years ago", int64(h/12/30/24))
	}
	if h >= 30*24 {
		return fmt.Sprintf("%d months ago", int64(h/30/24))
	}
	if h >= 24 {
		return fmt.Sprintf("%d days ago", int64(h/24))
	}
	if h > 0 {
		return fmt.Sprintf("%d hours ago", h)
	}
	if m > 0 {
		return fmt.Sprintf("%d minutes ago", m)
	}
	if s > 0 {
		return fmt.Sprintf("%d seconds ago", s)
	}
	return "just now"
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

		go func() {
			project, err := fetchProjectByID(projectID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to fetch the project information: id=%d err=%s\n", projectID, err)
				os.Exit(1)
			}
			log.Printf("Fetched info for projectID=%d", projectID)

			watchProject(&project)
		}()
	}

	t := template.Must(template.New("event").Funcs(template.FuncMap{"trunc": truncateString}).Parse(eventTemplate))

	for {
		_EventsMutex.Lock()
		events := make([]Event, len(_NewOrModifiedEvents))
		copy(events, _NewOrModifiedEvents)
		_NewOrModifiedEvents = nil
		_EventsMutex.Unlock()
		sort.Slice(events, func(i, j int) bool { return events[i].UpdatedAt < events[j].UpdatedAt })

		for _, event := range events {
			if *jsonOutput {
				fmt.Println(string(event.json))

				continue
			}

			updatedAt, err := time.Parse(time.RFC3339, event.UpdatedAt)
			if err != nil {
				log.Printf("Failed to parse date: UpdatedAt=%s err=%s", event.UpdatedAt, err)
			}

			url := fmt.Sprintf("🔗 https://%s/%s", *gitlabURL, event.Project.PathWithNamespace)
			if event.Note != nil {
				url += fmt.Sprintf("/-/merge_requests/%d", event.Note.NoteableIID)
			} else if event.TargetType == "MergeRequest" {
				url += fmt.Sprintf("/-/merge_requests/%d", event.TargetIID)
			}
			templateInput := TemplateInput{
				Green:     _GreenColor,
				Gray:      _GrayColor,
				Reset:     _ResetColor,
				Event:     event,
				URL:       url,
				TimeSince: formatTimeSinceShort(time.Since(updatedAt)),
			}

			t.Execute(os.Stdout, &templateInput)
		}
		time.Sleep(1 * time.Second)
	}
}
