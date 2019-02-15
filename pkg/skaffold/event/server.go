/*
Copyright 2019 The Skaffold Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package event

import (
	"context"
	"fmt"
	"net"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/event/proto"

	"github.com/golang/protobuf/ptypes"
	empty "github.com/golang/protobuf/ptypes/empty"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"google.golang.org/grpc"
)

type server struct{}

func (s *server) GetState(context.Context, *empty.Empty) (*proto.State, error) {
	return ev.state, nil
}

func (s *server) EventLog(stream proto.SkaffoldService_EventLogServer) error {
	for _, entry := range ev.eventLog {
		if err := stream.Send(&entry); err != nil {
			return err
		}
	}
	c := make(chan proto.LogEntry)
	ev.RegisterListener(c)
	var entry proto.LogEntry
	for {
		entry = <-c
		if err := stream.Send(&entry); err != nil {
			return err
		}
	}
}

func (s *server) Handle(ctx context.Context, event *proto.Event) (*empty.Empty, error) {
	var entry string
	if event.EventType == Build {
		ev.eventHandler.state.BuildState.Artifacts[event.Artifact] = event.Status
		switch event.Status {
		case InProgress:
			entry = fmt.Sprintf("Build started for artifact %s", event.Artifact)
		case Complete:
			entry = fmt.Sprintf("Build completed for artifact %s", event.Artifact)
		case Failed:
			entry = fmt.Sprintf("Build failed for artifact %s", event.Artifact)
		default:
		}
	}
	if event.EventType == Deploy {
		ev.eventHandler.state.DeployState.Status = event.Status
		switch event.Status {
		case InProgress:
			entry = "Deploy started"
		case Complete:
			entry = "Deploy complete"
		case Failed:
			entry = "Deploy failed"
		default:
		}
	}
	if event.EventType == Port {
		ev.eventHandler.state.PortState.ForwardedPorts = append(ev.eventHandler.state.PortState.ForwardedPorts, event.PortInfo)
		// ev.eventHandler.state.ForwardedPorts = append(ev.eventHandler.state.ForwardedPorts, event.PortInfo)
	}

	// var errStr string
	// if event.Err != nil {
	// 	errStr = event.Err.Error()
	// }
	ev.logEvent(proto.LogEntry{
		Timestamp: ptypes.TimestampNow(),
		Type:      event.EventType,
		Entry:     entry,
		Error:     event.Err,
	})
	return &empty.Empty{}, nil
}

// newStatusServer creates the grpc server for serving the state and event log.
func newStatusServer(port string) (func(), error) {
	if port == "" {
		return func() {}, nil
	}
	l, err := net.Listen("tcp", port)
	if err != nil {
		return func() {}, errors.Wrap(err, "creating listener")
	}

	s := grpc.NewServer()
	proto.RegisterSkaffoldServiceServer(s, &server{})

	go func() {
		if err := s.Serve(l); err != nil {
			logrus.Errorf("failed to start grpc server: %s", err)
		}
	}()
	return func() {
		s.Stop()
		l.Close()
	}, nil
}
