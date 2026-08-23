package entity

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	RunServiceLogsDir = "services"
	ServiceLogSuffix  = ".log"

	ServiceRing     = 500
	ServiceTailLine = 4096
	StepTailLines   = 20
)

var (
	ErrServiceUnknown = errors.New("this run has no service by that name")
	ErrServiceInvalid = errors.New("that service description cannot be started")
	ErrServiceWaiting = errors.New("a service this one needs is not healthy yet")
	ErrPortUnknown    = errors.New("nothing on this run reserves a port by that name")
	ErrPortsExhausted = errors.New(
		"this machine has no free port left in the range it was given; widen port_range",
	)
	ErrStepTimedOut = errors.New("that step ran past the time it was given")
)

var (
	serviceName = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	portMark    = regexp.MustCompile(`\$\{ports\.([a-z0-9][a-z0-9_-]{0,31})\}`)
	portUnsafe  = regexp.MustCompile(`[^A-Z0-9]`)
)

type HealthKind string

const (
	HealthNone HealthKind = ""
	HealthHTTP HealthKind = "http"
	HealthTCP  HealthKind = "tcp"
	HealthLog  HealthKind = "log"
)

func HealthKinds() []HealthKind {
	return []HealthKind{HealthNone, HealthHTTP, HealthTCP, HealthLog}
}

func (k HealthKind) Valid() bool {
	return slices.Contains(HealthKinds(), k)
}

type ServiceState string

const (
	ServiceStarting  ServiceState = "starting"
	ServiceHealthy   ServiceState = "healthy"
	ServiceUnhealthy ServiceState = "unhealthy"
	ServiceStopped   ServiceState = "stopped"
)

func ServiceStates() []ServiceState {
	return []ServiceState{ServiceStarting, ServiceHealthy, ServiceUnhealthy, ServiceStopped}
}

func (s ServiceState) Valid() bool {
	return slices.Contains(ServiceStates(), s)
}

func (s ServiceState) Live() bool {
	return s == ServiceStarting || s == ServiceHealthy
}

type Health struct {
	Kind    HealthKind
	Path    string
	Port    string
	Pattern string
}

func (h Health) Valid() error {
	if !h.Kind.Valid() {
		return fmt.Errorf("%w: %q is not a health check this machine knows", ErrServiceInvalid, h.Kind)
	}

	if h.Kind == HealthLog && strings.TrimSpace(h.Pattern) == "" {
		return fmt.Errorf("%w: a log health check needs something to look for", ErrServiceInvalid)
	}

	if h.Kind == HealthLog {
		if _, err := regexp.Compile(h.Pattern); err != nil {
			return fmt.Errorf("%w: %s is not a pattern: %w", ErrServiceInvalid, h.Pattern, err)
		}
	}

	return nil
}

type Service struct {
	Name        string
	Dir         string
	Command     []string
	Environment map[string]string
	Requires    []string
	Health      Health
}

func (s Service) Valid() error {
	if !serviceName.MatchString(s.Name) {
		return fmt.Errorf(
			"%w: %q is not a name a service can have; use lower-case letters, digits, "+
				"dashes and underscores",
			ErrServiceInvalid, s.Name,
		)
	}

	if len(s.Command) == 0 {
		return fmt.Errorf("%w: %s has nothing to run", ErrServiceInvalid, s.Name)
	}

	for _, needed := range s.Requires {
		if !serviceName.MatchString(needed) {
			return fmt.Errorf(
				"%w: %s says it needs %q, which is not a name a service can have",
				ErrServiceInvalid, s.Name, needed,
			)
		}

		if needed == s.Name {
			return fmt.Errorf("%w: %s cannot need itself", ErrServiceInvalid, s.Name)
		}
	}

	if strings.Contains(s.Dir, "..") {
		return fmt.Errorf(
			"%w: %s asks to run in %s, which leaves the workspace", ErrServiceInvalid, s.Name, s.Dir,
		)
	}

	return s.Health.Valid()
}

type ServiceRecord struct {
	Name      string
	Command   []string
	Dir       string
	Port      int
	PID       int
	State     ServiceState
	Attempts  int
	Reason    string
	StartedAt time.Time
	ChangedAt time.Time
}

type Step struct {
	Name    string
	Dir     string
	Command []string
	Timeout time.Duration
}

func (s Step) Valid() error {
	if !serviceName.MatchString(s.Name) {
		return fmt.Errorf(
			"%w: %q is not a name a step can have; use lower-case letters, digits, dashes "+
				"and underscores",
			ErrServiceInvalid, s.Name,
		)
	}

	if len(s.Command) == 0 {
		return fmt.Errorf("%w: %s has nothing to run", ErrServiceInvalid, s.Name)
	}

	if strings.Contains(s.Dir, "..") {
		return fmt.Errorf(
			"%w: %s asks to run in %s, which leaves the workspace", ErrServiceInvalid, s.Name, s.Dir,
		)
	}

	return nil
}

type StepResult struct {
	Name     string
	ExitCode int
	Output   string
	Took     time.Duration
	TimedOut bool
}

type LogQuery struct {
	Tail int
	Grep string
}

func (q LogQuery) Window() int {
	if q.Grep != "" {
		return 0
	}

	return q.Tail
}

func (q LogQuery) Select(lines []string) ([]string, error) {
	if q.Grep == "" {
		return lines, nil
	}

	pattern, err := regexp.Compile(q.Grep)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a pattern: %s", ErrServiceInvalid, q.Grep, err)
	}

	kept := make([]string, 0, len(lines))

	for _, line := range lines {
		if pattern.MatchString(line) {
			kept = append(kept, line)
		}
	}

	if q.Tail > 0 && len(kept) > q.Tail {
		kept = kept[len(kept)-q.Tail:]
	}

	return kept, nil
}

func ValidName(name string) bool {
	return serviceName.MatchString(name)
}

func PortVariable(name string) string {
	return "NORN_PORT_" + portUnsafe.ReplaceAllString(strings.ToUpper(name), "_")
}

func (s Service) Ports() []string {
	values := append([]string{s.Dir, s.Health.Path, s.Health.Port}, s.Command...)

	for _, key := range sorted(s.Environment) {
		values = append(values, key, s.Environment[key])
	}

	wanted := []string{s.Name}

	for _, name := range PortsWanted(values...) {
		if !slices.Contains(wanted, name) {
			wanted = append(wanted, name)
		}
	}

	return wanted
}

func sorted(values map[string]string) []string {
	keys := make([]string, 0, len(values))

	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

func PortsWanted(values ...string) []string {
	wanted := []string{}

	for _, value := range values {
		for _, found := range portMark.FindAllStringSubmatch(value, -1) {
			if !slices.Contains(wanted, found[1]) {
				wanted = append(wanted, found[1])
			}
		}
	}

	return wanted
}

func ResolvePorts(value string, ports map[string]int) (string, error) {
	var missing string

	resolved := portMark.ReplaceAllStringFunc(value, func(mark string) string {
		name := portMark.FindStringSubmatch(mark)[1]

		port, held := ports[name]
		if !held {
			missing = name

			return mark
		}

		return strconv.Itoa(port)
	})

	if missing != "" {
		return "", fmt.Errorf("%w: %s", ErrPortUnknown, missing)
	}

	return resolved, nil
}

func ServiceLogFile(name string) string {
	return name + ServiceLogSuffix
}
