package mcpserver_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/usenorn/runner/internal/control"
	"github.com/usenorn/runner/internal/entity"
)

func TestEveryToolTheAgentIsMeantToHaveIsThere(t *testing.T) {
	h := newHarness(t, newDaemon())

	named := []string{}

	for _, tool := range h.tools(t) {
		named = append(named, tool.Name)
	}

	for _, wanted := range []string{
		"start_service", "stop_service", "restart_service", "list_services",
		"get_service_logs", "run_step", "allocate_port",
		"expose_preview", "close_preview",
		"ask_human", "report_progress", "publish_artifact", "complete_task",
	} {
		if !slices.Contains(named, wanted) {
			t.Fatalf("the agent was given %v, without %s", named, wanted)
		}
	}
}

func TestNoToolLetsTheAgentNameAnExecutionAtAll(t *testing.T) {
	h := newHarness(t, newDaemon())

	for _, tool := range h.tools(t) {
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("read what %s accepts: %v", tool.Name, err)
		}

		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}

		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("read what %s accepts: %v", tool.Name, err)
		}

		for property := range schema.Properties {
			lowered := strings.ToLower(property)

			if strings.Contains(lowered, "exec") || lowered == "run" || lowered == "run_id" {
				t.Fatalf(
					"%s accepts %q. A tool that takes an execution is a tool that can be "+
						"pointed at somebody else's run, and no wording in a description "+
						"stops that",
					tool.Name, property,
				)
			}
		}
	}
}

func TestTheToolsCarryTheGuidanceThatKeepsARunTidy(t *testing.T) {
	h := newHarness(t, newDaemon())

	said := map[string]string{}

	for _, tool := range h.tools(t) {
		said[tool.Name] = tool.Description
	}

	for name, wanted := range map[string]string{
		"start_service":  "Never start one yourself",
		"allocate_port":  "Never pick a number yourself",
		"expose_preview": "only works on",
		"complete_task":  "Commit everything first",
	} {
		if !strings.Contains(said[name], wanted) {
			t.Fatalf(
				"%s never says %q. Guidance the model does not read where it acts is guidance "+
					"it does not follow, and a daemonised process outlives the run",
				name, wanted,
			)
		}
	}
}

func TestAskingAPersonHandsBackTheAnswerWhenSomebodyIsThere(t *testing.T) {
	made := newDaemon()
	made.answers[control.QuestionsPath] = control.QuestionAnswer{
		Status:     string(entity.AskAnswered),
		QuestionID: "01QUESTION",
		Answer:     "the lower of the two middle values",
		AnsweredBy: "Vlad Gorokhov",
	}

	h := newHarness(t, made)

	result := h.call(t, "ask_human", map[string]any{
		"question": "Which convention should median use?",
		"options":  []string{"the mean of the two", "the lower of the two middle values"},
		"blocking": true,
	})

	var answered struct {
		Status     string `json:"status"`
		Answer     string `json:"answer"`
		AnsweredBy string `json:"answered_by"`
	}

	decode(t, result, &answered)

	if answered.Answer != "the lower of the two middle values" {
		t.Fatalf(
			"the agent was handed %q. It branches on what a person actually decided, so the "+
				"answer travels word for word or not at all",
			answered.Answer,
		)
	}

	if answered.AnsweredBy != "Vlad Gorokhov" {
		t.Fatalf("the agent was not told who decided: %+v", answered)
	}
}

func TestAQuestionNobodyAnsweredComesBackTellingTheAgentToStop(t *testing.T) {
	made := newDaemon()
	made.answers[control.QuestionsPath] = control.QuestionAnswer{
		Status:     string(entity.AskPending),
		QuestionID: "01QUESTION",
		Advice:     entity.AskPendingAdvice,
	}

	h := newHarness(t, made)

	result := h.call(t, "ask_human", map[string]any{
		"question": "Which convention should median use?",
		"blocking": true,
	})

	var answered struct {
		Status string `json:"status"`
		Advice string `json:"advice"`
	}

	decode(t, result, &answered)

	if answered.Status != string(entity.AskPending) {
		t.Fatalf("the agent was told %q, want it left pending", answered.Status)
	}

	if answered.Advice != entity.AskPendingAdvice {
		t.Fatalf(
			"the agent was told %q instead of the daemon's own words. It has to end its turn "+
				"rather than guess, and rewording that here is how it stops being clear",
			answered.Advice,
		)
	}
}

func TestARefusedToolCallHandsTheDaemonsOwnSentenceToTheAgent(t *testing.T) {
	made := newDaemon()
	made.refusals[control.PreviewsPath] = control.Failure{
		Reason:  control.ReasonRefused,
		Message: entity.ErrPreviewNotOwned.Error() + ": this run is not running postgres",
	}

	h := newHarness(t, made)

	result := h.call(t, "expose_preview", map[string]any{"service": "postgres"})

	if !result.IsError {
		t.Fatalf("a refused call came back looking like it worked: %+v", result)
	}

	said := text(t, result)

	if !strings.Contains(said, "not running postgres") {
		t.Fatalf(
			"the agent was told %q. The daemon already wrote what was wrong and what to do "+
				"about it; a second, vaguer version of that helps nobody",
			said,
		)
	}
}

func TestExposingAPreviewSaysHowFarTheAddressActuallyReaches(t *testing.T) {
	made := newDaemon()
	made.answers[control.PreviewsPath] = control.Preview{
		Name:    "web",
		Service: "web",
		Port:    43111,
		URL:     "http://127.0.0.1:43111",
		Shared:  "https://web-exec-01abc.norn.ink",
		Reach:   "anybody in the workspace who can see the issue, at https://web-exec-01abc.norn.ink",
	}

	h := newHarness(t, made)

	result := h.call(t, "expose_preview", map[string]any{"service": "web"})

	var exposed struct {
		Preview struct {
			URL    string `json:"url"`
			Shared string `json:"shared"`
			Reach  string `json:"reach"`
		} `json:"preview"`
	}

	decode(t, result, &exposed)

	if exposed.Preview.Reach == "" {
		t.Fatalf(
			"the agent was handed %q with nothing saying who can open it. It will put that "+
				"link in front of a person who is not on this machine, and it will not resolve",
			exposed.Preview.URL,
		)
	}

	if exposed.Preview.Shared != "https://web-exec-01abc.norn.ink" {
		t.Fatalf(
			"the agent was handed shared address %q; the daemon composed the address that "+
				"actually reaches a reviewer and the tool has to pass it on unchanged",
			exposed.Preview.Shared,
		)
	}
}

func TestStartingAServiceSendsTheDaemonWhatTheAgentAskedFor(t *testing.T) {
	made := newDaemon()
	made.answers[control.ServicesPath] = control.Service{Name: "web", State: "starting", Port: 43111}

	h := newHarness(t, made)

	h.call(t, "start_service", map[string]any{
		"name":        "web",
		"command":     []string{"pnpm", "dev", "--port", "${ports.web}"},
		"health_http": "/healthz",
	})

	var sent control.ServiceRequest

	if err := json.Unmarshal(made.asked[control.ServicesPath], &sent); err != nil {
		t.Fatalf("read what the daemon was sent: %v", err)
	}

	if !slices.Contains(sent.Command, "${ports.web}") {
		t.Fatalf(
			"the command reached the daemon as %v. The port mark has to survive the tool "+
				"untouched, because norn is what turns it into a number",
			sent.Command,
		)
	}

	if sent.Health.Kind != string(entity.HealthHTTP) || sent.Health.Path != "/healthz" {
		t.Fatalf("the health check arrived as %+v, so nothing would ever call the service up", sent.Health)
	}
}

func TestSayingTheWorkIsDoneCarriesTheSummaryAndTheNotesSeparately(t *testing.T) {
	made := newDaemon()
	made.answers[control.CompletePath] = control.Completed{Advice: entity.CompletedAdvice}

	h := newHarness(t, made)

	h.call(t, "complete_task", map[string]any{
		"summary":            "added a median helper",
		"notes_for_reviewer": "the convention was decided by a person",
	})

	var sent control.CompleteRequest

	if err := json.Unmarshal(made.asked[control.CompletePath], &sent); err != nil {
		t.Fatalf("read what the daemon was sent: %v", err)
	}

	if sent.Summary != "added a median helper" || sent.Notes == "" {
		t.Fatalf(
			"what the agent said arrived as %+v. What it changed and what a reviewer should "+
				"know first are two different things and are read in two different places",
			sent,
		)
	}
}
