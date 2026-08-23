package config

func NewRunner(cfg Config) Runner { return cfg.Runner }

func NewApp(cfg Config) App { return cfg.App }

func NewState(cfg Config) State { return cfg.State }

func NewLog(cfg Config) Log { return cfg.Log }

func NewControl(cfg Config) Control { return cfg.Control }

func NewSession(cfg Config) Session { return cfg.Session }

func NewUpdate(cfg Config) Update { return cfg.Update }

func NewCodebase(cfg Config) Codebase { return cfg.Codebase }

func NewSnapshot(cfg Config) Snapshot { return cfg.Snapshot }

func NewChannel(cfg Config) Channel { return cfg.Channel }

func NewTunnel(cfg Config) Tunnel { return cfg.Tunnel }

func NewSpool(cfg Config) Spool { return cfg.Spool }

func NewScheduler(cfg Config) Scheduler { return cfg.Scheduler }

func NewSupervisor(cfg Config) Supervisor { return cfg.Supervisor }

func NewDriver(cfg Config) Driver { return cfg.Driver }

func NewQuestions(cfg Config) Questions { return cfg.Questions }

func NewUpload(cfg Config) Upload { return cfg.Upload }

func NewResults(cfg Config) Results { return cfg.Results }
