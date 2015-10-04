package test

import (
	"bytes"
	"math"
	"regexp"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nats-io/nats"
)

func TestCloseLeakingGoRoutines(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()

	base := runtime.NumGoroutine()

	nc := NewDefaultConnection(t)
	time.Sleep(10 * time.Millisecond)
	nc.Close()
	time.Sleep(10 * time.Millisecond)
	delta := (runtime.NumGoroutine() - base)
	if delta > 0 {
		t.Fatalf("%d Go routines still exist post Close()", delta)
	}
	// Make sure we can call Close() multiple times
	nc.Close()
}

func TestConnectedServer(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()

	nc := NewDefaultConnection(t)

	u := nc.ConnectedUrl()
	if u == "" || u != nats.DefaultURL {
		t.Fatalf("Unexpected connected URL of %s\n", u)
	}
	srv := nc.ConnectedServerId()
	if srv == "" {
		t.Fatal("Expeced a connected server id")
	}
	nc.Close()
	u = nc.ConnectedUrl()
	if u != "" {
		t.Fatalf("Expected a nil connected URL, got %s\n", u)
	}
	srv = nc.ConnectedServerId()
	if srv != "" {
		t.Fatalf("Expected a nil connect server, got %s\n", srv)
	}
}

func TestMultipleClose(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			nc.Close()
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestBadOptionTimeoutConnect(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()

	opts := nats.DefaultOptions
	opts.Timeout = -1
	opts.Url = "nats://localhost:4222"

	_, err := opts.Connect()
	if err == nil {
		t.Fatal("Expected an error")
	}
	if err != nats.ErrNoServers {
		t.Fatalf("Expected a ErrNoServers error: Got %v\n", err)
	}
}

func TestSimplePublish(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	if err := nc.Publish("foo", []byte("Hello World")); err != nil {
		t.Fatal("Failed to publish string message: ", err)
	}
}

func TestSimplePublishNoData(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	if err := nc.Publish("foo", nil); err != nil {
		t.Fatal("Failed to publish empty message: ", err)
	}
}

func TestAsyncSubscribe(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	omsg := []byte("Hello World")
	ch := make(chan bool)

	_, err := nc.Subscribe("foo", func(m *nats.Msg) {
		if !bytes.Equal(m.Data, omsg) {
			t.Fatal("Message received does not match")
		}
		if m.Sub == nil {
			t.Fatal("Callback does not have a valid Subscription")
		}
		ch <- true
	})
	if err != nil {
		t.Fatal("Failed to subscribe: ", err)
	}
	nc.Publish("foo", omsg)
	if e := Wait(ch); e != nil {
		t.Fatal("Message not received for subscription")
	}
}

func TestSyncSubscribe(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	sub, err := nc.SubscribeSync("foo")
	if err != nil {
		t.Fatal("Failed to subscribe: ", err)
	}
	omsg := []byte("Hello World")
	nc.Publish("foo", omsg)
	msg, err := sub.NextMsg(1 * time.Second)
	if err != nil || !bytes.Equal(msg.Data, omsg) {
		t.Fatal("Message received does not match")
	}
}

func TestPubSubWithReply(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	sub, err := nc.SubscribeSync("foo")
	if err != nil {
		t.Fatal("Failed to subscribe: ", err)
	}
	omsg := []byte("Hello World")
	nc.PublishMsg(&nats.Msg{Subject: "foo", Reply: "bar", Data: omsg})
	msg, err := sub.NextMsg(10 * time.Second)
	if err != nil || !bytes.Equal(msg.Data, omsg) {
		t.Fatal("Message received does not match")
	}
}

func TestFlush(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	omsg := []byte("Hello World")
	for i := 0; i < 10000; i++ {
		nc.Publish("flush", omsg)
	}
	if err := nc.Flush(); err != nil {
		t.Fatalf("Received error from flush: %s\n", err)
	}
	if nb, _ := nc.Buffered(); nb > 0 {
		t.Fatalf("Outbound buffer not empty: %d bytes\n", nb)
	}
}

func TestQueueSubscriber(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	s1, _ := nc.QueueSubscribeSync("foo", "bar")
	s2, _ := nc.QueueSubscribeSync("foo", "bar")
	omsg := []byte("Hello World")
	nc.Publish("foo", omsg)
	nc.Flush()
	r1, _ := s1.QueuedMsgs()
	r2, _ := s2.QueuedMsgs()
	if (r1 + r2) != 1 {
		t.Fatal("Received too many messages for multiple queue subscribers")
	}
	// Drain messages
	s1.NextMsg(0)
	s2.NextMsg(0)

	total := 1000
	for i := 0; i < total; i++ {
		nc.Publish("foo", omsg)
	}
	nc.Flush()
	v := uint(float32(total) * 0.15)
	r1, _ = s1.QueuedMsgs()
	r2, _ = s2.QueuedMsgs()
	if r1+r2 != total {
		t.Fatalf("Incorrect number of messages: %d vs %d", (r1 + r2), total)
	}
	expected := total / 2
	d1 := uint(math.Abs(float64(expected - r1)))
	d2 := uint(math.Abs(float64(expected - r2)))
	if d1 > v || d2 > v {
		t.Fatalf("Too much variance in totals: %d, %d > %d", d1, d2, v)
	}
}

func TestReplyArg(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	ch := make(chan bool)
	replyExpected := "bar"

	nc.Subscribe("foo", func(m *nats.Msg) {
		if m.Reply != replyExpected {
			t.Fatalf("Did not receive correct reply arg in callback: "+
				"('%s' vs '%s')", m.Reply, replyExpected)
		}
		ch <- true
	})
	nc.PublishMsg(&nats.Msg{Subject: "foo", Reply: replyExpected, Data: []byte("Hello")})
	if e := Wait(ch); e != nil {
		t.Fatal("Did not receive callback")
	}
}

func TestSyncReplyArg(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	replyExpected := "bar"
	sub, _ := nc.SubscribeSync("foo")
	nc.PublishMsg(&nats.Msg{Subject: "foo", Reply: replyExpected, Data: []byte("Hello")})
	msg, err := sub.NextMsg(1 * time.Second)
	if err != nil {
		t.Fatal("Received an err on NextMsg()")
	}
	if msg.Reply != replyExpected {
		t.Fatalf("Did not receive correct reply arg in callback: "+
			"('%s' vs '%s')", msg.Reply, replyExpected)
	}
}

func TestUnsubscribe(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	received := int32(0)
	max := int32(10)
	ch := make(chan bool)
	nc.Subscribe("foo", func(m *nats.Msg) {
		atomic.AddInt32(&received, 1)
		if received == max {
			err := m.Sub.Unsubscribe()
			if err != nil {
				t.Fatal("Unsubscribe failed with err:", err)
			}
			ch <- true
		}
	})
	send := 20
	for i := 0; i < send; i++ {
		nc.Publish("foo", []byte("hello"))
	}
	nc.Flush()
	<-ch

	r := atomic.LoadInt32(&received)
	if r != max {
		t.Fatalf("Received wrong # of messages after unsubscribe: %d vs %d",
			r, max)
	}
}

func TestDoubleUnsubscribe(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	sub, err := nc.SubscribeSync("foo")
	if err != nil {
		t.Fatal("Failed to subscribe: ", err)
	}
	if err = sub.Unsubscribe(); err != nil {
		t.Fatal("Unsubscribe failed with err:", err)
	}
	if err = sub.Unsubscribe(); err == nil {
		t.Fatal("Unsubscribe should have reported an error")
	}
}

func TestRequestTimeout(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	if _, err := nc.Request("foo", []byte("help"), 10*time.Millisecond); err == nil {
		t.Fatalf("Expected to receive a timeout error")
	}
}

func TestRequest(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	response := []byte("I will help you")
	nc.Subscribe("foo", func(m *nats.Msg) {
		nc.Publish(m.Reply, response)
	})
	msg, err := nc.Request("foo", []byte("help"), 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Received an error on Request test: %s", err)
	}
	if !bytes.Equal(msg.Data, response) {
		t.Fatalf("Received invalid response")
	}
}

func TestRequestNoBody(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	response := []byte("I will help you")
	nc.Subscribe("foo", func(m *nats.Msg) {
		nc.Publish(m.Reply, response)
	})
	msg, err := nc.Request("foo", nil, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("Received an error on Request test: %s", err)
	}
	if !bytes.Equal(msg.Data, response) {
		t.Fatalf("Received invalid response")
	}
}

func TestFlushInCB(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	ch := make(chan bool)

	nc.Subscribe("foo", func(_ *nats.Msg) {
		nc.Flush()
		ch <- true
	})
	nc.Publish("foo", []byte("Hello"))
	if e := Wait(ch); e != nil {
		t.Fatal("Flush did not return properly in callback")
	}
}

func TestReleaseFlush(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)

	for i := 0; i < 1000; i++ {
		nc.Publish("foo", []byte("Hello"))
	}
	go nc.Close()
	nc.Flush()
}

func TestInbox(t *testing.T) {
	inbox := nats.NewInbox()
	if matched, _ := regexp.Match(`_INBOX.\S`, []byte(inbox)); !matched {
		t.Fatal("Bad INBOX format")
	}
}

func TestStats(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	data := []byte("The quick brown fox jumped over the lazy dog")
	iter := 10

	for i := 0; i < iter; i++ {
		nc.Publish("foo", data)
	}

	if nc.OutMsgs != uint64(iter) {
		t.Fatalf("Not properly tracking OutMsgs: received %d, wanted %d\n", nc.OutMsgs, iter)
	}
	obb := uint64(iter * len(data))
	if nc.OutBytes != obb {
		t.Fatalf("Not properly tracking OutBytes: received %d, wanted %d\n", nc.OutBytes, obb)
	}

	// Clear outbound
	nc.OutMsgs, nc.OutBytes = 0, 0

	// Test both sync and async versions of subscribe.
	nc.Subscribe("foo", func(_ *nats.Msg) {})
	nc.SubscribeSync("foo")

	for i := 0; i < iter; i++ {
		nc.Publish("foo", data)
	}
	nc.Flush()

	if nc.InMsgs != uint64(2*iter) {
		t.Fatalf("Not properly tracking InMsgs: received %d, wanted %d\n", nc.InMsgs, 2*iter)
	}

	ibb := 2 * obb
	if nc.InBytes != ibb {
		t.Fatalf("Not properly tracking InBytes: received %d, wanted %d\n", nc.InBytes, ibb)
	}
}

func TestRaceSafeStats(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	go nc.Publish("foo", []byte("Hello World"))
	time.Sleep(200 * time.Millisecond)

	stats := nc.Stats()

	if stats.OutMsgs != uint64(1) {
		t.Fatalf("Not properly tracking OutMsgs: received %d, wanted %d\n", nc.OutMsgs, 1)
	}
}

func TestBadSubject(t *testing.T) {
	s := RunDefaultServer()
	defer s.Shutdown()
	nc := NewDefaultConnection(t)
	defer nc.Close()

	err := nc.Publish("", []byte("Hello World"))
	if err == nil {
		t.Fatalf("Expected an error on bad subject to publish")
	}
	if err != nats.ErrBadSubject {
		t.Fatalf("Expected a ErrBadSubject error: Got %v\n", err)
	}
}
