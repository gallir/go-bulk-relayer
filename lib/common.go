package lib

import (
	"flag"
	"github.com/gallir/smart-relayer/redis/radix.improved/redis"
)

type Relayer interface {
	Start() error
	Reload(*RelayerConfig) error
	Exit()
}

type RelayerClient interface {
	IsValid() bool
	Exit()
	Send(r interface{}) error
	Reload(*RelayerConfig)
}

type AsyncData struct {
	Resp          *redis.Resp
	ActualCommand string
}

type MainConfig struct {
	ConfigFileName string
	Debug          bool
	ShowVersion    bool
}

const (
	MinCompressSize = 256
)

var (
	// GlobalConfig is the configuration for the main programm
	GlobalConfig MainConfig
)

func init() {
	flag.StringVar(&GlobalConfig.ConfigFileName, "c", "relayer.conf", "Configuration filename")
	flag.BoolVar(&GlobalConfig.Debug, "d", false, "Show debug info")
	flag.BoolVar(&GlobalConfig.ShowVersion, "v", false, "Show version and exit")
	flag.Parse()
}
