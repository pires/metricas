package service

import (
	"time"

	influxdb "github.com/influxdb/influxdb/client"
	"github.com/nats-io/nats"
	"github.com/nats-io/nats/encoders/protobuf"

	"github.com/pires/metricas/api"
	"github.com/pires/metricas/timeseries"
)

const (
	SUBJECT = "metrics"
)

type Configuration struct {
	AddrNats         string // host:port
	TimeSeriesConfig *timeseries.Configuration
}

type metricsService struct {
	config     *Configuration
	metricChan chan *api.Metric
	quit       chan struct{}
}

func NewMetricsService(config *Configuration) (chan struct{}, error) {
	svc := &metricsService{
		metricChan: make(chan *api.Metric),
		quit:       make(chan struct{}, 1),
	}

	ts, err := timeseries.NewTimeSeries(config.TimeSeriesConfig)

	// start NATS client
	nc, err := nats.Connect("nats://" + config.AddrNats)
	if err != nil {
		return nil, err
	}
	ec, err := nats.NewEncodedConn(nc, protobuf.PROTOBUF_ENCODER)
	if err != nil {
		return nil, err
	}

	// set-up nats
	go func(ec *nats.EncodedConn, ts timeseries.TimeSeries) {
		defer ec.Close()
		defer close(svc.metricChan)
		ec.BindRecvChan(SUBJECT, svc.metricChan)
		for {
			select {
			case <-svc.quit:
				close(ts.Stop())
				return
			case metric := <-svc.metricChan:
				point := &influxdb.Point{
					Measurement: metric.Name,
					Tags:        metric.Tags,
					Time:        transformTime(metric.Timestamp),
					Fields:      transformFields(metric.Values),
				}
				ts.Points() <- point
			}
		}

	}(ec, ts)

	return svc.quit, nil
}

func transformTime(t *api.Timestamp) time.Time {
	return time.Unix(t.Seconds, int64(t.Nanos))
}

func transformFields(values map[string]int64) map[string]interface{} {
	fields := make(map[string]interface{}, len(values))
	for k, v := range values {
		fields[k] = v
	}
	return fields
}
