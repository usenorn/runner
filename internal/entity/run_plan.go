package entity

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	channelv1 "github.com/usenorn/norn/pkg/channel/v1"
)

const (
	PlanPreviewNameMax = 100
	PlanReasonMax      = 2000
)

var ErrPlanInvalid = errors.New("the run plan cannot be read")

var previewSlugUnsafe = regexp.MustCompile(`[^a-z0-9_-]+`)

type PlanServiceKind string

const (
	PlanServiceProcess PlanServiceKind = "process"
	PlanServiceCompose PlanServiceKind = "compose"
)

func PlanServiceKinds() []PlanServiceKind {
	return []PlanServiceKind{PlanServiceProcess, PlanServiceCompose}
}

func (k PlanServiceKind) Valid() bool {
	return slices.Contains(PlanServiceKinds(), k)
}

type PlanService struct {
	Kind    PlanServiceKind
	Service Service
}

type PlanPreview struct {
	Name    string
	Service string
	Path    string
}

func (p PlanPreview) Slug() string {
	slug := previewSlugUnsafe.ReplaceAllString(strings.ToLower(strings.TrimSpace(p.Name)), "-")

	return strings.Trim(slug, "-_")
}

type PlanDefinition struct {
	Services []PlanService
	Previews []PlanPreview
}

func (d PlanDefinition) Empty() bool {
	return len(d.Services) == 0 && len(d.Previews) == 0
}

func (d PlanDefinition) Service(name string) (PlanService, bool) {
	for _, held := range d.Services {
		if held.Service.Name == name {
			return held, true
		}
	}

	return PlanService{}, false
}

func (d PlanDefinition) Runnable() []Service {
	byName := map[string]Service{}

	for _, held := range d.Services {
		if held.Kind == PlanServiceProcess && held.Service.Valid() == nil {
			byName[held.Service.Name] = held.Service
		}
	}

	ordered := make([]Service, 0, len(byName))
	placed := map[string]bool{}
	visiting := map[string]bool{}

	var place func(name string) bool

	place = func(name string) bool {
		if placed[name] {
			return true
		}

		service, known := byName[name]
		if !known || visiting[name] {
			return false
		}

		visiting[name] = true

		for _, needed := range service.Requires {
			if !place(needed) {
				return false
			}
		}

		visiting[name] = false
		placed[name] = true
		ordered = append(ordered, service)

		return true
	}

	for _, held := range d.Services {
		place(held.Service.Name)
	}

	return ordered
}

func (d PlanDefinition) Unrunnable(name string) (string, bool) {
	held, declared := d.Service(name)
	if !declared {
		return fmt.Sprintf("the run plan declares no service named %s", name), true
	}

	if held.Kind != PlanServiceProcess {
		return fmt.Sprintf(
			"%s is a %s service, and this machine runs process services only", name, held.Kind,
		), true
	}

	if err := held.Service.Valid(); err != nil {
		return err.Error(), true
	}

	if !slices.ContainsFunc(d.Runnable(), func(service Service) bool { return service.Name == name }) {
		return fmt.Sprintf(
			"%s needs a service that cannot run here, or needs itself through another", name,
		), true
	}

	return "", false
}

type PreviewOutcomeState string

const (
	PreviewOutcomeReady       PreviewOutcomeState = channelv1.PreviewReady
	PreviewOutcomeFailed      PreviewOutcomeState = channelv1.PreviewFailed
	PreviewOutcomeUnsupported PreviewOutcomeState = channelv1.PreviewUnsupported
)

type PreviewOutcome struct {
	Name    string
	Service string
	Path    string
	State   PreviewOutcomeState
	Reason  string
	Port    int
}

func (o PreviewOutcome) Wire() channelv1.PreviewOutcome {
	return channelv1.PreviewOutcome{
		Name:    clip(o.Name, PlanPreviewNameMax),
		Service: o.Service,
		Path:    o.Path,
		State:   string(o.State),
		Reason:  clip(o.Reason, PlanReasonMax),
		Port:    o.Port,
	}
}

type ReviewPass struct {
	Revision int
	Previews []PreviewOutcome
}

func PlanUnreadable(err error) string {
	return fmt.Sprintf("no preview could be prepared for this review, because %s", err)
}

func ServiceUnstoppable(name string, err error) string {
	return fmt.Sprintf("%s could not be stopped before it was started again: %s", name, err)
}
