package coordinator

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	log "github.com/Sirupsen/logrus"
	"github.com/mistifyio/mistify/acomm"
	"github.com/tylerb/graceful"
)

// Server is the coordinator server. It handles accepting internal and external
// requests and proxying them to appropriate providers.
type Server struct {
	config   *Config
	proxy    *acomm.Tracker
	internal *acomm.UnixListener
	external *graceful.Server
}

// NewServer creates and initializes a new instance of Server.
func NewServer(config *Config) (*Server, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	var err error
	s := &Server{
		config: config,
	}

	// Internal socket for requests from providers
	internalSocket := filepath.Join(
		config.SocketDir(),
		"coordinator",
		config.ServiceName()+".sock")
	s.internal = acomm.NewUnixListener(internalSocket, 0)

	// External server for requests from outside
	mux := http.NewServeMux()
	mux.HandleFunc("/stream", acomm.ProxyStreamHandler)
	mux.HandleFunc("/", s.externalHandler)
	s.external = &graceful.Server{
		Server: &http.Server{
			Addr:    fmt.Sprintf(":%d", config.ExternalPort()),
			Handler: mux,
		},
		NoSignalHandling: true,
	}

	// Response socket for proxied requests
	responseSocket := filepath.Join(
		config.SocketDir(),
		"response",
		config.ServiceName()+".sock")

	streamURL, err := url.ParseRequestURI(
		fmt.Sprintf("http://%s:%d/stream", getLocalIP(), config.ExternalPort()))
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("failed to generate stream url")
	}
	s.proxy, err = acomm.NewTracker(responseSocket, streamURL, config.RequestTimeout())
	if err != nil {
		return nil, err
	}
	log.WithFields(log.Fields{
		"response": responseSocket,
		"stream":   streamURL.String(),
		"internal": internalSocket,
		"external": fmt.Sprintf(":%d", config.ExternalPort()),
	}).Info("server addresses")

	return s, nil
}

// externalHandler is the http handler for external requests.
func (s *Server) externalHandler(w http.ResponseWriter, r *http.Request) {
	var respErr error
	req := &acomm.Request{}

	// Send the immediate response
	defer func() {
		resp, err := acomm.NewResponse(req, nil, nil, respErr)
		respJSON, err := json.Marshal(resp)
		if err != nil {
			log.WithFields(log.Fields{
				"error":    err,
				"req":      req,
				"response": resp,
			}).Error("failed to marshal initial response")
		}

		if _, err := w.Write(respJSON); err != nil {
			log.WithFields(log.Fields{
				"error":    err,
				"req":      req,
				"response": resp,
			}).Error("failed to send initial response")
		}
	}()

	// Parse the request
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		respErr = err
		return
	}

	if err := json.Unmarshal(body, req); err != nil {
		respErr = err
		return
	}

	respErr = s.handleRequest(req)
}

func (s *Server) internalHandler() {
	for {
		conn := s.internal.NextConn()
		if conn == nil {
			return
		}
		go s.acceptInternalRequest(conn)
	}
}

func (s *Server) acceptInternalRequest(conn net.Conn) {
	defer s.internal.DoneConn(conn)
	var respErr error
	req := &acomm.Request{}
	defer func() {
		// Respond to the initial request
		resp, err := acomm.NewResponse(req, nil, nil, respErr)
		if err != nil {
			log.WithFields(log.Fields{
				"error":   err,
				"req":     req,
				"respErr": respErr,
			}).Error("failed to create initial response")
			return
		}

		if err := acomm.SendConnData(conn, resp); err != nil {
			log.WithFields(log.Fields{
				"error":   err,
				"req":     req,
				"respErr": respErr,
			}).Error("failed to create initial response")
			return
		}
	}()

	if err := acomm.UnmarshalConnData(conn, req); err != nil {
		respErr = err
		return
	}

	if err := req.Validate(); err != nil {
		respErr = err
		return
	}

	if err := s.handleRequest(req); err != nil {
		respErr = err
		return
	}

	return
}

// handleRequest handles proxying and forwarding a request to a provider for
// the specified task.
func (s *Server) handleRequest(req *acomm.Request) error {
	providerSockets, err := s.getProviders(req.Task)
	if err != nil {
		return err
	}

	if len(providerSockets) == 0 {
		return errors.New("no providers available for task")
	}

	proxyReq, err := s.proxy.ProxyUnix(req, 0)
	if err != nil {
		return err
	}

	// Cycle through available providers until one accepts the request
	for _, providerSocket := range providerSockets {
		addr, _ := url.ParseRequestURI(fmt.Sprintf("unix://%s", providerSocket))
		err = acomm.Send(addr, proxyReq)
		if err == nil {
			// Successfully sent
			break
		}
	}

	return err
}

// getProviders returns a list of providers registered for a given task.
func (s *Server) getProviders(task string) ([]string, error) {
	// Find Task Providers
	if task == "" {
		return nil, errors.New("request missing task")
	}

	taskSocketDir := filepath.Join(s.config.SocketDir(), task)
	files, err := ioutil.ReadDir(taskSocketDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		log.WithFields(log.Fields{
			"error": err,
			"req":   task,
			"dir":   taskSocketDir,
		}).Warn("failed to read task dir")
		return nil, err
	}

	// Filter out any non-socket files
	providerSockets := make([]string, 0, len(files))
	for _, fi := range files {
		if fi.Mode()&os.ModeSocket == os.ModeSocket {
			providerSockets = append(providerSockets, filepath.Join(taskSocketDir, fi.Name()))
		}
	}

	return providerSockets, nil
}

// externalListenAndServe runs and blocks on the external http server.
func (s *Server) externalListenAndServe() {
	if err := s.external.ListenAndServe(); err != nil {
		// Ignore the error from closing the listener, which is involved in the
		// graceful shutdown
		if !strings.Contains(err.Error(), "use of closed network connection") {
			log.WithField("error", err).Error("server error")

			// Stop the coordinator if this was unexpected
			s.Stop()
		}
	}
}

// Start starts the server, running all of the listeners and proxy tracker.
func (s *Server) Start() error {
	// Start up the proxy tracker
	if err := s.proxy.Start(); err != nil {
		return err
	}

	// Start up the internal request handler
	if err := s.internal.Start(); err != nil {
		return err
	}
	go s.internalHandler()

	// Start up the external request handler
	go s.externalListenAndServe()
	return nil
}

// Stop stops the server, gracefully stopping all of the listeners and proxy
// tracker.
func (s *Server) Stop() {
	// Stop accepting new external requests
	stopChan := s.external.StopChan()
	s.external.Stop(0)
	<-stopChan

	// Stop accepting new internal requests
	s.internal.Stop(0)

	// Stop the proxy tracker
	s.proxy.Stop()
}

// StopOnSignal will wait until one of the specified signals is received and
// then stop the server. If no signals are specified, it will use a default
// set.
func (s *Server) StopOnSignal(signals ...os.Signal) {
	if len(signals) == 0 {
		signals = []os.Signal{os.Interrupt, os.Kill, syscall.SIGTERM}
	}

	sigChan := make(chan os.Signal)
	signal.Notify(sigChan, signals...)
	sig := <-sigChan
	log.WithFields(log.Fields{
		"signal": sig,
	}).Info("signal received, stopping")

	s.Stop()
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		log.WithFields(log.Fields{
			"error": err,
		}).Error("failed to list interface addrs")
		return ""
	}

	for _, address := range addrs {
		if ipnet, ok := address.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return ""
}
