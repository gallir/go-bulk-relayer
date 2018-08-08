package redis2

import (
	"errors"
	"log"
	"net"
	"sync"
	"time"

	"github.com/gallir/smart-relayer/lib"
	"github.com/gallir/smart-relayer/redis/radix.improved/redis"
)

// Server is the thread that listen for clients' connections
type Server struct {
	sync.Mutex
	config   lib.RelayerConfig
	pool     *Pool
	mode     int
	done     chan bool
	exiting  bool
	listener net.Listener
}

const (
	connectionRetries = 3
	requestBufferSize = 1024
	listenTimeout     = 0 * time.Second // Don't timeout on local clients
	connectTimeout    = 5 * time.Second
	maxIdle           = 120 * time.Second
	selectCommand     = "SELECT"
	evalCommand       = "EVAL"
)

var (
	errBadCmd      = errors.New("ERR bad command")
	errKO          = errors.New("fatal error")
	errOverloaded  = errors.New("Redis overloaded")
	respOK         = redis.NewRespSimple("OK")
	respTrue       = redis.NewResp(1)
	respBadCommand = redis.NewResp(errBadCmd)
	respKO         = redis.NewResp(errKO)
	commands       map[string]lib.AsyncData
)

func init() {
	// These are the commands that can be sent in "background" when in smart mode
	// The values are the immediate responses to the clients
	commands = map[string]lib.AsyncData{
		"SET":       {Resp: respOK},
		"SETEX":     {Resp: respOK},
		"PSETEX":    {Resp: respOK},
		"MSET":      {Resp: respOK},
		"HMSET":     {Resp: respOK},
		"SELECT":    {Resp: respOK},
		"HSET":      {Resp: respTrue},
		"SADD":      {Resp: respTrue},
		"ZADD":      {Resp: respTrue},
		"EXPIRE":    {Resp: respTrue},
		"EXPIREAT":  {Resp: respTrue},
		"PEXPIRE":   {Resp: respTrue},
		"PEXPIREAT": {Resp: respTrue},
	}
}

// New creates a new Redis local server
func New(c lib.RelayerConfig, done chan bool) (*Server, error) {
	srv := &Server{
		done: done,
	}
	srv.Reload(&c)
	return srv, nil
}

// Start accepts incoming connections on the Listener
func (srv *Server) Start() (e error) {
	srv.Lock()
	defer srv.Unlock()

	srv.listener, e = lib.NewListener(srv.config)
	if e != nil {
		return e
	}

	// Serve clients
	go func(l net.Listener) {
		defer srv.listener.Close()
		for {
			netConn, e := l.Accept()
			if e != nil {
				if netErr, ok := e.(net.Error); ok && netErr.Timeout() {
					// Paranoid, ignore timeout errors
					log.Println("Timeout at local listener", srv.config.ListenHost(), e)
					continue
				}
				if srv.exiting {
					log.Println("Exiting local listener", srv.config.ListenHost())
					return
				}
				log.Fatalln("Emergency error in local listener", srv.config.ListenHost(), e)
				return
			}
			go srv.handleConnection(netConn)
		}
	}(srv.listener)

	return nil
}

// Reload the configuration
func (srv *Server) Reload(c *lib.RelayerConfig) error {
	srv.Lock()
	defer srv.Unlock()

	reset := false
	if srv.config.URL != c.URL {
		reset = true
	}

	srv.config = *c
	srv.mode = c.Type()

	if reset {
		if srv.pool != nil {
			log.Printf("Reset redis server at port %s for target %s", srv.config.Listen, srv.config.Host())
			srv.pool.Reset()
		}
		srv.pool = NewPool(c)
	} else {
		log.Printf("Reload redis config at port %s for target %s", srv.config.Listen, srv.config.Host())
		srv.pool.ReadConfig(c)
	}

	return nil
}

// Exit closes the listener and send done to main
func (srv *Server) Exit() {
	srv.exiting = true
	if srv.listener != nil {
		srv.listener.Close()
	}
	srv.done <- true
}

func (srv *Server) handleConnection(netCon net.Conn) {
	defer netCon.Close()

	reader := redis.NewRespReader(netCon)
	client := srv.pool.Get()
	defer srv.pool.Put(client)

	currentDB := 0

	for {
		r := reader.Read()
		if r.IsType(redis.IOErr) {
			if redis.IsTimeout(r) {
				// Paranoid, don't close it just log it
				log.Println("Local client listen timeout at", srv.config.Listen)
				continue
			}
			// Connection was closed
			return
		}

		req := lib.NewRequest(r, &srv.config)
		if req == nil {
			respBadCommand.WriteTo(netCon)
			continue
		}

		if req.Database != lib.UnknownDB && req.Database != currentDB {
			currentDB = req.Database
		}
		req.Database = currentDB

		// Smart mode, answer immediately and forget
		if srv.mode == lib.ModeSmart {
			fastResponse, ok := commands[req.Command]
			if ok {
				fastResponse.Resp.WriteTo(netCon)
				client.send(req)
				continue
			}
		}

		// Synchronized mode
		req.Conn = netCon

		e := client.send(req)
		if e != nil {
			redis.NewResp(e).WriteTo(netCon)
			continue
		}
	}
}

func sendRequest(c chan *lib.Request, r *lib.Request) (ok bool) {
	defer func() {
		e := recover() // To avoid panic due to closed channels
		if e != nil {
			log.Printf("sendRequest: Recovered from error %s, channel length %d", e, len(c))
			ok = false
		}
	}()

	if c == nil {
		lib.Debugf("Nil channel in send request")
		return false
	}

	c <- r
	return true
}
