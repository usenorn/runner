package control

import "github.com/goforj/wire"

var Set = wire.NewSet(NewServer, NewClient, NewBearer)
