package config

import (
	"crypto/tls"
	"fmt"
	"os"
	"time"

	"git.sr.ht/~spc/go-log"
	"github.com/redhatinsights/yggdrasil/internal/constants"
	"github.com/rjeczalik/notify"
)

const (
	FlagNameLogLevel       = "log-level"
	FlagNameCertFile       = "cert-file"
	FlagNameKeyFile        = "key-file"
	FlagNameCaRoot         = "ca-root"
	FlagNameServer         = "server"
	FlagNameClientID       = "client-id"
	FlagNamePathPrefix     = "path-prefix"
	FlagNameProtocol       = "protocol"
	FlagNameDataHost       = "data-host"
	FlagNameCanonicalFacts = "canonical-facts"
	FlagNameHTTPRetries    = "http-retries"
	FlagNameHTTPTimeout    = "http-timeout"
)

var DefaultConfig = Config{
	PathPrefix: constants.DefaultPathPrefix,
}

// Config contains current configuration state for yggdrasil.
type Config struct {
	// LogLevel is the level value used for logging.
	LogLevel string

	// ClientID is a unique identification value for the client over connection
	// transports.
	ClientID string

	// Server is a URI to which yggd connects in order to send and receive data.
	Server []string

	// CertFile is a path to a public certificate, optionally used along with
	// KeyFile to authenticate connections.
	CertFile string

	// KeyFile is a path to a private certificate, optionally used along with
	// CertFile to authenticate connections.
	KeyFile string

	// CARoot is the list of paths with chain certificate file to optionally
	// include in the TLS configration's CA root list.
	CARoot []string

	// PathPrefix is a value prepended to all path names at the transport layer.
	PathPrefix string

	// Protocol is the protocol used by yggd when connecting to Server. Can be
	// either MQTT or HTTP.
	Protocol string

	// DataHost is a hostname value to interject into all HTTP requests when
	// handling data retrieval for "detachedContent" workers.
	DataHost string

	// CanonicalFacts is a path to a JSON file containing "canonical facts",
	// a set of facts about the system used to uniquely identify it.
	CanonicalFacts string

	// HTTPRetries is the number of times the client will attempt to resend
	// failed HTTP requests before giving up.
	HTTPRetries int

	// HTTPTimeout is the duration the client will wait before cancelling an
	// HTTP request.
	HTTPTimeout time.Duration
}

func (conf *Config) CreateTLSConfig() (*tls.Config, error) {
	var certData, keyData []byte
	var err error
	rootCAs := make([][]byte, 0)

	if conf.CertFile != "" && conf.KeyFile != "" {
		certData, err = os.ReadFile(conf.CertFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read cert-file '%v': %w", conf.CertFile, err)
		}

		keyData, err = os.ReadFile(conf.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read key-file '%v': %w", conf.KeyFile, err)
		}
	}

	for _, file := range conf.CARoot {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("cannot read ca-file '%v': %w", file, err)
		}
		rootCAs = append(rootCAs, data)
	}

	tlsConfig, err := newTLSConfig(certData, keyData, rootCAs)
	if err != nil {
		return nil, err
	}

	return tlsConfig, nil
}

// WatcherUpdate creates a Inotify watcher on all TLS related information
// (Cert-file, key-file and CA-root) if any of those files are updated, it'll
// send over the returned channel a new TLS.Config that consumers can use to
// renew their connections.
// The main use case if when on short-lived certificates, where a connection
// need to be reloaded to create a new TLSHandshake
// It will return a error if cannot set the inoty on any file
func (conf *Config) WatcherUpdate() (chan *tls.Config, error) {
	c := make(chan notify.EventInfo, 1)
	files := []string{}

	if len(conf.CARoot) > 0 {
		files = append(files, conf.CARoot...)
	}

	if conf.CertFile != "" {
		files = append(files, conf.CertFile)
	}

	if conf.KeyFile != "" {
		files = append(files, conf.KeyFile)
	}

	if len(files) == 0 {
		return nil, nil
	}

	for _, fp := range files {
		if err := notify.Watch(fp, c, notify.InCloseWrite, notify.InDelete); err != nil {
			return nil, fmt.Errorf("cannot start watching file '%v': %w", fp, err)
		}
		log.Debugf("added watchpoint for file: %v", fp)
	}

	events := make(chan *tls.Config, 1)
	go func() {
		for e := range c {
			log.Debugf("received inotify event %v", e.Event())
			switch e.Event() {
			case notify.InCloseWrite, notify.InDelete:
				cfg, err := conf.CreateTLSConfig()
				if err != nil {
					log.Errorf("cannot create TLS config from file '%v' on event %v: %v", e.Path(), e.Event(), err)
				}
				if cfg != nil {
					events <- cfg
				}
			}
		}
	}()

	return events, nil
}
