package dq

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestConsume(t *testing.T) {
	t.SkipNow()

	q := New(append(testOpts(t), WithName(""))...)

	q.Consume(HandlerFunc(func(ctx context.Context, m *Message) error {
		t.Log("consume:", m.ID)
		return nil
	}))

	<-time.Tick(10 * time.Second)
}

func TestConsumeRealtime(t *testing.T) {
	// init
	q := New(testOpts(t)...)
	defer t.Cleanup(func() { cleanup(t, q) })

	// produce
	num := 5
	var sendIDs []string
	for i := 0; i < num; i++ {
		id, err := q.Produce(context.Background(), &ProducerMessage{
			Payload: []byte("ready_" + strconv.Itoa(i)),
		})
		assert.Nil(t, err)
		sendIDs = append(sendIDs, id)
	}

	// consume
	var wg sync.WaitGroup
	wg.Add(num)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	q.Consume(HandlerFunc(func(ctx context.Context, m *Message) error {
		// t.Log("consume:", string(m.Payload))
		wg.Done()
		return nil
	}))

	select {
	case <-time.After(1 * time.Second):
		t.Fatal("consume timeout")
	case <-done:
	}
}

func TestConsumeDelay(t *testing.T) {
	// init
	q := New(testOpts(t)...)
	defer t.Cleanup(func() { cleanup(t, q) })

	// produce
	num := 5
	var sendIDs []string
	for i := 0; i < num; i++ {
		at := time.Now().Add(100 * time.Millisecond)
		id, err := q.Produce(context.Background(), &ProducerMessage{
			Payload:   []byte("delay_" + strconv.Itoa(i)),
			DeliverAt: &at,
		})
		assert.Nil(t, err)
		sendIDs = append(sendIDs, id)
	}

	// consume
	var wg sync.WaitGroup
	wg.Add(num)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	q.Consume(HandlerFunc(func(ctx context.Context, m *Message) error {
		// t.Log("consume:", string(m.Payload))
		wg.Done()
		return nil
	}))

	select {
	case <-time.After(1 * time.Second):
		t.Fatal("consume timeout")
	case <-done:
	}
}

func TestConsumeErrRetry(t *testing.T) {
	// init
	retry := 3
	q := New(append(testOpts(t),
		WithRetryTimes(retry),
		WithRetryInterval(10*time.Millisecond),
	)...)
	defer t.Cleanup(func() { cleanup(t, q) })

	// produce
	num := 10
	var sendIDs []string
	for i := 0; i < num; i++ {
		id, err := q.Produce(context.Background(), &ProducerMessage{
			Payload: []byte("ready_" + strconv.Itoa(i)),
		})
		assert.Nil(t, err)
		sendIDs = append(sendIDs, id)
	}

	// consume
	var wg sync.WaitGroup
	wg.Add(num * (retry + 1))

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	q.Consume(HandlerFunc(func(ctx context.Context, m *Message) error {
		t.Log("consume:", m.DeliverCnt, string(m.Payload))
		wg.Done()
		return fmt.Errorf("mock err")
	}))

	select {
	case <-time.After(2 * time.Second):
		t.Fatal("consume timeout")
	case <-done:
	}
}

func TestConsumeRedeliver(t *testing.T) {
	// init
	retry := 3
	q := New(append(testOpts(t),
		WithRetryTimes(retry),
		WithRetryInterval(1000*time.Millisecond),
	)...)
	defer t.Cleanup(func() { cleanup(t, q) })

	// produce
	num := 10
	var sendIDs []string
	for i := 0; i < num; i++ {
		id, err := q.Produce(context.Background(), &ProducerMessage{
			Payload: []byte("ready_" + strconv.Itoa(i)),
		})
		assert.Nil(t, err)
		sendIDs = append(sendIDs, id)
	}

	// consume
	var wg sync.WaitGroup
	wg.Add(num * (retry + 1))

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	q.Consume(HandlerFunc(func(ctx context.Context, m *Message) error {
		t.Log("consume:", m.DeliverCnt, string(m.Payload))
		err := q.RedeliveryAfter(ctx, m.ID, 100*time.Millisecond)
		fmt.Println("redelivery:", err)
		wg.Done()
		return fmt.Errorf("mock err")
	}))

	select {
	case <-time.After(1000 * time.Millisecond):
		t.Fatal("consume timeout")
	case <-done:
	}
}

func TestConsumePanicRetry(t *testing.T) {
	// init
	retry := 3
	q := New(append(testOpts(t),
		WithRetryTimes(retry),
		WithRetryInterval(10*time.Millisecond),
	)...)
	defer t.Cleanup(func() { cleanup(t, q) })

	// produce
	num := 5
	var sendIDs []string
	for i := 0; i < num; i++ {
		id, err := q.Produce(context.Background(), &ProducerMessage{
			Payload: []byte("ready_" + strconv.Itoa(i)),
		})
		assert.Nil(t, err)
		sendIDs = append(sendIDs, id)
	}

	// consume
	var wg sync.WaitGroup
	wg.Add(num * (retry + 1))

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	q.Consume(HandlerFunc(func(ctx context.Context, m *Message) error {
		t.Log("consume:", m.DeliverCnt, string(m.Payload))
		wg.Done()
		panic("mock panic")
		return nil
	}))

	select {
	case <-time.After(200 * time.Second):
		t.Fatal("consume timeout")
	case <-done:
	}
}

func TestGracefulShutdown(t *testing.T) {
	// init
	q := New(append(testOpts(t),
		WithRetryInterval(10*time.Millisecond),
		WithConsumerWorkerInterval(10*time.Millisecond),
		WithDaemonWorkerInterval(10*time.Millisecond),
		WithLogMode(Trace),
	)...)
	defer t.Cleanup(func() { cleanup(t, q) })

	ctx := context.Background()

	// consume
	q.Consume(HandlerFunc(func(ctx context.Context, m *Message) error {
		t.Log("consumer begin process:", m.ID)
		<-time.After(100 * time.Millisecond)
		t.Log("consumer end process:", m.ID)
		return nil
	}))

	// produce
	id, err := q.Produce(ctx, &ProducerMessage{Payload: []byte("payload")})
	assert.Nil(t, err)
	t.Log("produce:", id)

	// graceful shutdown success
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	assert.Nil(t, q.Close(ctx))
}

func TestGracefulShutdownWithError(t *testing.T) {
	// init
	q := New(append(testOpts(t),
		WithRetryInterval(10*time.Millisecond),
		WithConsumerWorkerInterval(10*time.Millisecond),
		WithDaemonWorkerInterval(10*time.Millisecond),
		WithLogMode(Trace),
	)...)
	defer t.Cleanup(func() { cleanup(t, q) })

	ctx := context.Background()

	// consume
	q.Consume(HandlerFunc(func(ctx context.Context, m *Message) error {
		t.Log("consumer begin process:", m.ID)
		<-time.After(1000 * time.Millisecond)
		t.Log("consumer end process:", m.ID)
		return nil
	}))

	// produce
	id, err := q.Produce(ctx, &ProducerMessage{Payload: []byte("payload")})
	assert.Nil(t, err)
	t.Log("produce:", id)

	<-time.After(10 * time.Millisecond)

	// graceful shutdown timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.ErrorIs(t, q.Close(ctx), context.DeadlineExceeded)
}

func TestConsumeLimit(t *testing.T) {
	// init
	interval := 20 * time.Millisecond
	q := New(append(testOpts(t),
		WithConsumerWorkerNum(2),
		WithConsumerWorkerInterval(10*time.Millisecond),
		WithLimiter(rate.Every(interval), 1),
		WithLogMode(Trace),
	)...)
	defer t.Cleanup(func() { cleanup(t, q) })

	// consume
	var dataCh = make(chan string)
	q.Consume(HandlerFunc(func(ctx context.Context, m *Message) error {
		// t.Log("consume:", m.ID)
		dataCh <- m.ID
		return nil
	}))

	// produce
	num := 5
	var sendIDs []string
	for i := 0; i < num; i++ {
		id, err := q.Produce(context.Background(), &ProducerMessage{
			Payload: []byte("ready_" + strconv.Itoa(i)),
		})
		assert.Nil(t, err)
		sendIDs = append(sendIDs, id)
	}

	go func() {
		lastRecv := time.Now()
		first := true
		for data := range dataCh {
			since := time.Since(lastRecv)
			lastRecv = time.Now()

			t.Log("recv:", data, since)
			if first {
				first = false
				continue
			}

			if math.Abs(float64(interval-since)) > 0.3*float64(interval) {
				t.Errorf("consume interval not match, actual: %v, want: %v", since, interval)
			}
		}
	}()

	<-time.After(interval*time.Duration(num) + 100*time.Millisecond)
}
