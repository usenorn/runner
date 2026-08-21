package config

func NewRunner(cfg Config) Runner { return cfg.Runner }

func NewApp(cfg Config) App { return cfg.App }

func NewState(cfg Config) State { return cfg.State }

func NewLog(cfg Config) Log { return cfg.Log }

func NewControl(cfg Config) Control { return cfg.Control }
