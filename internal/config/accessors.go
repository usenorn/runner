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

func NewSpool(cfg Config) Spool { return cfg.Spool }

func NewScheduler(cfg Config) Scheduler { return cfg.Scheduler }
