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
	"google.golang.org/grpc/metadata"
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
	globalData   map[string]interface{}
	isLocal      bool
}

func Initialise(
	serverUrl string,
	flushTimeout time.Duration,
	globalData map[string]interface{},
) error {
	var tracerClient tracer.TracerClient
	var isLocal bool

	if len(serverUrl) != 0 {
		tracerConn, err := grpc.NewClient(
			serverUrl,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			fmt.Println("Error connecting to tracer server", err)
			return err
		}
		tracerClient = tracer.NewTracerClient(tracerConn)
	}
	if len(serverUrl) == 0 {
		isLocal = true
	}

	library = &lib{
		isLocal:      isLocal,
		tracerClient: tracerClient,
		events:       make([]event, 0, 100),
		logs:         make([]log, 0, 100),
		logChan:      make(chan log, 1000),
		eventChan:    make(chan event, 1000),
		closeChan:    make(chan struct{}),
		flushTimeout: flushTimeout,
		globalData:   globalData,
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
	if l.isLocal {
		return
	}
	fmt.Println("Sending logs to server")
	wg := sync.WaitGroup{}
	wg.Add(2)

	go func(group *sync.WaitGroup) {
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
			// fmt.Println("Log received")
			if l.isLocal {
				fmt.Println(log.Log)
				break
			}
			l.logs = append(l.logs, log)
			if len(l.logs) >= 100 {
				if !timer.Stop() {
					<-timer.C
				}
				l.sendLogs()
				timer = time.NewTimer(l.flushTimeout)
			}
		case event := <-l.eventChan:
			if l.isLocal {
				break
			}
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
			if l.isLocal {
				break
			}
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
		if library.globalData != nil {
			for key, value := range library.globalData {
				metaData[key] = value
			}
		}
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
		if library.globalData != nil {
			for key, value := range library.globalData {
				metaData[key] = value
			}
		}
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
		if library.globalData != nil {
			for key, value := range library.globalData {
				metaData[key] = value
			}
		}
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
		if library.globalData != nil {
			for key, value := range library.globalData {
				metaData[key] = value
			}
		}
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

func GetTracerContextFromGrpcContext(ctx context.Context) context.Context {
	md, _ := metadata.FromIncomingContext(ctx)
	values := md.Get("tracer_id")
	var tracerId string
	if len(values) == 1 {
		tracerId = values[0]
	}

	newCtx := context.WithValue(ctx, "tracer_id", tracerId)
	return newCtx
}

func GetGprcContextFromContext(ctx context.Context) context.Context {
	tracerId, _ := ctx.Value("tracer_id").(string)
	mdCtx := metadata.NewOutgoingContext(
		ctx,
		metadata.Pairs("tracer_id", tracerId),
	)
	return mdCtx
}
