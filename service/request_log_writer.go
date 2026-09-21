package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	requestLogQueueCapacity  = 4096
	requestLogWorkerCount    = 5
	requestLogBatchSize      = 5
	requestLogFlushInterval  = time.Second
	requestLogMaxQueuedBytes = int64(256 << 20)
)

type requestLogWriterState struct {
	startOnce sync.Once
	stopOnce  sync.Once
	mu        sync.RWMutex
	queue     chan *model.RequestLog
	accepting bool
	workers   sync.WaitGroup
	config    requestLogWriterConfig

	queuedBytes atomic.Int64
	dropped     atomic.Int64
	writeFailed atomic.Int64
}

type requestLogWriterConfig struct {
	queueCapacity  int
	workerCount    int
	batchSize      int
	flushInterval  time.Duration
	maxQueuedBytes int64
	insert         func(context.Context, []*model.RequestLog) error
}

var requestLogWriter = newRequestLogWriter(requestLogWriterConfig{
	queueCapacity:  requestLogQueueCapacity,
	workerCount:    requestLogWorkerCount,
	batchSize:      requestLogBatchSize,
	flushInterval:  requestLogFlushInterval,
	maxQueuedBytes: requestLogMaxQueuedBytes,
	insert:         model.CreateRequestLogs,
})

func newRequestLogWriter(config requestLogWriterConfig) *requestLogWriterState {
	return &requestLogWriterState{config: config}
}

func StartRequestLogWriter() {
	requestLogWriter.start()
}

func (writer *requestLogWriterState) start() {
	writer.startOnce.Do(func() {
		writer.mu.Lock()
		writer.queue = make(chan *model.RequestLog, writer.config.queueCapacity)
		writer.accepting = true
		writer.mu.Unlock()

		for range writer.config.workerCount {
			writer.workers.Go(writer.runWorker)
		}
	})
}

func EnqueueRequestLog(log *model.RequestLog) bool {
	if log == nil || !common.RequestLogEnabled {
		return false
	}
	return requestLogWriter.enqueue(log)
}

func (writer *requestLogWriterState) enqueue(log *model.RequestLog) bool {
	if log == nil {
		return false
	}
	if log.PayloadBytes < 0 {
		log.PayloadBytes = 0
	}
	if !writer.reserveBytes(log.PayloadBytes) {
		writer.dropped.Add(1)
		common.SysLog(fmt.Sprintf("request log dropped: request_id=%s size=%d reason=queue_bytes_limit", log.RequestId, log.PayloadBytes))
		return false
	}

	writer.mu.RLock()
	defer writer.mu.RUnlock()
	if !writer.accepting || writer.queue == nil {
		writer.queuedBytes.Add(-log.PayloadBytes)
		writer.dropped.Add(1)
		common.SysLog(fmt.Sprintf("request log dropped: request_id=%s size=%d reason=writer_stopped", log.RequestId, log.PayloadBytes))
		return false
	}
	select {
	case writer.queue <- log:
		return true
	default:
		writer.queuedBytes.Add(-log.PayloadBytes)
		writer.dropped.Add(1)
		common.SysLog(fmt.Sprintf("request log dropped: request_id=%s size=%d reason=queue_full", log.RequestId, log.PayloadBytes))
		return false
	}
}

func ShutdownRequestLogWriter(ctx context.Context) error {
	return requestLogWriter.shutdown(ctx)
}

func (writer *requestLogWriterState) shutdown(ctx context.Context) error {
	writer.stopOnce.Do(func() {
		writer.mu.Lock()
		writer.accepting = false
		if writer.queue != nil {
			close(writer.queue)
		}
		writer.mu.Unlock()
	})

	done := make(chan struct{})
	go func() {
		writer.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (writer *requestLogWriterState) reserveBytes(size int64) bool {
	for {
		current := writer.queuedBytes.Load()
		if current+size > writer.config.maxQueuedBytes {
			return false
		}
		if writer.queuedBytes.CompareAndSwap(current, current+size) {
			return true
		}
	}
}

func (writer *requestLogWriterState) runWorker() {
	batch := make([]*model.RequestLog, 0, writer.config.batchSize)
	timer := time.NewTimer(writer.config.flushInterval)
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := writer.config.insert(ctx, batch)
		cancel()
		if err != nil {
			writer.writeFailed.Add(int64(len(batch)))
			requestIds := make([]string, 0, len(batch))
			for _, item := range batch {
				requestIds = append(requestIds, item.RequestId)
			}
			common.SysLog(fmt.Sprintf("request log batch write failed: count=%d request_ids=%v error=%v", len(batch), requestIds, err))
		}
		for _, item := range batch {
			writer.queuedBytes.Add(-item.PayloadBytes)
		}
		batch = batch[:0]
	}

	for {
		select {
		case item, ok := <-writer.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, item)
			if len(batch) >= writer.config.batchSize {
				flush()
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(writer.config.flushInterval)
			}
		case <-timer.C:
			flush()
			timer.Reset(writer.config.flushInterval)
		}
	}
}
