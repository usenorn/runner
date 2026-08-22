package config

import "github.com/goforj/wire"

var Set = wire.NewSet(New, NewRunner, NewApp, NewState, NewLog, NewControl, NewSession, NewUpdate, NewCodebase, NewSnapshot, NewChannel, NewSpool, NewScheduler, NewSupervisor)
