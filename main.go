package main // import "github.com/rs/jplot"

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/monochromegane/terminal"
	"github.com/r3labs/sse/v2"
	"github.com/rs/jplot/data"
)

//go:embed index.html
var webUI string

func main() {
	flag.Usage = func() {
		out := os.Stderr
		fmt.Fprintln(out, "Usage: jplot [OPTIONS] FIELD_SPEC [FIELD_SPEC...]:")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "OPTIONS:")
		flag.PrintDefaults()
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "FIELD_SPEC: [<option>[,<option>...]:]path")
		fmt.Fprintln(out, "  option:")
		fmt.Fprintln(out, "    - counter: Computes the difference with the last value. The value must increase monotonically.")
		fmt.Fprintln(out, "    - marker: When the value is none-zero, a vertical line is drawn.")
		fmt.Fprintln(out, "  path:")
		fmt.Fprintln(out, "    JSON field path (eg: field.sub-field).")
	}
	url := flag.String("url", "", "URL to fetch every second. Read JSON objects from stdin if not specified.")
	interval := flag.Duration("interval", time.Second, "When url is provided, defines the interval between fetches."+
		" Note that counter fields are computed based on this interval.")
	steps := flag.Int("steps", 100, "Number of values to plot.")
	port := flag.Int("port", 8080, "Port to run server on.")
	flag.Parse()

	if len(flag.Args()) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	specs, err := data.ParseSpec(flag.Args())
	if err != nil {
		fatal("Cannot parse spec: ", err)
	}

	var dp *data.Points
	target := make(chan data.Point)
	if *url != "" {
		dp = data.FromHTTP(target, *url, *interval, *steps)
	} else if !terminal.IsTerminal(os.Stdin) {
		dp = data.FromStdin(target, *steps)
	} else {
		fatal("neither --url nor stdin is provided")
	}

	sseServer := sse.New()
	dataPointsStream := sseServer.CreateStream("data-points")
	http.HandleFunc("/events", sseServer.ServeHTTP)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(webUI)); err != nil {
			http.Error(w, "Failed to response with file", 503)
		}
	})

	go func() {
		log.Printf("Listening on http://localhost:%d", *port)
		log.Fatal(http.ListenAndServe(fmt.Sprintf(":%d", *port), nil))
	}()
	cmd := exec.Command("open", fmt.Sprintf("http://localhost:%d", *port))
	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to open http://localhost:%d: %v", *port, err)
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	defer wg.Wait()
	exit := make(chan struct{})
	defer close(exit)
	go func() {
		defer wg.Done()
		t := time.NewTicker(time.Second)
		defer t.Stop()
		c := make(chan os.Signal, 2)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		for eventID := 1; ; eventID++ {
			select {
			case value := <-target:
				data, err := json.Marshal(value)
				if err != nil {
					log.Printf("Failed to JSONify value %v: %v", value, err)
					continue
				}
				sseServer.Publish(dataPointsStream.ID, &sse.Event{
					ID:   []byte(strconv.Itoa(eventID)),
					Data: data,
				})
			case <-exit:
				return
			case <-c:
				dp.Close()
				signal.Stop(c)
			}
		}
	}()

	if err := dp.Run(specs); err != nil {
		fatal("Data source error: ", err)
	}
}

func fatal(a ...interface{}) {
	fmt.Println(append([]interface{}{"jplot: "}, a...)...)
	os.Exit(1)
}
