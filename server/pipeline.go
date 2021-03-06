// Copyright 2017 The Nakama Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package server

import (
	"database/sql"
	"fmt"

	"nakama/pkg/social"

	"strings"

	"github.com/gogo/protobuf/jsonpb"
	"go.uber.org/zap"
)

type pipeline struct {
	config            Config
	db                *sql.DB
	tracker           Tracker
	matchmaker        Matchmaker
	hmacSecretByte    []byte
	messageRouter     MessageRouter
	sessionRegistry   *SessionRegistry
	socialClient      *social.Client
	runtime           *Runtime
	jsonpbMarshaler   *jsonpb.Marshaler
	jsonpbUnmarshaler *jsonpb.Unmarshaler
}

// NewPipeline creates a new Pipeline
func NewPipeline(config Config, db *sql.DB, tracker Tracker, matchmaker Matchmaker, messageRouter MessageRouter, registry *SessionRegistry, socialClient *social.Client, runtime *Runtime) *pipeline {
	return &pipeline{
		config:          config,
		db:              db,
		tracker:         tracker,
		matchmaker:      matchmaker,
		hmacSecretByte:  []byte(config.GetSession().EncryptionKey),
		messageRouter:   messageRouter,
		sessionRegistry: registry,
		socialClient:    socialClient,
		runtime:         runtime,
		jsonpbMarshaler: &jsonpb.Marshaler{
			EnumsAsInts:  true,
			EmitDefaults: false,
			Indent:       "",
			OrigName:     false,
		},
		jsonpbUnmarshaler: &jsonpb.Unmarshaler{
			AllowUnknownFields: false,
		},
	}
}

func (p *pipeline) processRequest(logger *zap.Logger, session *session, originalEnvelope *Envelope) {
	if originalEnvelope.Payload == nil {
		session.Send(ErrorMessage(originalEnvelope.CollationId, MISSING_PAYLOAD, "No payload found"))
		return
	}

	messageType := fmt.Sprintf("%T", originalEnvelope.Payload)
	logger.Debug("Received message", zap.String("type", messageType))

	messageType = strings.TrimPrefix(messageType, "*server.Envelope_")
	envelope, fnErr := RuntimeBeforeHook(p.runtime, p.jsonpbMarshaler, p.jsonpbUnmarshaler, messageType, originalEnvelope, session)
	if fnErr != nil {
		logger.Error("Runtime before function caused an error", zap.String("message", messageType), zap.Error(fnErr))
		session.Send(ErrorMessage(originalEnvelope.CollationId, RUNTIME_FUNCTION_EXCEPTION, fmt.Sprintf("Runtime before function caused an error: %s", fnErr.Error())))
		return
	}

	switch envelope.Payload.(type) {
	case *Envelope_Logout:
		// TODO Store JWT into a blacklist until remaining JWT expiry.
		p.sessionRegistry.remove(session)
		session.close()

	case *Envelope_Link:
		p.linkID(logger, session, envelope)
	case *Envelope_Unlink:
		p.unlinkID(logger, session, envelope)

	case *Envelope_SelfFetch:
		p.selfFetch(logger, session, envelope)
	case *Envelope_SelfUpdate:
		p.selfUpdate(logger, session, envelope)
	case *Envelope_UsersFetch:
		p.usersFetch(logger, session, envelope)

	case *Envelope_FriendAdd:
		p.friendAdd(logger, session, envelope)
	case *Envelope_FriendRemove:
		p.friendRemove(logger, session, envelope)
	case *Envelope_FriendBlock:
		p.friendBlock(logger, session, envelope)
	case *Envelope_FriendsList:
		p.friendsList(logger, session, envelope)

	case *Envelope_GroupCreate:
		p.groupCreate(logger, session, envelope)
	case *Envelope_GroupUpdate:
		p.groupUpdate(logger, session, envelope)
	case *Envelope_GroupRemove:
		p.groupRemove(logger, session, envelope)
	case *Envelope_GroupsFetch:
		p.groupsFetch(logger, session, envelope)
	case *Envelope_GroupsList:
		p.groupsList(logger, session, envelope)
	case *Envelope_GroupsSelfList:
		p.groupsSelfList(logger, session, envelope)
	case *Envelope_GroupUsersList:
		p.groupUsersList(logger, session, envelope)
	case *Envelope_GroupJoin:
		p.groupJoin(logger, session, envelope)
	case *Envelope_GroupLeave:
		p.groupLeave(logger, session, envelope)
	case *Envelope_GroupUserAdd:
		p.groupUserAdd(logger, session, envelope)
	case *Envelope_GroupUserKick:
		p.groupUserKick(logger, session, envelope)
	case *Envelope_GroupUserPromote:
		p.groupUserPromote(logger, session, envelope)

	case *Envelope_TopicJoin:
		p.topicJoin(logger, session, envelope)
	case *Envelope_TopicLeave:
		p.topicLeave(logger, session, envelope)
	case *Envelope_TopicMessageSend:
		p.topicMessageSend(logger, session, envelope)
	case *Envelope_TopicMessagesList:
		p.topicMessagesList(logger, session, envelope)

	case *Envelope_MatchCreate:
		p.matchCreate(logger, session, envelope)
	case *Envelope_MatchJoin:
		p.matchJoin(logger, session, envelope)
	case *Envelope_MatchLeave:
		p.matchLeave(logger, session, envelope)
	case *Envelope_MatchDataSend:
		p.matchDataSend(logger, session, envelope)

	case *Envelope_MatchmakeAdd:
		p.matchmakeAdd(logger, session, envelope)
	case *Envelope_MatchmakeRemove:
		p.matchmakeRemove(logger, session, envelope)

	case *Envelope_StorageFetch:
		p.storageFetch(logger, session, envelope)
	case *Envelope_StorageWrite:
		p.storageWrite(logger, session, envelope)
	case *Envelope_StorageRemove:
		p.storageRemove(logger, session, envelope)

	case *Envelope_LeaderboardsList:
		p.leaderboardsList(logger, session, envelope)
	case *Envelope_LeaderboardRecordWrite:
		p.leaderboardRecordWrite(logger, session, envelope)
	case *Envelope_LeaderboardRecordsFetch:
		p.leaderboardRecordsFetch(logger, session, envelope)
	case *Envelope_LeaderboardRecordsList:
		p.leaderboardRecordsList(logger, session, envelope)

	case *Envelope_Rpc:
		p.rpc(logger, session, envelope)

	default:
		session.Send(ErrorMessage(envelope.CollationId, UNRECOGNIZED_PAYLOAD, "Unrecognized payload"))
		return
	}

	RuntimeAfterHook(logger, p.runtime, p.jsonpbMarshaler, messageType, envelope, session)
}

func ErrorMessageRuntimeException(collationID string, message string) *Envelope {
	return ErrorMessage(collationID, RUNTIME_EXCEPTION, message)
}

func ErrorMessageBadInput(collationID string, message string) *Envelope {
	return ErrorMessage(collationID, BAD_INPUT, message)
}

func ErrorMessage(collationID string, code Error_Code, message string) *Envelope {
	return &Envelope{
		CollationId: collationID,
		Payload: &Envelope_Error{&Error{
			Message: message,
			Code:    int32(code),
		}}}
}
