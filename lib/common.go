package lib

import "flag"

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

type MainConfig struct {
	ConfigFileName string
	Debug          bool
	ShowVersion    bool
}

// GlobalConfig is the configuration for the main programm
var GlobalConfig MainConfig

func init() {
	flag.StringVar(&GlobalConfig.ConfigFileName, "c", "relayer.conf", "Configuration filename")
	flag.BoolVar(&GlobalConfig.Debug, "d", false, "Show debug info")
	flag.BoolVar(&GlobalConfig.ShowVersion, "v", false, "Show version and exit")
	flag.Parse()
}
