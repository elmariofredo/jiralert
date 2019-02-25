package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"

	_ "net/http/pprof"

	log "github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sysincz/jiralert"
	"github.com/sysincz/jiralert/alertmanager"
)

const (
	unknownReceiver = "<unknown>"
)

var (
	listenAddress = flag.String("listen-address", ":9097", "The address to listen on for HTTP requests.")
	configFile    = flag.String("config", "config/jiralert.yml", "The JIRAlert configuration file")
	checkConfig   = flag.Bool("check-config", false, "Check JIRAlert configuration file and templates that defined in flag -config")

	// Version is the build version, set by make to latest git tag/hash via `-ldflags "-X main.Version=$(VERSION)"`.
	Version = "<local build>"
)

func main() {
	if os.Getenv("DEBUG") != "" {
		runtime.SetBlockProfileRate(1)
		runtime.SetMutexProfileFraction(1)
	}

	// Override -alsologtostderr default value.
	if alsoLogToStderr := flag.Lookup("alsologtostderr"); alsoLogToStderr != nil {
		alsoLogToStderr.DefValue = "true"
		alsoLogToStderr.Value.Set("true")
	}
	flag.Parse()

	log.Infof("Starting JIRAlert version %s", Version)

	mux := http.ServeMux{}

	reload := func() (err error) {
		log.Infof("Loading configuration file %s", *configFile)

		config, _, err := jiralert.LoadConfigFile(*configFile)
		if err != nil {
			log.Errorf("Error loading configuration: %s", err)
			return err
		}

		tmpl, err := jiralert.LoadTemplate(config.Template)
		if err != nil {
			log.Errorf("Error loading templates from %s: %s", config.Template, err)
			return err
		}

		if *checkConfig {
			log.Infof("All check are passed")
			os.Exit(0)
		}

		mux = http.ServeMux{}
		mux.HandleFunc("/alert", func(w http.ResponseWriter, req *http.Request) {
			log.V(1).Infof("Handling /alert webhook request")
			defer req.Body.Close()

			// https://godoc.org/github.com/prometheus/alertmanager/template#Data
			data := alertmanager.Data{}
			if err := json.NewDecoder(req.Body).Decode(&data); err != nil {
				errorHandler(w, http.StatusBadRequest, err, unknownReceiver, &data)
				return
			}

			conf := config.ReceiverByName(data.Receiver)
			if conf == nil {
				errorHandler(w, http.StatusNotFound, fmt.Errorf("Receiver missing: %s", data.Receiver), unknownReceiver, &data)
				return
			}
			log.V(1).Infof("Matched receiver: %q", conf.Name)

			// Filter out resolved alerts, not interested in them.
			alerts := data.Alerts.Firing()
			if len(alerts) < len(data.Alerts) {
				log.Warningf("Please set \"send_resolved: false\" on receiver %s in the Alertmanager config", conf.Name)
				data.Alerts = alerts
			}

			if len(data.Alerts) > 0 {
				r, err := jiralert.NewReceiver(conf, tmpl)
				if err != nil {
					errorHandler(w, http.StatusInternalServerError, err, conf.Name, &data)
				}
				if retry, err := r.Notify(&data); err != nil {
					var status int
					if retry {
						status = http.StatusServiceUnavailable
					} else {
						status = http.StatusInternalServerError
					}
					errorHandler(w, status, err, conf.Name, &data)
					return
				}
			}

			requestTotal.WithLabelValues(conf.Name, "200").Inc()
		})

		mux.HandleFunc("/", HomeHandlerFunc())
		mux.HandleFunc("/config", ConfigHandlerFunc(config))
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { http.Error(w, "OK", http.StatusOK) })
		mux.Handle("/metrics", promhttp.Handler())
		mux.HandleFunc("/reload", func(w http.ResponseWriter, r *http.Request) {
			//w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("OK"))
			log.Info("Reload OK /reload")
			//serv.Shutdown(context.Background())
			syscall.Kill(syscall.Getpid(), syscall.SIGHUP)

		})

		return nil
	}

	err := reload()
	if err != nil {
		os.Exit(1)
	}

	if os.Getenv("PORT") != "" {
		*listenAddress = ":" + os.Getenv("PORT")
	}

	log.Infof("Listening on %s", *listenAddress)

	go listen(*listenAddress, &mux)

	var (
		hup      = make(chan os.Signal)
		hupReady = make(chan bool)
		term     = make(chan os.Signal, 1)
	)
	signal.Notify(hup, syscall.SIGHUP)
	signal.Notify(term, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-hupReady
		for {
			select {
			case <-hup:
				log.Infof("Received SIGHUP, reloading...")
				reload()
			}
		}
	}()

	// Wait for reload or termination signals.
	close(hupReady) // Unblock SIGHUP handler.

	<-term

	log.Infof("Received SIGTERM, exiting gracefully...")

}

func listen(listen string, mux *http.ServeMux) {
	err := http.ListenAndServe(listen, mux)
	if err != nil {
		log.Errorf("Error Listening: %s", err)
		os.Exit(1)
	}
}

func errorHandler(w http.ResponseWriter, status int, err error, receiver string, data *alertmanager.Data) {
	w.WriteHeader(status)

	response := struct {
		Error   bool
		Status  int
		Message string
	}{
		true,
		status,
		err.Error(),
	}
	// JSON response
	bytes, _ := json.Marshal(response)
	json := string(bytes[:])
	fmt.Fprint(w, json)

	log.Errorf("%d %s: err=%s receiver=%q groupLabels=%+v", status, http.StatusText(status), err, receiver, data.GroupLabels)
	requestTotal.WithLabelValues(receiver, strconv.FormatInt(int64(status), 10)).Inc()
}
