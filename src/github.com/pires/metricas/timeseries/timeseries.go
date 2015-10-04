package timeseries

import (
	"net/url"
	"time"

	influxdb "github.com/influxdb/influxdb/client"
)

const (
	FLUSH_INTERVAL_MS = 5000 // flush every 5 seconds
	FLUSH_MAX_POINTS  = 1024 // or flush when we reach 1024 points
)

type Configuration struct {
	AddrInfluxDb string
	DbUser       string
	DbPwd        string
	DbName       string
}

type TimeSeries interface {
	Points() chan<- *influxdb.Point
	Stop() chan struct{}
}

type timeseries struct {
	config    *Configuration
	db        *influxdb.Client
	pointsBuf []influxdb.Point
	// channels
	pointsChan chan *influxdb.Point
	stop       chan struct{}
}

func NewTimeSeries(config *Configuration) (TimeSeries, error) {
	// validate InfluxDB url
	u, err := url.Parse("http://" + config.AddrInfluxDb)
	if err != nil {
		return nil, err
	}

	// connect to InfluxDB
	cconfig := influxdb.Config{
		URL:      *u,
		Username: config.DbUser,
		Password: config.DbPwd,
	}
	client, err := influxdb.NewClient(cconfig)
	if err != nil {
		return nil, err
	}
	// we may have connected, but let's ping
	_, _, err = client.Ping()
	if err != nil {
		return nil, err
	}
	// we're good to go
	ts := &timeseries{
		config:     config,
		db:         client,
		pointsBuf:  make([]influxdb.Point, 0, FLUSH_MAX_POINTS),
		pointsChan: make(chan *influxdb.Point),
		stop:       make(chan struct{}),
	}

	// handle incoming metrics
	go ts.run(FLUSH_INTERVAL_MS, FLUSH_MAX_POINTS)

	return ts, nil
}

func (ts *timeseries) Points() chan<- *influxdb.Point {
	return ts.pointsChan
}

func (ts *timeseries) Stop() chan struct{} {
	return ts.stop
}

// Handles incoming metrics in batches
// TODO handle write errors
// TODO implement pool of flushers
func (ts *timeseries) run(flushInterval int, flushMaxPoints int) {
	flushTimeout := time.NewTicker(time.Duration(flushInterval) * time.Millisecond)
	for {
		select {
		case <-ts.stop:
			ts.flush()
			flushTimeout.Stop()
			return
		case point := <-ts.pointsChan:
			ts.pointsBuf = append(ts.pointsBuf, *point)
			if len(ts.pointsBuf) == flushMaxPoints {
				ts.flush()
			}
		case <-flushTimeout.C:
			// is there anything to flush?
			if len(ts.pointsBuf) > 0 {
				ts.flush()
			}
		}
	}
}

// Writes a batch of points to InfluxDB
func (ts *timeseries) flush() {
	batch := influxdb.BatchPoints{
		Points:          ts.pointsBuf,
		Database:        ts.config.DbName,
		RetentionPolicy: "default",
	}
	r, err := ts.db.Write(batch)
	if err != nil {
		// TODO handle errors writing to InfluxDB
		println("Error", err.Error(), r.Error())
	}
	// empty slice
	ts.pointsBuf = nil // TODO make([]influxdb.Point, FLUSH_MAX_POINTS)
}
