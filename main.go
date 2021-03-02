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
{{.Green}}{{.ProjectPathWithNamespace}}{{.Gray}} {{.CreatedAt}} ({{.TimeSince}}){{.Green}} {{.Author}}{{.Gray}}: {{.EventAction}}{{.Reset}} {{trunc .TargetTitle 100}}
{{- if .IsNote }}
💬 {{trunc .Body 400 -}}
{{- if .Resolved -}} {{.Green}} ✔{{.Reset -}}{{- end}}
{{- end -}}
{{- if .IsPush }}
⬆️  {{.Ref}} {{.CommitTitle -}}
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
	Green, ProjectPathWithNamespace, Gray, CreatedAt, Author, EventAction, Reset, TargetTitle, Body, Ref, CommitTitle, URL, TimeSince string
	IsNote, IsPush, Resolved                                                                                                          bool
}

type Project struct {
	ID                int64
	PathWithNamespace string `json:"path_with_namespace"`
	Name              string
}

type Note struct {
	Type        string
	Body        string
	Resolved    bool
	NoteableIID int64 `json:"noteable_iid"`
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
	TargetIID      int64  `json:"target_iid"`
	TargetType     string `json:"target_type"`
	Note           *Note
	Push           *Push `json:"push_data"`
	Project        *Project
	JSON           []byte
}

func addEvents(events *[]Event) {
	_EventsMutex.Lock()
	defer _EventsMutex.Unlock()

	for _, event := range *events {
		hash := _Hasher.Sum(event.JSON)

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
		events[i].JSON, _ = json.Marshal(&events[i])
		events[i].Project = project
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
	log.Printf("%v s=%d m=%d h=%d", d, s, m, h)

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
		log.Printf("Handling projectID=%s", projectIDStr)
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
		sort.Slice(events, func(i, j int) bool { return events[i].CreatedAt < events[j].CreatedAt })

		for _, event := range events {
			if *jsonOutput {
				fmt.Println(string(event.JSON))

				continue
			}

			createdAt, err := time.Parse(time.RFC3339, event.CreatedAt)
			if err != nil {
				log.Printf("Failed to parse date: CreatedAt=%s err=%s", event.CreatedAt, err)
			}

			url := fmt.Sprintf("🔗 https://%s/%s", *gitlabURL, event.Project.PathWithNamespace)
			if event.Note != nil {
				url += fmt.Sprintf("/-/merge_requests/%d", event.Note.NoteableIID)
			} else if event.TargetType == "MergeRequest" {
				url += fmt.Sprintf("/-/merge_requests/%d", event.TargetIID)
			}
			templateInput := TemplateInput{
				Green:                    _GreenColor,
				Gray:                     _GrayColor,
				Reset:                    _ResetColor,
				CreatedAt:                event.CreatedAt,
				Author:                   event.AuthorUsername,
				TargetTitle:              event.TargetTitle,
				ProjectPathWithNamespace: event.Project.PathWithNamespace,
				URL:                      url,
				TimeSince:                formatTimeSinceShort(time.Since(createdAt)),
				EventAction:              event.Action}
			if event.Note != nil {
				templateInput.IsNote = true
				templateInput.Resolved = event.Note.Resolved
				templateInput.Body = event.Note.Body
			} else if event.Push != nil {
				templateInput.IsPush = true
				templateInput.Ref = event.Push.Ref
				templateInput.CommitTitle = event.Push.CommitTitle
			}

			t.Execute(os.Stdout, &templateInput)
		}
		time.Sleep(1 * time.Second)
	}
}
