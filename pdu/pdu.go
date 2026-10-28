// Package pdu defines concrete SMPP command bodies. Payloads are raw bytes;
// encoding never changes caller data or interprets message IDs as numbers.
package pdu

import (
	"bytes"

	"github.com/skill215/smppkit/tlv"
)

// Version is the exact interface_version octet, not a decimal version number.
type Version uint8

const (
	V33 Version = 0x33
	V34 Version = 0x34
	V50 Version = 0x50
)

func (v Version) Valid() bool { return v == V33 || v == V34 || v == V50 }

// CommandID identifies a request or response on the wire.
type CommandID uint32

const ResponseBit CommandID = 0x80000000

func (c CommandID) IsResponse() bool { return c&ResponseBit != 0 }

// Status is command_status. Requests use OK; response status is peer acceptance,
// not a final SMS delivery state. Unknown peer status values remain representable.
type Status uint32

const (
	OK                       Status = 0
	InvalidMessageLength     Status = 1
	InvalidCommandLength     Status = 2
	InvalidCommandID         Status = 3
	InvalidBindState         Status = 4
	SystemError              Status = 8
	BindFailed               Status = 13
	Throttled                Status = 0x58
	InvalidOptionalParameter Status = 0xc4
)

// Body supplies command identity; known standard commands require concrete
// structs. Use a registered CommandCodec for private command bodies.
type Body interface{ CommandID() CommandID }

// PDU is one frame without a caller-controlled length. Codec calculates length;
// Session assigns outbound request sequences and preserves response correlation.
type PDU struct {
	Sequence uint32
	Status   Status
	Body     Body
}

// Address retains numeric TON/NPI and the ASCII SMPP address string. Codec
// checks their version-specific representation; it does not resolve or validate
// carrier routing. Value is not BCD and may contain alphanumeric sender IDs.
type Address struct {
	TON, NPI uint8
	Value    string
}

// Opaque is only for unregistered private commands. Known standard commands
// cannot bypass their typed validation using Opaque.
type Opaque struct {
	ID   CommandID
	Data []byte
}

func (b *Opaque) CommandID() CommandID { return b.ID }

// Omitted represents an absent body of a negative response, distinct from an
// empty C-octet field. Legality depends on command, status, and version.
type Omitted struct{ ID CommandID }

func (b *Omitted) CommandID() CommandID { return b.ID }

// BindFields contains the ESME identity and requested interface version.
// Password is sensitive; avoid logging the entire struct.
type BindFields struct {
	SystemID, Password, SystemType string
	InterfaceVersion               Version
	AddrTON, AddrNPI               uint8
	AddressRange                   string
}
type BindResponseFields struct {
	SystemID string
	Optional tlv.Fields
}

// SMFields is the shared mandatory/optional shape of submit_sm and deliver_sm.
// ShortMessage is already encoded data; DataCoding does not trigger conversion.
// ShortMessage and message_payload are mutually exclusive.
// ScheduleDeliveryTime and ValidityPeriod are empty or SMPP time strings, not
// Go timestamps. 3.3 accepts absolute times; 3.4/5.0 also allow relative offsets.
// deliver_sm reserves schedule/validity/replacement/default ID in 3.4; 5.0
// permits them for forwarding to another message centre. The application owns
// that routing context. Codec never normalizes times or decodes ShortMessage.
type SMFields struct {
	ServiceType                                                        string
	Source, Destination                                                Address
	ESMClass, ProtocolID, PriorityFlag                                 uint8
	ScheduleDeliveryTime, ValidityPeriod                               string
	RegisteredDelivery, ReplaceIfPresent, DataCoding, DefaultMessageID uint8
	ShortMessage                                                       []byte
	Optional                                                           tlv.Fields
}

// Clone deep-copies payload and TLVs, preserving nil versus empty slices.
func (b SMFields) Clone() SMFields {
	b.ShortMessage = bytes.Clone(b.ShortMessage)
	b.Optional = b.Optional.Clone()
	return b
}

// MessageResponseFields carries an opaque peer ID; never parse it as an integer
// or assume a successful response is a final delivery receipt.
type MessageResponseFields struct {
	MessageID string
	Optional  tlv.Fields
}

const BindReceiverID CommandID = 0x1

type BindReceiver struct{ BindFields }

func (*BindReceiver) CommandID() CommandID { return BindReceiverID }

const BindReceiverRespID CommandID = BindReceiverID | ResponseBit

type BindReceiverResp struct{ BindResponseFields }

func (*BindReceiverResp) CommandID() CommandID { return BindReceiverRespID }

const BindTransmitterID CommandID = 0x2

type BindTransmitter struct{ BindFields }

func (*BindTransmitter) CommandID() CommandID { return BindTransmitterID }

const BindTransmitterRespID CommandID = BindTransmitterID | ResponseBit

type BindTransmitterResp struct{ BindResponseFields }

func (*BindTransmitterResp) CommandID() CommandID { return BindTransmitterRespID }

const QuerySMID CommandID = 0x3

type QuerySM struct {
	MessageID string
	Source    Address
}

func (*QuerySM) CommandID() CommandID { return QuerySMID }

const QuerySMRespID CommandID = QuerySMID | ResponseBit

// QuerySMResp reports a peer state, distinct from successful submission. FinalDate
// is empty or absolute; 3.3/3.4 also accept their specified 12-digit local response
// date. An OK 3.4/5.0 response must contain a defined MessageState. Negative
// responses may contain zero placeholders or use Omitted where permitted.
type QuerySMResp struct {
	MessageID, FinalDate    string
	MessageState, ErrorCode uint8
	Optional                tlv.Fields // 5.0 response TLVs only
}

func (*QuerySMResp) CommandID() CommandID { return QuerySMRespID }

const SubmitSMID CommandID = 0x4

type SubmitSM struct{ SMFields }

func (*SubmitSM) CommandID() CommandID { return SubmitSMID }

const SubmitSMRespID CommandID = SubmitSMID | ResponseBit

type SubmitSMResp struct{ MessageResponseFields }

func (*SubmitSMResp) CommandID() CommandID { return SubmitSMRespID }

const DeliverSMID CommandID = 0x5

type DeliverSM struct{ SMFields }

func (*DeliverSM) CommandID() CommandID { return DeliverSMID }

const DeliverSMRespID CommandID = DeliverSMID | ResponseBit

// DeliverSMResp requires an empty MessageID in 3.4/5.0. 3.3 reuses the submission
// response shape and can retain its peer ID.
type DeliverSMResp struct{ MessageResponseFields }

func (*DeliverSMResp) CommandID() CommandID { return DeliverSMRespID }

const UnbindID CommandID = 0x6

type Unbind struct{}

func (*Unbind) CommandID() CommandID { return UnbindID }

const UnbindRespID CommandID = UnbindID | ResponseBit

// UnbindResp carries optional TLVs only in SMPP 5.0 (e.g. congestion_state).
type UnbindResp struct{ Optional tlv.Fields }

func (*UnbindResp) CommandID() CommandID { return UnbindRespID }

const ReplaceSMID CommandID = 0x7

type ReplaceSM struct {
	MessageID                            string
	Source                               Address
	ScheduleDeliveryTime, ValidityPeriod string
	RegisteredDelivery, DefaultMessageID uint8
	ShortMessage                         []byte
}

func (*ReplaceSM) CommandID() CommandID { return ReplaceSMID }

const ReplaceSMRespID CommandID = ReplaceSMID | ResponseBit

// ReplaceSMResp carries optional TLVs only in SMPP 5.0 (e.g. congestion_state).
type ReplaceSMResp struct{ Optional tlv.Fields }

func (*ReplaceSMResp) CommandID() CommandID { return ReplaceSMRespID }

const CancelSMID CommandID = 0x8

type CancelSM struct {
	ServiceType, MessageID string
	Source, Destination    Address
}

func (*CancelSM) CommandID() CommandID { return CancelSMID }

const CancelSMRespID CommandID = CancelSMID | ResponseBit

// CancelSMResp carries optional TLVs only in SMPP 5.0 (e.g. congestion_state).
type CancelSMResp struct{ Optional tlv.Fields }

func (*CancelSMResp) CommandID() CommandID { return CancelSMRespID }

const BindTransceiverID CommandID = 0x9

type BindTransceiver struct{ BindFields }

func (*BindTransceiver) CommandID() CommandID { return BindTransceiverID }

const BindTransceiverRespID CommandID = BindTransceiverID | ResponseBit

type BindTransceiverResp struct{ BindResponseFields }

func (*BindTransceiverResp) CommandID() CommandID { return BindTransceiverRespID }

const OutbindID CommandID = 0xb

// Outbind is an MC's pre-bind invitation, not a request with an SMPP response.
// Credentials belong to the MC; applications must validate them before binding
// and avoid logging this body. The PDU contains no interface version.
type Outbind struct{ SystemID, Password string }

func (*Outbind) CommandID() CommandID { return OutbindID }

const EnquireLinkID CommandID = 0x15

type EnquireLink struct{}

func (*EnquireLink) CommandID() CommandID { return EnquireLinkID }

const EnquireLinkRespID CommandID = EnquireLinkID | ResponseBit

// EnquireLinkResp carries optional TLVs only in SMPP 5.0 (e.g. congestion_state).
type EnquireLinkResp struct{ Optional tlv.Fields }

func (*EnquireLinkResp) CommandID() CommandID { return EnquireLinkRespID }

const SubmitMultiID CommandID = 0x21
const SubmitMultiRespID CommandID = SubmitMultiID | ResponseBit
const AlertNotificationID CommandID = 0x102

// AlertNotification reports subscriber availability on an RX/TRX session.
// It has no response/ACK and does not report final message delivery.
type AlertNotification struct {
	Source, ESME Address
	Optional     tlv.Fields
}

func (*AlertNotification) CommandID() CommandID { return AlertNotificationID }

const DataSMID CommandID = 0x103

// DataSM is shared by ESME submission and MC delivery. Standalone Codec handles
// their union; use Codec.ValidateDataDirection to add explicit request context.
// Session automatically enforces the direction at both receiving and sending.
type DataSM struct {
	ServiceType                              string
	Source, Destination                      Address
	ESMClass, RegisteredDelivery, DataCoding uint8
	Optional                                 tlv.Fields
}

func (*DataSM) CommandID() CommandID { return DataSMID }

const DataSMRespID CommandID = DataSMID | ResponseBit

// DataSMResp uses the original request's direction and ESM class for contextual
// validation (Codec.ValidateDataResponse; automatic in Session). Unlike
// deliver_sm_resp, its MessageID is not forced empty for delivery. Explicit
// transaction mode reports a synchronous peer delivery outcome; an ordinary
// acceptance response must not be interpreted as final handset delivery.
type DataSMResp struct{ MessageResponseFields }

func (*DataSMResp) CommandID() CommandID { return DataSMRespID }

const BroadcastSMID CommandID = 0x111
const BroadcastSMRespID CommandID = BroadcastSMID | ResponseBit
const QueryBroadcastSMID CommandID = 0x112
const QueryBroadcastSMRespID CommandID = QueryBroadcastSMID | ResponseBit
const CancelBroadcastSMID CommandID = 0x113
const CancelBroadcastSMRespID CommandID = CancelBroadcastSMID | ResponseBit
const GenericNACKID CommandID = ResponseBit

// GenericNACK carries optional TLVs only in SMPP 5.0 (e.g. congestion_state).
type GenericNACK struct{ Optional tlv.Fields }

func (*GenericNACK) CommandID() CommandID { return GenericNACKID }
