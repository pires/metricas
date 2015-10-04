package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"runtime/pprof"

	"github.com/pires/metricas/service"
	"github.com/pires/metricas/timeseries"
)

var (
	cpuprofile = flag.String("cpuprofile", "", "write cpu profile to file")
	db         = flag.String("db", "localhost:8086", "InfluxDB address (host:port)")
	dbUser     = flag.String("db_user", "", "Optional user to access InfluxDB")
	dbPwd      = flag.String("db_pwd", "", "Optional user password to access InfluxDB")
	dbName     = flag.String("db_name", "metrics", "InfluxDB database to write to")
	nats       = flag.String("nats", "localhost:4222", "NATS adress (host:port)")
)

func main() {
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	config := &service.Configuration{
		AddrNats: *nats,
		TimeSeriesConfig: &timeseries.Configuration{
			AddrInfluxDb: *db,
			DbUser:       *dbUser,
			DbPwd:        *dbPwd,
			DbName:       *dbName,
		},
	}

	log.Println("Starting metrics service...")
	svc, err := service.NewMetricsService(config)
	if err != nil {
		log.Fatalln(err)
	}
	defer close(svc)

	log.Printf("Press ^C to quit.")
	// wait for Ctrl-c to stop server
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, os.Kill)

	<-c
	log.Println("Terminated metrics server.")
}
