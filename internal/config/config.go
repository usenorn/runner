package config

import "time"

type Config struct {
	Runner  Runner  `mapstructure:",squash"`
	App     App     `mapstructure:"app"`
	State   State   `mapstructure:"state"`
	Log     Log     `mapstructure:"log"`
	Control Control `mapstructure:"control"`
	Session Session `mapstructure:"session"`
	Update  Update  `mapstructure:"update"`

	Codebase  Codebase  `mapstructure:"codebase"`
	Snapshot  Snapshot  `mapstructure:"snapshot"`
	Channel   Channel   `mapstructure:"channel"`
	Spool     Spool     `mapstructure:"spool"`
	Scheduler Scheduler `mapstructure:"scheduler"`
}

type Channel struct {
	Enabled          bool          `mapstructure:"enabled"`
	HandshakeTimeout time.Duration `mapstructure:"handshake_timeout"`
	Heartbeat        time.Duration `mapstructure:"heartbeat"`
	WriteTimeout     time.Duration `mapstructure:"write_timeout"`
	RetryMin         time.Duration `mapstructure:"retry_min"`
	RetryMax         time.Duration `mapstructure:"retry_max"`
	MaxMessageBytes  int64         `mapstructure:"max_message_bytes"`
}

type Spool struct {
	MaxMessages int           `mapstructure:"max_messages"`
	MaxAge      time.Duration `mapstructure:"max_age"`
	Batch       int           `mapstructure:"batch"`
}

type Scheduler struct {
	MinFreeDisk int64 `mapstructure:"min_free_disk"`
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

type Codebase struct {
	ScanDepth      int           `mapstructure:"scan_depth"`
	RescanInterval time.Duration `mapstructure:"rescan_interval"`
	ProbeTimeout   time.Duration `mapstructure:"probe_timeout"`
}

type Update struct {
	Check    bool          `mapstructure:"check"`
	Interval time.Duration `mapstructure:"interval"`
	Timeout  time.Duration `mapstructure:"timeout"`
	Feed     string        `mapstructure:"feed"`
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

type Snapshot struct {
	GitMode        string        `mapstructure:"git_mode"`
	Base           string        `mapstructure:"base"`
	LocalChanges   string        `mapstructure:"local_changes"`
	Fetch          bool          `mapstructure:"fetch"`
	FetchTimeout   time.Duration `mapstructure:"fetch_timeout"`
	GitTimeout     time.Duration `mapstructure:"git_timeout"`
	MaxSharedBytes int64         `mapstructure:"max_shared_bytes"`
}
