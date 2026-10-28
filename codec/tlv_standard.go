// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

package codec

import "github.com/skill215/smppkit/tlv"

// Command masks describe wire applicability, not a Session's direction. data_sm
// supports both submission and delivery, so normal encoding accepts the union.
// ValidateDataDirection applies the narrower contextual table when supplied.
type tlvCommands uint16

const (
	submitTLV tlvCommands = 1 << iota
	multiTLV
	deliverTLV
	dataTLV
	submitResponseTLV
	multiResponseTLV
	deliverResponseTLV
	dataResponseTLV
	bindResponseTLV
	alertTLV
	otherResponseTLV
)

type standardTLV struct {
	min, max int // value octets, excluding tag/length
	v34, v50 tlvCommands
	repeat   bool
}

// Immutable built-ins; customer registries cannot replace this table. Bounds
// below are 3.4 bounds, except newly introduced 5.0-only tags. The two changed
// value layouts (alert and ms_validity) are selected explicitly in bounds.
// Applicability: 3.4 PDU tables and 5.0 §§4.2.4/5, 4.3.3/4. dest_subaddress is
// excluded from submit_multi by the field-specific prohibition in both editions.
var standardTLVs = map[uint16]standardTLV{
	tlv.DestAddrSubunit:          {1, 1, submitTLV | multiTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.DestNetworkType:          {1, 1, dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.DestBearerType:           {1, 1, dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.DestTelematicsID:         {2, 2, dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.SourceAddrSubunit:        {1, 1, submitTLV | multiTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.SourceNetworkType:        {1, 1, dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.SourceBearerType:         {1, 1, dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.SourceTelematicsID:       {1, 1, dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.QOSTimeToLive:            {4, 4, dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.PayloadType:              {1, 1, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.AdditionalStatusInfoText: {1, 256, dataResponseTLV, submitResponseTLV | multiResponseTLV | deliverResponseTLV | dataResponseTLV, false},
	tlv.ReceiptedMessageID:       {1, 65, deliverTLV | dataTLV, deliverTLV | dataTLV, false},
	tlv.MSMsgWaitFacilities:      {1, 1, submitTLV | multiTLV | dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.PrivacyIndicator:         {1, 1, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.SourceSubaddress:         {2, 23, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.DestSubaddress:           {2, 23, submitTLV | deliverTLV | dataTLV, submitTLV | deliverTLV | dataTLV, false},
	tlv.UserMessageReference:     {2, 2, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.UserResponseCode:         {1, 1, submitTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.SourcePort:               {2, 2, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.DestinationPort:          {2, 2, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.SARMsgRefNum:             {2, 2, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.LanguageIndicator:        {1, 1, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.SARTotalSegments:         {1, 1, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.SARSegmentSeqnum:         {1, 1, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.SCInterfaceVersion:       {1, 1, bindResponseTLV, bindResponseTLV, false},
	tlv.CallbackNumPresInd:       {1, 1, submitTLV | multiTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, true},
	tlv.CallbackNumATag:          {1, 65, submitTLV | multiTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, true},
	tlv.NumberOfMessages:         {1, 1, submitTLV | dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.CallbackNum:              {4, 19, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, true},
	tlv.DPFResult:                {1, 1, dataResponseTLV, deliverTLV | dataTLV | submitResponseTLV | multiResponseTLV | dataResponseTLV, false},
	tlv.SetDPF:                   {1, 1, dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.MSAvailabilityStatus:     {1, 1, alertTLV, alertTLV, false},
	tlv.NetworkErrorCode:         {3, 3, deliverTLV | dataTLV | dataResponseTLV, deliverTLV | dataTLV | submitResponseTLV | multiResponseTLV | deliverResponseTLV | dataResponseTLV, false},
	tlv.MessagePayload:           {0, 65535, submitTLV | multiTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.DeliveryFailureReason:    {1, 1, dataResponseTLV, submitResponseTLV | multiResponseTLV | deliverResponseTLV | dataResponseTLV, false},
	tlv.MoreMessagesToSend:       {1, 1, submitTLV | dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.MessageState:             {1, 1, deliverTLV | dataTLV, deliverTLV | dataTLV, false},
	tlv.CongestionState:          {1, 1, 0, submitResponseTLV | multiResponseTLV | deliverResponseTLV | dataResponseTLV | bindResponseTLV | otherResponseTLV, false},
	tlv.BillingIdentification:    {1, 1024, 0, submitTLV | multiTLV | dataTLV, false},
	tlv.SourceNetworkID:          {7, 65, 0, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.DestNetworkID:            {7, 65, 0, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.SourceNodeID:             {6, 6, 0, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.DestNodeID:               {6, 6, 0, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.DestAddrNPResolution:     {1, 1, 0, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.DestAddrNPInformation:    {10, 10, 0, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.DestAddrNPCountry:        {1, 5, 0, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.USSDServiceOp:            {1, 1, submitTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.DisplayTime:              {1, 1, submitTLV | multiTLV | dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.SMSSignal:                {2, 2, submitTLV | multiTLV | dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.MSValidity:               {1, 1, submitTLV | multiTLV | dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.AlertOnMessageDelivery:   {0, 0, submitTLV | multiTLV | dataTLV, submitTLV | multiTLV | dataTLV, false},
	tlv.ITSReplyType:             {1, 1, submitTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
	tlv.ITSSessionInfo:           {2, 2, submitTLV | deliverTLV | dataTLV, submitTLV | multiTLV | deliverTLV | dataTLV, false},
}
