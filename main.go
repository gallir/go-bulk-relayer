package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gallir/smart-relayer/lib"
	"github.com/gallir/smart-relayer/redis/cluster"
	"github.com/gallir/smart-relayer/redis/radix"
)

const (
	version = "4.0.6"
)

var (
	statusListener = ":9091"
	relayers       = make(map[string]lib.Relayer)
	totalRelayers  = 0
	relayersConfig *lib.Config
	done           = make(chan bool)
	reloadSig      = make(chan os.Signal, 1)
	exitSig        = make(chan os.Signal, 1)
)

func getNewServer(conf lib.RelayerConfig) (srv lib.Relayer, err error) {
	switch conf.Protocol {
	case "redis", "redis2":
		srv, err = redis2.New(conf, done)
	case "redis-cluster", "redis-plus":
		srv, err = cluster.New(conf, done)
	default:
		err = errors.New("no valid option")
	}
	return
}

func startOrReload() bool {
	// Check config is OK
	newConf, err := lib.ReadConfig(lib.GlobalConfig.ConfigFileName)
	if err != nil {
		log.Println("Bad configuration", err)
		return false
	}

	statusListener = newConf.StatusListener

	newEndpoints := make(map[string]bool)

	for _, conf := range newConf.Relayer {
		endpoint, ok := relayers[conf.Listen]
		newEndpoints[conf.Listen] = true
		if !ok {
			// Start a new relayer
			r, err := getNewServer(conf)
			if err != nil {
				log.Println("Error starting relayer", conf.Protocol, err)
				continue
			}
			lib.Debugf("Starting new relayer from %s to %s", conf.Listen, conf.URL)
			totalRelayers++
			if e := r.Start(); e == nil {
				relayers[conf.Listen] = r
			}
		} else {
			// The relayer exists, reload it
			endpoint.Reload(&conf)
		}
	}

	for endpoint, r := range relayers {
		_, ok := newEndpoints[endpoint]
		if !ok {
			log.Printf("Deleting old endpoint %s", endpoint)
			delete(relayers, endpoint)
			r.Exit()
		}
	}

	return true
}

func main() {
	// Force a high number of file descriptoir, if possible
	var rLimit syscall.Rlimit
	e := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if e == nil {
		rLimit.Cur = 65536
		syscall.Setrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	}

	// Show version and exit
	if lib.GlobalConfig.ShowVersion {
		fmt.Println("smart-relayer version", version)
		fmt.Printf("Max files %d/%d\n", rLimit.Cur, rLimit.Max)
		os.Exit(0)
	}

	// Run http server for stats
	go runStatus()

	if !startOrReload() {
		os.Exit(1)
	}

	// Listen for reload signals
	signal.Notify(reloadSig, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGUSR2)
	signal.Notify(exitSig, syscall.SIGINT, syscall.SIGKILL, os.Interrupt, syscall.SIGTERM)

	// Reload config
	go func() {
		for {
			_ = <-reloadSig
			startOrReload()
		}
	}()

	// Exit
	go func() {
		for {
			s := <-exitSig
			log.Printf("Signal %d received, exiting", s)
			for _, r := range relayers {
				r.Exit()
			}
		}
	}()

	for i := 0; i < totalRelayers; i++ {
		<-done
	}
	os.Exit(0)
}
