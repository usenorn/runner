package config

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
)

const (
	envPrefix      = "NORN"
	StateRootEnv   = "NORN_STATE_ROOT"
	stateDirName   = ".norn"
	configFileName = "runner.yaml"

	lowestUnprivilegedPort = 1024
	highestPort            = 65535
)

const (
	defaultUpdateFeed = "https://api.github.com/repos/usenorn/runner/releases/latest"

	defaultScanDepth = 5
	maxScanDepth     = 12
)

var defaultVersion = "dev"

func Version() string {
	return defaultVersion
}

type Overrides struct {
	Capacity *int
	Runtime  *Runtime
}

func New(cfgFile string, overrides Overrides) (Config, error) {
	root, err := bootstrapRoot()
	if err != nil {
		return Config{}, err
	}

	v := viper.New()

	setDefaults(v, root)

	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	read, err := readConfigFile(v, cfgFile, root)
	if err != nil {
		return Config{}, err
	}

	var cfg Config

	decode := viper.DecodeHook(mapstructure.ComposeDecodeHookFunc(
		stringToExtendedDurationHook(),
		stringToByteSizeHook(),
		stringToPortRangeHook(),
		mapstructure.StringToTimeDurationHookFunc(),
		mapstructure.StringToSliceHookFunc(","),
	))

	if err := v.Unmarshal(&cfg, decode); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.State.ConfigFile = read

	if overrides.Capacity != nil {
		cfg.Runner.Capacity = *overrides.Capacity
	}

	if overrides.Runtime != nil {
		cfg.Runner.Runtime = *overrides.Runtime
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func DefaultStateRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}

	return filepath.Join(home, stateDirName), nil
}

func bootstrapRoot() (string, error) {
	if root := strings.TrimSpace(os.Getenv(StateRootEnv)); root != "" {
		return root, nil
	}

	return DefaultStateRoot()
}

func readConfigFile(v *viper.Viper, cfgFile, root string) (string, error) {
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)

		if err := v.ReadInConfig(); err != nil {
			return "", fmt.Errorf("read config file %q: %w", cfgFile, err)
		}

		return cfgFile, nil
	}

	standard := filepath.Join(root, configFileName)

	v.SetConfigFile(standard)

	if err := v.ReadInConfig(); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}

		return "", fmt.Errorf("read config file %q: %w", standard, err)
	}

	return standard, nil
}

func setDefaults(v *viper.Viper, root string) {
	v.SetDefault("version", 1)
	v.SetDefault("server", "https://api.norn.site")
	v.SetDefault("capacity", 2)
	v.SetDefault("runtime", string(RuntimeAuto))
	v.SetDefault("port_range", []int{43000, 44999})
	v.SetDefault("telemetry", string(TelemetryStandard))

	v.SetDefault("retention.workspace_after_done", 30*time.Minute)
	v.SetDefault("retention.runs_max_age", 14*24*time.Hour)
	v.SetDefault("retention.runs_max_disk", int64(20)<<30)

	v.SetDefault("app.version", defaultVersion)
	v.SetDefault("app.log_level", "info")

	v.SetDefault("state.root", root)

	v.SetDefault("log.console", string(ConsoleAuto))
	v.SetDefault("log.max_size_mb", 100)
	v.SetDefault("log.max_backups", 5)
	v.SetDefault("log.max_age_days", 14)
	v.SetDefault("log.compress", true)

	v.SetDefault("control.dial_timeout", 2*time.Second)
	v.SetDefault("control.request_timeout", 10*time.Second)
	v.SetDefault("control.read_header_timeout", 5*time.Second)
	v.SetDefault("control.shutdown_timeout", 15*time.Second)

	v.SetDefault("session.request_timeout", 15*time.Second)
	v.SetDefault("session.refresh_lead", 2*time.Minute)
	v.SetDefault("session.retry_min", 5*time.Second)
	v.SetDefault("session.retry_max", 5*time.Minute)

	v.SetDefault("codebase.scan_depth", defaultScanDepth)
	v.SetDefault("codebase.rescan_interval", 6*time.Hour)
	v.SetDefault("codebase.probe_timeout", 10*time.Second)

	v.SetDefault("update.check", true)
	v.SetDefault("update.interval", 24*time.Hour)
	v.SetDefault("update.timeout", 5*time.Second)
	v.SetDefault("update.feed", defaultUpdateFeed)
}

var extendedDurationPattern = regexp.MustCompile(`^(\d+)([dw])$`)

func stringToExtendedDurationHook() mapstructure.DecodeHookFunc {
	durationType := reflect.TypeOf(time.Duration(0))

	return func(from, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to != durationType {
			return data, nil
		}

		raw := strings.TrimSpace(data.(string))

		match := extendedDurationPattern.FindStringSubmatch(raw)
		if match == nil {
			return data, nil
		}

		count, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, fmt.Errorf("parse duration %q: %w", raw, err)
		}

		span := 24 * time.Hour
		if match[2] == "w" {
			span = 7 * 24 * time.Hour
		}

		return time.Duration(count) * span, nil
	}
}

var byteSizePattern = regexp.MustCompile(`(?i)^(\d+)\s*([kmgt])?i?b?$`)

func stringToByteSizeHook() mapstructure.DecodeHookFunc {
	sizeType := reflect.TypeOf(int64(0))

	return func(from, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to != sizeType {
			return data, nil
		}

		raw := strings.TrimSpace(data.(string))
		if raw == "" {
			return data, nil
		}

		match := byteSizePattern.FindStringSubmatch(raw)
		if match == nil {
			return nil, fmt.Errorf(
				"parse size %q: write a whole number of bytes, or a number with a unit such as "+
					"512MB or 20GB, where every step is 1024 of the one below it",
				raw,
			)
		}

		size, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse size %q: %w", raw, err)
		}

		switch strings.ToLower(match[2]) {
		case "k":
			size <<= 10
		case "m":
			size <<= 20
		case "g":
			size <<= 30
		case "t":
			size <<= 40
		}

		return size, nil
	}
}

func stringToPortRangeHook() mapstructure.DecodeHookFunc {
	rangeType := reflect.TypeOf([2]int{})

	return func(from, to reflect.Type, data any) (any, error) {
		if from.Kind() != reflect.String || to != rangeType {
			return data, nil
		}

		raw := strings.TrimSpace(data.(string))

		lowest, highest, found := strings.Cut(raw, "-")
		if !found {
			lowest, highest, found = strings.Cut(raw, ",")
		}

		if !found {
			return nil, fmt.Errorf("parse port range %q: write it as 43000-44999", raw)
		}

		low, err := strconv.Atoi(strings.TrimSpace(lowest))
		if err != nil {
			return nil, fmt.Errorf("parse port range %q: %w", raw, err)
		}

		high, err := strconv.Atoi(strings.TrimSpace(highest))
		if err != nil {
			return nil, fmt.Errorf("parse port range %q: %w", raw, err)
		}

		return [2]int{low, high}, nil
	}
}

func validate(cfg Config) error {
	if cfg.Runner.Version != 1 {
		return fmt.Errorf(
			"version is %d, and this binary understands 1. A file written for a newer runner "+
				"needs a newer runner to read it",
			cfg.Runner.Version,
		)
	}

	server, err := url.Parse(cfg.Runner.Server)
	if err != nil || server.Scheme == "" || server.Host == "" {
		return fmt.Errorf(
			"server (%q) must be an absolute url such as https://api.norn.site",
			cfg.Runner.Server,
		)
	}

	if server.Scheme != "http" && server.Scheme != "https" {
		return fmt.Errorf("server (%q) must be an http or https url", cfg.Runner.Server)
	}

	if cfg.Runner.Capacity < 1 {
		return fmt.Errorf(
			"capacity is %d, and a runner that may hold no executions can never take work",
			cfg.Runner.Capacity,
		)
	}

	if !slices.Contains(Runtimes(), cfg.Runner.Runtime) {
		return fmt.Errorf("runtime (%q) must be one of auto, process or docker", cfg.Runner.Runtime)
	}

	if !slices.Contains(Telemetries(), cfg.Runner.Telemetry) {
		return fmt.Errorf(
			"telemetry (%q) must be either standard or minimal", cfg.Runner.Telemetry,
		)
	}

	if err := validatePortRange(cfg.Runner.PortRange); err != nil {
		return err
	}

	if err := validateRetention(cfg.Runner.Retention); err != nil {
		return err
	}

	if !slices.Contains(Consoles(), cfg.Log.Console) {
		return fmt.Errorf(
			"log.console (%q) must be one of auto, always or never", cfg.Log.Console,
		)
	}

	if cfg.Log.MaxSizeMB < 1 {
		return fmt.Errorf("log.max_size_mb is %d and must be at least 1", cfg.Log.MaxSizeMB)
	}

	if cfg.App.Version == "" {
		return fmt.Errorf("app.version must not be empty")
	}

	if strings.TrimSpace(cfg.State.Root) == "" {
		return fmt.Errorf("state.root must name a directory the runner may own")
	}

	if err := validateControl(cfg.Control); err != nil {
		return err
	}

	if err := validateSession(cfg.Session); err != nil {
		return err
	}

	if err := validateCodebase(cfg.Codebase); err != nil {
		return err
	}

	return validateUpdate(cfg.Update)
}

func validateCodebase(codebase Codebase) error {
	if codebase.ScanDepth < 1 || codebase.ScanDepth > maxScanDepth {
		return fmt.Errorf(
			"codebase.scan_depth is %d and must be between 1 and %d. A folder is scanned from its "+
				"root downwards, and walking deeper than that costs more than it finds",
			codebase.ScanDepth, maxScanDepth,
		)
	}

	if codebase.RescanInterval <= 0 || codebase.ProbeTimeout <= 0 {
		return fmt.Errorf(
			"codebase.rescan_interval and codebase.probe_timeout must both be positive",
		)
	}

	return nil
}

func validateUpdate(update Update) error {
	if update.Interval <= 0 || update.Timeout <= 0 {
		return fmt.Errorf("update.interval and update.timeout must both be positive")
	}

	if strings.TrimSpace(update.Feed) == "" {
		return fmt.Errorf("update.feed must name where releases are published")
	}

	return nil
}

func validatePortRange(ports [2]int) error {
	if ports[0] < lowestUnprivilegedPort {
		return fmt.Errorf(
			"port_range starts at %d, below %d. Binding there needs privileges a user service "+
				"does not have",
			ports[0], lowestUnprivilegedPort,
		)
	}

	if ports[1] > highestPort {
		return fmt.Errorf("port_range ends at %d, above the highest port %d", ports[1], highestPort)
	}

	if ports[0] >= ports[1] {
		return fmt.Errorf(
			"port_range is %d-%d, which reserves nothing; the first port must be below the second",
			ports[0], ports[1],
		)
	}

	return nil
}

func validateRetention(retention Retention) error {
	if retention.WorkspaceAfterDone <= 0 {
		return fmt.Errorf("retention.workspace_after_done must be positive")
	}

	if retention.RunsMaxAge <= 0 {
		return fmt.Errorf("retention.runs_max_age must be positive")
	}

	if retention.RunsMaxDisk <= 0 {
		return fmt.Errorf("retention.runs_max_disk must be positive")
	}

	return nil
}

func validateSession(session Session) error {
	if session.RequestTimeout <= 0 || session.RefreshLead <= 0 {
		return fmt.Errorf("session.request_timeout and session.refresh_lead must both be positive")
	}

	if session.RetryMin <= 0 || session.RetryMax < session.RetryMin {
		return fmt.Errorf(
			"session.retry_min (%s) must be positive and session.retry_max (%s) must not be shorter",
			session.RetryMin, session.RetryMax,
		)
	}

	return nil
}

func validateControl(control Control) error {
	if control.DialTimeout <= 0 || control.RequestTimeout <= 0 || control.ReadHeaderTimeout <= 0 {
		return fmt.Errorf(
			"control.dial_timeout, control.request_timeout and control.read_header_timeout must " +
				"all be positive",
		)
	}

	if control.ShutdownTimeout <= control.RequestTimeout {
		return fmt.Errorf(
			"control.shutdown_timeout (%s) must be longer than control.request_timeout (%s). A "+
				"request accepted just before the daemon is asked to stop is allowed to run for "+
				"the request timeout, so a shorter drain guarantees killing it mid-flight",
			control.ShutdownTimeout, control.RequestTimeout,
		)
	}

	return nil
}
