// SPDX-AI-Disclosure: ai-generated
// SPDX-AI-Model: GPT-6.0 Astra
// SPDX-AI-Provider: OpenAI
// SPDX-AI-Scope: Library code generated and revised with Codex under maintainer direction.
// SPDX-AI-Date: 2026-10-28

// Package tlv represents owned, ordered SMPP optional parameters. Unknown tags
// and duplicate values survive structural decoding. Codec profiles separately
// validate the semantics of known fields.
package tlv

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// Standard SMS TLV tags. Values remain ordered raw bytes in Fields.
const (
	DestAddrSubunit          uint16 = 0x0005
	DestNetworkType          uint16 = 0x0006
	DestBearerType           uint16 = 0x0007
	DestTelematicsID         uint16 = 0x0008
	SourceAddrSubunit        uint16 = 0x000d
	SourceNetworkType        uint16 = 0x000e
	SourceBearerType         uint16 = 0x000f
	SourceTelematicsID       uint16 = 0x0010
	QOSTimeToLive            uint16 = 0x0017
	PayloadType              uint16 = 0x0019
	AdditionalStatusInfoText uint16 = 0x001d
	ReceiptedMessageID       uint16 = 0x001e
	MSMsgWaitFacilities      uint16 = 0x0030
	PrivacyIndicator         uint16 = 0x0201
	SourceSubaddress         uint16 = 0x0202
	DestSubaddress           uint16 = 0x0203
	UserMessageReference     uint16 = 0x0204
	UserResponseCode         uint16 = 0x0205
	SourcePort               uint16 = 0x020a
	DestinationPort          uint16 = 0x020b
	SARMsgRefNum             uint16 = 0x020c
	LanguageIndicator        uint16 = 0x020d
	SARTotalSegments         uint16 = 0x020e
	SARSegmentSeqnum         uint16 = 0x020f
	SCInterfaceVersion       uint16 = 0x0210
	CallbackNumPresInd       uint16 = 0x0302
	CallbackNumATag          uint16 = 0x0303
	NumberOfMessages         uint16 = 0x0304
	CallbackNum              uint16 = 0x0381
	DPFResult                uint16 = 0x0420
	SetDPF                   uint16 = 0x0421
	MSAvailabilityStatus     uint16 = 0x0422
	NetworkErrorCode         uint16 = 0x0423
	MessagePayload           uint16 = 0x0424
	DeliveryFailureReason    uint16 = 0x0425
	MoreMessagesToSend       uint16 = 0x0426
	MessageState             uint16 = 0x0427
	CongestionState          uint16 = 0x0428
	BillingIdentification    uint16 = 0x060b
	SourceNetworkID          uint16 = 0x060d
	DestNetworkID            uint16 = 0x060e
	SourceNodeID             uint16 = 0x060f
	DestNodeID               uint16 = 0x0610
	DestAddrNPResolution     uint16 = 0x0611
	DestAddrNPInformation    uint16 = 0x0612
	DestAddrNPCountry        uint16 = 0x0613
	USSDServiceOp            uint16 = 0x0501
	DisplayTime              uint16 = 0x1201
	SMSSignal                uint16 = 0x1203
	MSValidity               uint16 = 0x1204
	AlertOnMessageDelivery   uint16 = 0x130c
	ITSReplyType             uint16 = 0x1380
	ITSSessionInfo           uint16 = 0x1383
)

// Field stores a tag and raw value without a caller-controlled wire length.
// A Field literal borrows its Value slice; Clone/Append provide owned copies.
type Field struct {
	Tag   uint16
	Value []byte
}

// Fields preserves wire order and duplicates. It is intentionally a slice, not
// a map: unknown repeated parameters must survive a codec round trip.
// Literals and ordinary slice copies borrow values. Mutation needs external
// synchronization; use Clone to give another owner independent values.
type Fields []Field

// First returns a borrowed value. Use Clone when retaining mutable input.
func (f Fields) First(tag uint16) ([]byte, bool) {
	for _, x := range f {
		if x.Tag == tag {
			return x.Value, true
		}
	}
	return nil, false
}

// All returns matching values in wire order. Values borrow the original slices.
func (f Fields) All(tag uint16) [][]byte {
	var out [][]byte
	for _, x := range f {
		if x.Tag == tag {
			out = append(out, x.Value)
		}
	}
	return out
}

// Clone deep-copies each value, preserving nil versus empty slices.
func (f Fields) Clone() Fields {
	if f == nil {
		return nil
	}
	out := make(Fields, len(f))
	for i, x := range f {
		out[i] = Field{x.Tag, bytes.Clone(x.Value)}
	}
	return out
}

// Append adds an owned copy without normalizing order or duplicates.
func (f *Fields) Append(tag uint16, value []byte) {
	*f = append(*f, Field{tag, append([]byte(nil), value...)})
}

// ReplaceFirst copies value into the first matching position and preserves all
// other occurrences. It returns false without adding a field when absent.
func (f *Fields) ReplaceFirst(tag uint16, value []byte) bool {
	for i := range *f {
		if (*f)[i].Tag == tag {
			(*f)[i].Value = append([]byte(nil), value...)
			return true
		}
	}
	return false
}

// DeleteAll removes every occurrence while retaining other tags' order. Values
// of the remaining fields are reused, not cloned.
func (f *Fields) DeleteAll(tag uint16) {
	out := make(Fields, 0, len(*f))
	for _, x := range *f {
		if x.Tag != tag {
			out = append(out, x)
		}
	}
	*f = out
}

// Uint16 constructs a two-octet unsigned value in SMPP network byte order.
func Uint16(tag uint16, v uint16) Field { return Field{tag, []byte{byte(v >> 8), byte(v)}} }

// Uint16 decodes only exact two-octet values, without interpreting the tag.
func (f Field) Uint16() (uint16, error) {
	if len(f.Value) != 2 {
		return 0, errors.New("tlv: expected two octets")
	}
	return binary.BigEndian.Uint16(f.Value), nil
}
