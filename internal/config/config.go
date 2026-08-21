package config

import "time"

type Config struct {
	Runner  Runner  `mapstructure:",squash"`
	App     App     `mapstructure:"app"`
	State   State   `mapstructure:"state"`
	Log     Log     `mapstructure:"log"`
	Control Control `mapstructure:"control"`
	Session Session `mapstructure:"session"`
}

type Runner struct {
	Version   int       `mapstructure:"version"`
	Server    string    `mapstructure:"server"`
	Capacity  int       `mapstructure:"capacity"`
	Runtime   Runtime   `mapstructure:"runtime"`
	PortRange [2]int    `mapstructure:"port_range"`
	Retention Retention `mapstructure:"retention"`
	Telemetry Telemetry `mapstructure:"telemetry"`
}

type Retention struct {
	WorkspaceAfterDone time.Duration `mapstructure:"workspace_after_done"`
	RunsMaxAge         time.Duration `mapstructure:"runs_max_age"`
	RunsMaxDisk        int64         `mapstructure:"runs_max_disk"`
}

type App struct {
	Version  string `mapstructure:"version"`
	LogLevel string `mapstructure:"log_level"`
}

type State struct {
	Root       string `mapstructure:"root"`
	ConfigFile string `mapstructure:"-"`
}

type Log struct {
	Console    Console `mapstructure:"console"`
	MaxSizeMB  int     `mapstructure:"max_size_mb"`
	MaxBackups int     `mapstructure:"max_backups"`
	MaxAgeDays int     `mapstructure:"max_age_days"`
	Compress   bool    `mapstructure:"compress"`
}

type Control struct {
	DialTimeout       time.Duration `mapstructure:"dial_timeout"`
	RequestTimeout    time.Duration `mapstructure:"request_timeout"`
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`
	ShutdownTimeout   time.Duration `mapstructure:"shutdown_timeout"`
}

type Session struct {
	RequestTimeout time.Duration `mapstructure:"request_timeout"`
	RefreshLead    time.Duration `mapstructure:"refresh_lead"`
	RetryMin       time.Duration `mapstructure:"retry_min"`
	RetryMax       time.Duration `mapstructure:"retry_max"`
}

type Runtime string

const (
	RuntimeAuto    Runtime = "auto"
	RuntimeProcess Runtime = "process"
	RuntimeDocker  Runtime = "docker"
)

func Runtimes() []Runtime {
	return []Runtime{RuntimeAuto, RuntimeProcess, RuntimeDocker}
}

type Telemetry string

const (
	TelemetryStandard Telemetry = "standard"
	TelemetryMinimal  Telemetry = "minimal"
)

func Telemetries() []Telemetry {
	return []Telemetry{TelemetryStandard, TelemetryMinimal}
}

type Console string

const (
	ConsoleAuto   Console = "auto"
	ConsoleAlways Console = "always"
	ConsoleNever  Console = "never"
)

func Consoles() []Console {
	return []Console{ConsoleAuto, ConsoleAlways, ConsoleNever}
}
