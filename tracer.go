package tracerlib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/harshk1999/tracer_lib/internal/client/tracer"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var library *lib

type event struct {
	CreatedAt time.Time
	Id        string
	Metadata  []byte
}

type log struct {
	EventId   string
	Metadata  []byte
	Log       string
	CreatedAt time.Time
}

type lib struct {
	tracerClient tracer.TracerClient
	logs         []log
	events       []event
	logChan      chan log
	eventChan    chan event
	closeChan    chan struct{}
	flushTimeout time.Duration
}

func Initialise(serverUrl string, flushTimeout time.Duration) error {
	tracerConn, err := grpc.NewClient(
		serverUrl,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		fmt.Println("Error connecting to tracer server", err)
		return err
	}

	tracerClient := tracer.NewTracerClient(tracerConn)
	library = &lib{
		tracerClient: tracerClient,
		events:       make([]event, 0, 100),
		logs:         make([]log, 0, 100),
		logChan:      make(chan log, 1000),
		eventChan:    make(chan event, 1000),
		closeChan:    make(chan struct{}),
		flushTimeout: flushTimeout,
	}

	go library.listenForLogs()

	return nil
}

func Shutdown() error {
	if library == nil {
		return errors.New("Tracer not initialised")
	}
	library.closeChan <- struct{}{}
	return nil
}

func (l *lib) sendLogs() {
	fmt.Println("Sending logs to server")
	wg := sync.WaitGroup{}
	go func(group *sync.WaitGroup) {
		group.Add(1)
		defer group.Done()
		logs := []*tracer.Log{}
		for _, v := range l.logs {
			timestamp := timestamppb.New(v.CreatedAt)
			logs = append(logs, &tracer.Log{
				EventId:   v.EventId,
				Log:       v.Log,
				CreatedAt: timestamp,
				Metadata:  v.Metadata,
			})
		}
		_, err := l.tracerClient.InsertLog(context.Background(), &tracer.Logs{
			Logs: logs,
		})

		if err != nil {
			fmt.Println("Error sending logs to tracer server", err)
		}
		l.logs = make([]log, 0, 100)

	}(&wg)

	go func(group *sync.WaitGroup) {
		group.Add(1)
		defer group.Done()
		events := []*tracer.Event{}
		for _, v := range l.events {
			timestamp := timestamppb.New(v.CreatedAt)
			events = append(events, &tracer.Event{
				EventId:   v.Id,
				CreatedAt: timestamp,
				Metadata:  v.Metadata,
			})
		}
		_, err := l.tracerClient.InsertEvent(context.Background(), &tracer.Events{
			Events: events,
		})

		if err != nil {
			fmt.Println("Error sending events to tracer server", err)
		}

		l.events = make([]event, 0, 100)

	}(&wg)

	wg.Wait()
}

func (l *lib) listenForLogs() {
	timer := time.NewTimer(l.flushTimeout)
	for {
		select {
		case log := <-l.logChan:
			fmt.Println("Log received")
			l.logs = append(l.logs, log)
			if len(l.logs) >= 100 {
				if !timer.Stop() {
					<-timer.C
				}
				l.sendLogs()
				timer = time.NewTimer(l.flushTimeout)
			}
		case event := <-l.eventChan:
			fmt.Println("Event received")
			l.events = append(l.events, event)
			if len(l.events) >= 100 {
				if !timer.Stop() {
					<-timer.C
				}
				l.sendLogs()
				timer = time.NewTimer(l.flushTimeout)
			}
		case <-timer.C:
			fmt.Println("Timer timedout")
			l.sendLogs()
			timer = time.NewTimer(l.flushTimeout)
		case <-l.closeChan:
			fmt.Println("shutdown")
			l.sendLogs()
		}
	}
}

func initialisationCheck() {
	if library == nil {
		panic("Tracer not initilised")
	}
}

func CreateEvent(metaData map[string]interface{}) string {
	initialisationCheck()
	tm := time.Now()
	id := uuid.New()
	idStr := id.String()
	_, file, line, ok := runtime.Caller(1)
	if ok {
		if metaData == nil {
			metaData = make(map[string]interface{})
		}
		metaData["line"] = line
		metaData["file"] = file
	} else {
		panic("Could not get caller info")
	}

	bytes, err := json.Marshal(metaData)
	if err != nil {
		panic("Error marshalling metadata")
	}

	event := event{
		Id:        idStr,
		CreatedAt: tm,
		Metadata:  bytes,
	}

	library.eventChan <- event

	return idStr
}

func LogInfo(ctx context.Context, metaData map[string]interface{}, logs ...any) string {
	initialisationCheck()
	tracerId, _ := ctx.Value("tracer_id").(string)
	tm := time.Now()
	id := uuid.New()
	idStr := id.String()
	_, file, line, ok := runtime.Caller(1)
	if ok {
		if metaData == nil {
			metaData = make(map[string]interface{})
		}
		metaData["line"] = line
		metaData["file"] = file
		metaData["level"] = "info"
	} else {
		panic("Could not get caller info")
	}

	bytes, err := json.Marshal(metaData)
	if err != nil {
		panic("Error marshalling metadata")
	}

	log := log{
		EventId:   tracerId,
		Log:       fmt.Sprint(logs...),
		CreatedAt: tm,
		Metadata:  bytes,
	}

	library.logChan <- log

	return idStr
}

func LogError(ctx context.Context, metaData map[string]interface{}, logs ...any) string {
	initialisationCheck()
	tracerId, _ := ctx.Value("tracer_id").(string)
	tm := time.Now()
	id := uuid.New()
	idStr := id.String()
	_, file, line, ok := runtime.Caller(1)
	if ok {
		if metaData == nil {
			metaData = make(map[string]interface{})
		}
		metaData["line"] = line
		metaData["file"] = file
		metaData["level"] = "error"
	} else {
		panic("Could not get caller info")
	}

	bytes, err := json.Marshal(metaData)
	if err != nil {
		panic("Error marshalling metadata")
	}

	log := log{
		EventId:   tracerId,
		Log:       fmt.Sprint(logs...),
		CreatedAt: tm,
		Metadata:  bytes,
	}

	library.logChan <- log

	return idStr
}

func LogWarn(ctx context.Context, metaData map[string]interface{}, logs ...any) string {
	initialisationCheck()
	tracerId, _ := ctx.Value("tracer_id").(string)
	tm := time.Now()
	id := uuid.New()
	idStr := id.String()
	_, file, line, ok := runtime.Caller(1)
	if ok {
		if metaData == nil {
			metaData = make(map[string]interface{})
		}
		metaData["line"] = line
		metaData["file"] = file
		metaData["level"] = "warn"
	} else {
		panic("Could not get caller info")
	}

	bytes, err := json.Marshal(metaData)
	if err != nil {
		panic("Error marshalling metadata")
	}

	log := log{
		EventId:   tracerId,
		Log:       fmt.Sprint(logs...),
		CreatedAt: tm,
		Metadata:  bytes,
	}

	library.logChan <- log

	return idStr
}
